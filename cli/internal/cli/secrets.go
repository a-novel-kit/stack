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

// readPassword reads a line from the terminal with echo disabled. A package var
// so tests can stub it; production reads via golang.org/x/term so the typed
// secret never appears on screen.
var readPassword = func() ([]byte, error) { return term.ReadPassword(int(os.Stdin.Fd())) }

// newSecretsCmd manages the local encrypted secrets the secrets package stores.
// A secret reaches a child process through its environment only, so it never
// shows on a terminal, in a log, in git or as a CLI argument.
func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage local, encrypted secrets passed to programs through their environment",
		Long: `Keep secrets, such as API keys, encrypted on this machine with AES-256-GCM, and
pass them to programs through their environment only. No command prints a
value.

A repository lists the secrets it needs in .a-novel/secrets.yaml, as
environment variables mapped to secret IDs, without values. 'a-novel test',
'a-novel build' and the targets the daemon runs receive them, and a missing
one prints a warning that names it.`,
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
		Short: "Create the local secrets key and store directory",
		Long: `Create the secrets directory and a 32-byte local key, both readable by you
alone. An existing key stays as it is.`,
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
		Short: "Store a secret value, typed with no echo",
		Long: `Read the value of <id> from the terminal with echo off, and store it
encrypted. The command refuses to run without a terminal, so a value never
passes through a pipe, a log or a shell history.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if strings.TrimSpace(id) == "" {
				return errors.New("secrets set: <id> must not be empty")
			}
			// A secret must never reach a terminal, a log or a shell history.
			// Reading it with echo disabled takes a real terminal, so a
			// non-interactive stdin is refused.
			if !stdinIsTTY() {
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
		Short: "Run a command with named secrets in its environment",
		Long: `Run <cmd> with each --env NAME=<secret-id> added to its environment, NAME
holding the value of that secret. The values reach the command through its
environment alone.`,
		Example: `  a-novel secrets exec --env OPENAI_API_KEY=openai-key -- python main.py`,
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(envPairs) == 0 {
				return errors.New("secrets exec: at least one --env NAME=<secret-id> is required")
			}
			extra, err := secretEnv(envPairs)
			if err != nil {
				return err
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

// secretEnv resolves each NAME=<secret-id> pair into a NAME=value child-env
// entry. Every id resolves before the child starts, so a typo fails first.
func secretEnv(pairs []string) ([]string, error) {
	st, err := secrets.Open()
	if err != nil {
		return nil, err
	}
	env := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		name, id, ok := strings.Cut(pair, "=")
		if !ok || name == "" || id == "" {
			return nil, fmt.Errorf("secrets exec: --env %q must be NAME=<secret-id>", pair)
		}
		value, found := st.Get(id)
		if !found {
			return nil, fmt.Errorf("secrets exec: no secret named %q (used for %s)", id, name)
		}
		env = append(env, name+"="+value)
	}
	return env, nil
}
