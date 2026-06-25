// `a-novel secrets` — a local, encrypted secrets manager. Secrets are stored
// AES-256-GCM-encrypted at rest under a 0600 local key, set interactively with
// no echo, and never printed by any command. They are injected into a child
// process's environment only — by `secrets exec`, and automatically by
// `test`/`run`/`ui` via the value-free per-repo mapping (.a-novel/secrets.yaml).
//
// The goal is to prevent accidental exposure of API secrets (e.g.
// OPENAI_API_KEY) when a developer — or an AI agent — runs the toolchain: the
// value is never seen on a terminal, in a log, in git, or as a CLI argument.
package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/a-novel-kit/stack/cli/internal/secrets"
)

// secretsStdinIsTTY reports whether stdin is an interactive terminal. A package
// var (not a direct call) so the `secrets set` non-interactive refusal is
// testable without a real PTY — the same seam the publish command uses. Setting
// a secret reads a no-echo value from the terminal, so it is human-only: an
// agent or CI run (no TTY) is refused rather than silently reading nothing.
var secretsStdinIsTTY = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// readPassword reads a line from the terminal with echo disabled. A package var
// so tests can stub it; production reads via golang.org/x/term so the typed
// secret never appears on screen.
var readPassword = func() ([]byte, error) { return term.ReadPassword(int(os.Stdin.Fd())) }

func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage local, encrypted secrets injected into child process env only",
		Long: `Local encrypted secrets manager. Secrets are encrypted at rest with
AES-256-GCM under a 0600 local key, set interactively with no echo, and never
printed by any command. They are injected into a child process's environment
only.

  secrets init             create the local key + store dir (idempotent)
  secrets set <id>         read a value with no echo and store it encrypted
  secrets ls               list secret ids (never values)
  secrets rm <id>          delete a secret
  secrets exec --env ...   run a command with named secrets in its env

Auto-injection: a service repo can commit a value-free mapping at
.a-novel/secrets.yaml ('env: { OPENAI_API_KEY: openai-key }') and the named
secrets are injected automatically into the child env of 'a-novel test',
'a-novel run' and 'a-novel run ui'. No command ever prints a secret value.`,
	}
	cmd.AddCommand(newSecretsInitCmd())
	cmd.AddCommand(newSecretsSetCmd())
	cmd.AddCommand(newSecretsListCmd())
	cmd.AddCommand(newSecretsRemoveCmd())
	cmd.AddCommand(newSecretsExecCmd())
	return cmd
}

func newSecretsInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the local secrets key and store directory (idempotent)",
		Long: `Creates the secrets directory (mode 0700) and a fresh 32-byte local key
(mode 0600) if they don't already exist. Idempotent: an existing key is never
overwritten. Prints a confirmation only — never any secret.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			created, err := secrets.Init()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if created {
				_, _ = fmt.Fprintln(out, "✓ initialized secrets store (new local key generated)")
			} else {
				_, _ = fmt.Fprintln(out, "✓ secrets store already initialized")
			}
			return nil
		},
	}
}

func newSecretsSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <id>",
		Short: "Store a secret value (read with no echo); never printed",
		Long: `Reads a value for <id> from the terminal with echo disabled, encrypts it
into the local store, and prints only 'set <id>' — never the value.

Interactive-only: it refuses to run without a terminal so an agent or CI job
cannot pipe a value through (which would risk it landing in a log or shell
history). Provision secrets yourself from a terminal.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if strings.TrimSpace(id) == "" {
				return errors.New("secrets set: <id> must not be empty")
			}
			// Reading a no-echo value needs a real terminal. Refuse otherwise —
			// piping a value in would defeat the "never on a terminal/log/history"
			// guarantee.
			if !secretsStdinIsTTY() {
				return errors.New("secrets set: refusing to run non-interactively — a secret " +
					"is read with no echo from a terminal, so it must be set by a human. Run it yourself")
			}
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Enter value for %q (input hidden): ", id)
			value, err := readPassword()
			// term.ReadPassword leaves the cursor on the same line; advance it so
			// the confirmation isn't glued to the prompt.
			_, _ = fmt.Fprintln(out)
			if err != nil {
				return fmt.Errorf("secrets set: read value: %w", err)
			}
			if len(value) == 0 {
				return errors.New("secrets set: empty value — nothing stored")
			}
			st, err := secrets.Open()
			if err != nil {
				return err
			}
			st.Set(id, string(value))
			if err := st.Save(); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "✓ set %s\n", id)
			return nil
		},
	}
}

func newSecretsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List secret ids (never values)",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := secrets.Open()
			if err != nil {
				return err
			}
			ids := st.List()
			out := cmd.OutOrStdout()
			if len(ids) == 0 {
				_, _ = fmt.Fprintln(out, "no secrets set — use `a-novel secrets set <id>`")
				return nil
			}
			for _, id := range ids {
				_, _ = fmt.Fprintln(out, id)
			}
			return nil
		},
	}
}

func newSecretsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rm <id>",
		Aliases: []string{"remove"},
		Short:   "Delete a secret",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			st, err := secrets.Open()
			if err != nil {
				return err
			}
			if _, ok := st.Get(id); !ok {
				return fmt.Errorf("secrets rm: no secret named %q", id)
			}
			st.Remove(id)
			if err := st.Save(); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ removed %s\n", id)
			return nil
		},
	}
}

func newSecretsExecCmd() *cobra.Command {
	var envPairs []string
	cmd := &cobra.Command{
		Use:   "exec --env NAME=<secret-id> [--env ...] -- <cmd> [args...]",
		Short: "Run a command with named secrets injected into its environment only",
		Long: `Decrypts the secrets named by --env and runs <cmd> with them added to the
child process's environment (on top of the inherited environment). The values
are passed only through the child's env — never printed, logged, or placed on a
command line.

Each --env is NAME=<secret-id>: NAME is the environment variable the child
sees, <secret-id> is the id in the local store.

  a-novel secrets exec --env OPENAI_API_KEY=openai-key -- python main.py`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(envPairs) == 0 {
				return errors.New("secrets exec: at least one --env NAME=<secret-id> is required")
			}
			st, err := secrets.Open()
			if err != nil {
				return err
			}
			// Build NAME=value child-env additions. Resolve before exec so a
			// typo'd secret id fails cleanly instead of half-running the command.
			extra := make([]string, 0, len(envPairs))
			for _, pair := range envPairs {
				name, id, ok := strings.Cut(pair, "=")
				if !ok || name == "" || id == "" {
					return fmt.Errorf("secrets exec: --env %q must be NAME=<secret-id>", pair)
				}
				value, found := st.Get(id)
				if !found {
					return fmt.Errorf("secrets exec: no secret named %q (used for %s)", id, name)
				}
				extra = append(extra, name+"="+value)
			}

			child := exec.CommandContext(cmd.Context(), args[0], args[1:]...)
			child.Env = append(os.Environ(), extra...)
			child.Stdin = cmd.InOrStdin()
			child.Stdout = cmd.OutOrStdout()
			child.Stderr = cmd.ErrOrStderr()
			if err := child.Run(); err != nil {
				// Surface the child's exit code without echoing any secret.
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					return &ExitError{Code: exitErr.ExitCode()}
				}
				return fmt.Errorf("secrets exec: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&envPairs, "env", nil, "NAME=<secret-id> to inject into the child env (repeatable)")
	// Stop flag parsing after `--` so the child command's own flags pass through.
	cmd.Flags().SetInterspersed(false)
	return cmd
}
