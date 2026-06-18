// `a-novel core bot-comment <org> <repo> <number> --body <text>` — post a
// PR/issue/review comment as the org's GitHub App bot.
//
// The bot's signing key never lives on a dev machine. Instead this
// command hands the comment (body + target metadata) to the per-org
// `bot-comment` workflow_dispatch (one dispatcher per org — see
// botDispatchRepos), triggered with the OPERATOR's own gh token. The
// workflow holds the App key, mints a scoped short-lived token, and
// posts as <app-slug>[bot]. See .github/workflows/bot-comment.yaml.
//
// Why a workflow instead of minting locally:
//
//   - No local app tokens. A user only needs gh + actions:write on the
//     dispatcher repo; they never handle a .pem. Revocation is per-user.
//   - Comment-only BY DESIGN. The workflow can only post a comment, so a
//     compromised trigger cannot push, merge, or author a PR — there is
//     no skeleton-key token locally to misuse. This replaces the old
//     bot-gh deny-list (a soft wrapper guard) with a structural one.
//   - Separation of duties. The trigger token cannot change WHAT the
//     workflow does — that is code on the protected master branch.
//
// The command waits for the dispatched run synchronously (gh run watch
// --exit-status) and surfaces failures, so callers can react to errors.
package cli

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	// botWorkflowFile is the workflow_dispatch file, by basename.
	botWorkflowFile = "bot-comment.yaml"
	// botRunLookupTimeout bounds how long we wait for the dispatched run
	// to register before giving up — dispatch returns no run id, so we
	// poll the run list until our nonce shows up in a run-name.
	botRunLookupTimeout = 60 * time.Second
)

// botDispatchRepos maps each org to the repo that hosts its bot-comment
// dispatcher workflow. The dispatcher MUST live in a repo within the
// target org: it reads that org's AGENT_BOT_PRIVATE_KEY org secret, and
// org secrets do not cross org boundaries. Hence one dispatcher (and one
// routing entry) per org, not a single central one.
var botDispatchRepos = map[string]string{
	orgAnovelKit: orgAnovelKit + "/stack",
	orgAnovel:    orgAnovel + "/.github",
}

func newCoreBotCommentCmd() *cobra.Command {
	var (
		body    string
		replyTo string
	)
	cmd := &cobra.Command{
		Use:   "bot-comment <org> <repo> <number>",
		Short: "Post a PR/issue/review comment as the org App bot",
		Long: `Posts a comment that attributes to <app-slug>[bot] instead of your
user account, by triggering the bot-comment workflow in the target org's
dispatcher repo with your own gh token. The App signing key never touches
your machine.

This is the ONLY supported way to comment as the bot. PR/issue authoring,
edits, ready/merge/close stay on your operator gh token (the bot cannot
do them — the workflow only posts comments).

Works against both PRs and issues, for both new comments and replies:

  # comment on a PR (top-level "Conversation" comment)
  a-novel core bot-comment a-novel service-authentication 549 \
      --body "automated review note"

  # comment on an issue (same command — PRs and issues share this path)
  a-novel core bot-comment a-novel-kit golib 42 \
      --body "tracked, will fix in the next pass"

  # reply inside a PR's inline review thread. <id> is a REST
  # review-comment id from repos/<org>/<repo>/pulls/<n>/comments.
  a-novel core bot-comment a-novel service-authentication 549 \
      --reply-to 2103847559 --body "Fixed in abc1234."

--reply-to targets a PR inline review thread only; issues have no review
threads, so to answer an issue comment just post a new comment (the body
can quote it with a leading '>').

The command blocks until the workflow run finishes and exits non-zero if
it failed, printing the failed step's logs.

Requires: gh authenticated as yourself, with actions:write on the target
org's dispatcher repo.`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			org, repo, number := args[0], args[1], args[2]
			if err := validateBotCommentArgs(org, repo, number); err != nil {
				return err
			}
			if strings.TrimSpace(body) == "" {
				return errors.New("bot-comment: --body is required and must be non-empty")
			}
			if replyTo != "" {
				if n, err := strconv.Atoi(replyTo); err != nil || n <= 0 {
					return fmt.Errorf("bot-comment: --reply-to %q must be a positive integer (a REST review-comment id)", replyTo)
				}
			}
			return runBotComment(cmd, org, repo, number, body, replyTo)
		},
	}
	cmd.Flags().StringVar(&body, "body", "", "markdown comment body (required)")
	cmd.Flags().StringVar(&replyTo, "reply-to", "", "inline review-comment id to reply to (omit for a top-level comment)")
	return cmd
}

