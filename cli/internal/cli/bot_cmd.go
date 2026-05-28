// `a-novel core bot-token <org>` + `a-novel core bot-gh <org> ...` —
// the Go port of scripts/lib/bot-token.sh.
//
// What this does:
//
//   - Mints a short-lived (1h) GitHub App installation token for the
//     bot account attached to <org>, by signing an RS256 JWT with the
//     org's private .pem and exchanging it via
//     /app/installations/<id>/access_tokens.
//   - bot-gh runs `gh` with GH_TOKEN set to that fresh token, so the
//     resulting API calls attribute to <app-slug>[bot] instead of the
//     operator's user account.
//   - Refuses every gh subcommand that AUTHORS a PR/issue or changes
//     its state. The bot account is comment-only; PR creation/edits/
//     ready/merge/close MUST run with the operator user token so CI
//     triggers and CODEOWNERS work. This guard hard-enforces what was
//     previously only a doc rule (see the open-pull-request skill).
//
// Org config is hardcoded here (App ID + Installation ID are PUBLIC —
// visible in any GitHub App settings URL). Private keys live under
// $BOT_KEY_DIR (default: <stack-root>/.secrets/) and are .gitignored.

package cli

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// botOrg is the hardcoded per-org bot config. App ID and Installation
// ID are public; only the .pem file is sensitive.
type botOrg struct {
	AppID          string
	InstallationID string
	KeyFile        string
}

// botOrgs mirrors scripts/lib/bots.conf one-for-one. Keep these IDs in
// sync if/when the bash file changes (the bash file remains the source
// of truth until both ports are committed and the file is deleted).
var botOrgs = map[string]botOrg{
	orgAnovel: {
		AppID:          "3549319",
		InstallationID: "128231241",
		KeyFile:        "anovelbot-agent.private-key.pem",
	},
	orgAnovelKit: {
		AppID:          "3549379",
		InstallationID: "128232102",
		KeyFile:        "anovelkitbot-agent.private-key.pem",
	},
}

// deniedBotSubcommands is the set of `gh` subcommand pairs the bot
// account is NOT allowed to invoke. Anything that authors or
// state-changes a PR/issue must run as the operator (plain `gh`).
// Keys are "verb subverb" pairs — matched on the first two args.
var deniedBotSubcommands = map[string]struct{}{
	"pr create":      {},
	"pr edit":        {},
	"pr ready":       {},
	"pr merge":       {},
	"pr close":       {},
	"pr reopen":      {},
	"pr review":      {},
	"pr lock":        {},
	"pr unlock":      {},
	"issue create":   {},
	"issue edit":     {},
	"issue close":    {},
	"issue reopen":   {},
	"issue transfer": {},
	"issue pin":      {},
	"issue lock":     {},
	"issue unlock":   {},
}

func newCoreBotTokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bot-token <org>",
		Short: "Mint a short-lived GitHub App installation token for an org",
		Long: `Prints a fresh (1h) GitHub App installation token to stdout. The
private key lives at $BOT_KEY_DIR/<org>'s pem (default key dir is
<stack-root>/.secrets/, gitignored).

Use this for one-off API calls that need to author as the bot account:

  GH_TOKEN="$(a-novel core bot-token a-novel)" \
      gh api repos/.../comments --field body=...

For the common case of running gh as the bot, see 'a-novel core bot-gh'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := mintInstallationToken(args[0])
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), token)
			return nil
		},
	}
}

func newCoreBotGhCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bot-gh <org> -- <gh args...>",
		Short: "Run gh as the bot account (comments-only)",
		Long: `Wrapper around 'gh' that authenticates as the bot account for <org>.
Equivalent to running:

  GH_TOKEN="$(a-novel core bot-token <org>)" gh <args...>

Reserved for COMMENT operations (PR/issue top-level comments, review-
thread replies). PR creation, edits, ready/merge/close, and the
matching issue verbs are HARD-REJECTED with a non-zero exit — those
must run with the operator's user token so CI triggers and CODEOWNERS
attribution work correctly.

