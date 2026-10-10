// `a-novel install` — the safe re-install path.
//
// A bare `go install ./cmd/a-novel` leaves the previous daemon running on the
// old binary, so a new CLI talks to an old daemon. This verb replaces both: it
// checkpoints the running daemon, rebuilds the binary, and starts a daemon that
// relaunches the checkpointed go-exec targets, so the same containers and
// targets end up running on the new binary.

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

func newInstallCmd() *cobra.Command {
	var sourceDir string
	cmd := &cobra.Command{
		Use:   commandInstall,
		Short: "Rebuild + reinstall the CLI and restart the daemon (state-preserving)",
		Long: `One command for the dev-loop reinstall cycle. Equivalent to running:

  1. a-novel core prepare-reinstall   (checkpoint daemon state)
  2. cd <cli-source>; go install ./cmd/a-novel
  3. a-novel core start               (replays checkpoint, deletes it)

The source dir defaults to '<default-stack>/cli' (typically
~/git-projects/a-novel/cli). Pass --source to override.

After this command exits, 'a-novel core status' shows the freshly-built
binary's version and the same containers / go-exec targets that were
running before.`,
		Example: `  a-novel install
  a-novel install --source ~/forks/stack/cli`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, out := cmd.Context(), cmd.OutOrStdout()
			if sourceDir == "" {
				stack, err := stacks.Default()
				if err != nil {
					return fmt.Errorf("resolve cli source dir: %w (pass --source explicitly)", err)
				}
				sourceDir = filepath.Join(stack.Path, "cli")
			}
			if !exists(filepath.Join(sourceDir, "go.mod")) {
				return fmt.Errorf("no go.mod at %s — pass --source pointing at the CLI source dir", sourceDir)
			}
			_, _ = fmt.Fprintf(out, "▸ source: %s\n", sourceDir)

			_, _ = fmt.Fprintln(out, "▸ checkpointing running daemon (if any)...")
			c := rpc.New("")
			if up, _ := daemonUp(ctx, c); up {
				if err := prepareReinstall(ctx, c, out); err != nil {
					return fmt.Errorf("prepare-reinstall: %w", err)
				}
				// The old daemon must exit before `go install` replaces its binary.
				if err := waitDaemonGone(ctx, c, out); err != nil {
					_, _ = fmt.Fprintf(out, "  ⚠ %v; continuing anyway\n", err)
				}
			} else {
				_, _ = fmt.Fprintln(out, "  ○ no daemon running (skipping checkpoint)")
			}

			_, _ = fmt.Fprintln(out, "▸ go install ./cmd/a-novel ...")
			install := exec.CommandContext(ctx, "go", commandInstall, "./cmd/a-novel")
			install.Dir = sourceDir
			install.Stdout = out
			install.Stderr = cmd.ErrOrStderr()
			install.Env = os.Environ()
			if err := install.Run(); err != nil {
				return fmt.Errorf("go install: %w", err)
			}
			_, _ = fmt.Fprintln(out, "  ✓ binary built")

			// The new daemon reads the reinstall checkpoint and relaunches the
			// go-exec targets.
			_, _ = fmt.Fprintln(out, "▸ starting fresh daemon...")
			if err := startDetached(); err != nil {
				return fmt.Errorf("daemon start: %w", err)
			}
			_, _ = fmt.Fprintln(out, "\n✓ install complete.")
			return nil
		},
	}
	cmd.Flags().StringVar(&sourceDir, "source", "", "path to the cli source dir (defaults to <default-stack>/cli)")
	return cmd
}