// validateBotCommentArgs checks the positional args before we spend a
// dispatch on them. Kept separate from the gh plumbing so it is unit
// testable.
func validateBotCommentArgs(org, repo, number string) error {
	if _, ok := botDispatchRepos[org]; !ok {
		return fmt.Errorf("bot-comment: org %q is not configured (known: %s, %s)", org, orgAnovel, orgAnovelKit)
	}
	if strings.TrimSpace(repo) == "" {
		return fmt.Errorf("bot-comment: repo is required (name only, no %q prefix)", org+"/")
	}
	if strings.ContainsRune(repo, '/') {
		return fmt.Errorf("bot-comment: repo %q must be the bare repo name, without the org prefix", repo)
	}
	if n, err := strconv.Atoi(number); err != nil || n <= 0 {
		return fmt.Errorf("bot-comment: number %q must be a positive integer", number)
	}
	return nil
}

// runBotComment dispatches the workflow and watches the resulting run to
// completion.
func runBotComment(cmd *cobra.Command, org, repo, number, body, replyTo string) error {
	dispatchRepo := botDispatchRepos[org]

	nonce, err := newNonce()
	if err != nil {
		return fmt.Errorf("bot-comment: generate nonce: %w", err)
	}

	dispatchArgs := []string{
		"workflow", "run", botWorkflowFile,
		"--repo", dispatchRepo,
		"-f", "repo=" + repo,
		"-f", "number=" + number,
		"-f", "body=" + body,
		"-f", "nonce=" + nonce,
	}
	if replyTo != "" {
		dispatchArgs = append(dispatchArgs, "-f", "reply_to="+replyTo)
	}
	if _, err := runGHOut(dispatchArgs...); err != nil {
		return fmt.Errorf("bot-comment: dispatch to %s failed: %w", dispatchRepo, err)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "dispatched bot-comment for %s/%s#%s; waiting for the run...\n", org, repo, number)

	runID, err := waitForDispatchedRun(dispatchRepo, nonce)
	if err != nil {
		return fmt.Errorf("bot-comment: %w", err)
	}

	// gh run watch streams progress and, with --exit-status, returns a
	// non-zero exit if the run concluded in failure. Wire it straight to
	// our stdio so the operator sees live progress.
	watch := exec.Command("gh", "run", "watch", strconv.FormatInt(runID, 10),
		"--repo", dispatchRepo, "--exit-status", "--interval", "3")
	watch.Stdout = cmd.OutOrStdout()
	watch.Stderr = cmd.ErrOrStderr()
	if err := watch.Run(); err != nil {
		// The run failed; pull the failed step's logs to explain why.
		logs, _ := runGHOut("run", "view", strconv.FormatInt(runID, 10),
			"--repo", dispatchRepo, "--log-failed")
		return fmt.Errorf("bot-comment: workflow run %d failed: %w\n%s", runID, err, logs)
	}
	return nil
}

// waitForDispatchedRun polls the run list until a run whose name carries
// our nonce appears, and returns its database id. workflow_dispatch does
// not return a run id, and the run takes a moment to register, so we
// poll with a bounded deadline.
func waitForDispatchedRun(dispatchRepo, nonce string) (int64, error) {
	deadline := time.Now().Add(botRunLookupTimeout)
	for {
		out, err := runGHOut("run", "list",
			"--repo", dispatchRepo,
			"--workflow", botWorkflowFile,
			"--event", "workflow_dispatch",
			"--limit", "30",
			"--json", "databaseId,displayTitle")
		if err == nil {
			var runs []struct {
				DatabaseID   int64  `json:"databaseId"`
				DisplayTitle string `json:"displayTitle"`
			}
			if jsonErr := json.Unmarshal([]byte(out), &runs); jsonErr == nil {
				for _, r := range runs {
					if strings.Contains(r.DisplayTitle, "("+nonce+")") {
						return r.DatabaseID, nil
					}
				}
			}
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("dispatched run did not appear within %s "+
				"(check 'gh run list --repo %s')", botRunLookupTimeout, dispatchRepo)
		}
		time.Sleep(2 * time.Second)
	}
}

// runGHOut runs gh with the operator's own credentials and returns
// stdout only. stderr is captured separately so gh's progress/warning
// chatter can never corrupt the JSON we parse from stdout (the run-list
// poll); on failure stderr is folded into the returned error so the
// caller still sees what went wrong.
func runGHOut(args ...string) (string, error) {
	c := exec.Command("gh", args...)
	c.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return stdout.String(), nil
}

// newNonce returns a short random hex token used to correlate a dispatch
// with its run (via the run-name) and to dedupe the comment.
func newNonce() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