Args after the first positional are forwarded to gh verbatim. Use
'--' to separate flags from gh-side flags if your shell needs help:

  a-novel core bot-gh a-novel -- pr comment 549 --body "review note"`,
		Args:               cobra.MinimumNArgs(2),
		DisableFlagParsing: true, // hand the rest straight to gh
		RunE: func(cmd *cobra.Command, args []string) error {
			org := args[0]
			ghArgs := args[1:]
			// Strip leading "--" some shells preserve.
			if len(ghArgs) > 0 && ghArgs[0] == "--" {
				ghArgs = ghArgs[1:]
			}
			if err := checkBotGhAllowed(ghArgs); err != nil {
				return err
			}
			token, err := mintInstallationToken(org)
			if err != nil {
				return err
			}
			c := exec.Command("gh", ghArgs...)
			c.Env = append(os.Environ(), "GH_TOKEN="+token)
			c.Stdin = cmd.InOrStdin()
			c.Stdout = cmd.OutOrStdout()
			c.Stderr = cmd.ErrOrStderr()
			return c.Run()
		},
	}
	return cmd
}

// checkBotGhAllowed enforces the deny-list. Returns nil for allowed
// (i.e. comment-only) invocations.
func checkBotGhAllowed(args []string) error {
	if len(args) < 2 {
		return nil
	}
	key := args[0] + " " + args[1]
	if _, denied := deniedBotSubcommands[key]; denied {
		return fmt.Errorf(
			"bot-gh: '%s' is not allowed — the bot account is comment-only. "+
				"Run this with the operator user token instead: plain `gh %s ...`",
			key, key)
	}
	return nil
}

// mintInstallationToken does the full JWT sign + token exchange dance
// for a single org. Returns the installation token on success.
func mintInstallationToken(org string) (string, error) {
	cfg, ok := botOrgs[org]
	if !ok {
		return "", fmt.Errorf("bot-token: org %q is not configured (known: %s, %s)", org, orgAnovel, orgAnovelKit)
	}
	pemPath, err := resolveBotPemPath(cfg.KeyFile)
	if err != nil {
		return "", err
	}
	key, err := loadRSAPrivateKey(pemPath)
	if err != nil {
		return "", fmt.Errorf("bot-token: load %s: %w", pemPath, err)
	}
	jwt, err := signAppJWT(cfg.AppID, key)
	if err != nil {
		return "", fmt.Errorf("bot-token: sign JWT: %w", err)
	}
	tok, err := exchangeJWTForInstallationToken(jwt, cfg.InstallationID)
	if err != nil {
		return "", fmt.Errorf("bot-token: exchange JWT for token (%s): %w", org, err)
	}
	return tok, nil
}

// resolveBotPemPath returns the absolute path of the org's private
// key. Precedence:
//  1. $BOT_KEY_DIR/<file> if BOT_KEY_DIR is set (explicit operator
//     override — bash compat).
//  2. <stack-root>/.secrets/<file> otherwise — same default the bash
//     script uses, computed via defaultCLISource (which returns
//     <stack>/cli; the .secrets dir is one level up).
//
// Returns an error if the file is missing — failing fast with a clear
// path is more useful than letting the RS256 step blow up on EOF.
func resolveBotPemPath(filename string) (string, error) {
	if dir := os.Getenv("BOT_KEY_DIR"); dir != "" {
		return filepath.Join(dir, filename), assertReadable(filepath.Join(dir, filename))
	}
	cliDir, err := defaultCLISource()
	if err != nil {
		return "", fmt.Errorf("resolve stack root: %w (set BOT_KEY_DIR to override)", err)
	}
	stackRoot := filepath.Dir(cliDir)
	path := filepath.Join(stackRoot, ".secrets", filename)
	return path, assertReadable(path)
}

func assertReadable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("private key not found at %s: %w", path, err)
	}
	// Permission warning matches the bash script — group/world-readable
	// .pem lets any local user mint tokens.
	if info.Mode().Perm()&0o077 != 0 {
		_, _ = fmt.Fprintf(os.Stderr,
			"warning: %s has permissive mode %o; recommend chmod 600\n",
			path, info.Mode().Perm())
	}
	return nil
}

// loadRSAPrivateKey reads a PEM-encoded RSA private key (PKCS#1 or
// PKCS#8) and returns the parsed *rsa.PrivateKey.
func loadRSAPrivateKey(path string) (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	// Try PKCS#1 first (the format `openssl genrsa` and GitHub-issued
	// keys default to), fall back to PKCS#8.
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	rsaKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is %T, want *rsa.PrivateKey", parsed)
	}
	return rsaKey, nil
}

// signAppJWT builds and signs a GitHub-App-shaped JWT.
//
//	{ "alg": "RS256", "typ": "JWT" }
//	{ "iat": <now-60s>, "exp": <now+540s>, "iss": "<appID>" }
//
// iat is set 60s back to absorb local clock skew; exp stays under
// GitHub's 600s cap. Exact same window the bash script uses.
func signAppJWT(appID string, key *rsa.PrivateKey) (string, error) {
	now := time.Now().Unix()
	header := `{"alg":"RS256","typ":"JWT"}`
	payload := fmt.Sprintf(`{"iat":%d,"exp":%d,"iss":"%s"}`, now-60, now+540, appID)
	unsigned := b64url([]byte(header)) + "." + b64url([]byte(payload))
	digest := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + b64url(sig), nil
}

// exchangeJWTForInstallationToken POSTs to the installation
// access-tokens endpoint and returns the short-lived token.
func exchangeJWTForInstallationToken(jwt, installationID string) (string, error) {
	url := "https://api.github.com/app/installations/" + installationID + "/access_tokens"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(nil))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("github api %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if parsed.Token == "" {
		return "", errors.New("github returned an empty token")
	}
	return parsed.Token, nil
}

// b64url is RFC 7515 §2 base64url: URL-safe alphabet, no padding.
func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
