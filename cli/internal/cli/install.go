// `a-novel install` — the safe re-install path.
//
// A bare `go install ./cmd/a-novel` leaves the previous daemon running on the
// old binary, so a new CLI ends up talking to an old daemon and `core status`
// reports a stale `started:` timestamp. This verb replaces both halves at once:
//
//  1. asks the running daemon, if any, to checkpoint via PrepareReinstall;
//  2. runs `go install ./cmd/a-novel` from the resolved CLI source dir;
//  3. starts a fresh daemon, which reads the checkpoint, re-launches the
//     recorded go-exec targets, then deletes it.
//
// The end state is the same containers and go-exec targets running on the new
// binary. It runs from anywhere in the workspace.

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

func newInstallCmd() *cobra.Command {
	var sourceDir string
	cmd := &cobra.Command{
		Use:   commandInstall,
		Short: "Rebuild + reinstall the CLI and restart the daemon (state-preserving)",
		Long: `One command for the dev-loop reinstall cycle. Equivalent to manually
running:

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
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			// 1. Resolve the source dir.
			if sourceDir == "" {
				resolved, err := defaultCLISource()
				if err != nil {
					return fmt.Errorf("resolve cli source dir: %w (pass --source explicitly)", err)
				}
				sourceDir = resolved
			}
			// Sanity-check: a go.mod must live at <source>/go.mod.
			if _, err := os.Stat(filepath.Join(sourceDir, goModFile)); err != nil {
				return fmt.Errorf("no go.mod at %s — pass --source pointing at the CLI source dir", sourceDir)
			}
			_, _ = fmt.Fprintf(out, "▸ source: %s\n", sourceDir)

			// 2. Checkpoint the running daemon (if any). PrepareReinstall
			// is idempotent and silently no-ops when no daemon is up.
			_, _ = fmt.Fprintln(out, "▸ checkpointing running daemon (if any)...")
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			daemonRunning := false
			if _, err := rpc.New("").Ping(ctx); err == nil {
				daemonRunning = true
			}
			cancel()
			if daemonRunning {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				resp, err := rpc.New("").PrepareReinstall(ctx)
				cancel()
				if err != nil {
					return fmt.Errorf("prepare-reinstall: %w", err)
				}
				_, _ = fmt.Fprintf(out, "  ✓ checkpoint written: %s (%d go-exec target(s))\n",
					resp.GetCheckpointPath(), resp.GetGoExecTargetCount())
				// Wait for the daemon to actually exit so the next
				// `go install` doesn't race the running binary.
				if err := waitDaemonGone(5 * time.Second); err != nil {
					_, _ = fmt.Fprintf(out, "  ⚠ daemon still responding after %v; continuing anyway\n", 5*time.Second)
				}
			} else {
				_, _ = fmt.Fprintln(out, "  ○ no daemon running (skipping checkpoint)")
			}

			// 3. go install.
			_, _ = fmt.Fprintln(out, "▸ go install ./cmd/a-novel ...")
			install := exec.Command("go", commandInstall, "./cmd/a-novel")
			install.Dir = sourceDir
			install.Stdout = out
			install.Stderr = cmd.ErrOrStderr()
			install.Env = os.Environ()
			if err := install.Run(); err != nil {
				return fmt.Errorf("go install: %w", err)
			}
			_, _ = fmt.Fprintln(out, "  ✓ binary built")

			// 4. Start the new daemon. startDetached is idempotent
			// against an already-running daemon (pings first), so a
			// no-op race is safe. The new daemon will read the
			// reinstall checkpoint and relaunch go-exec targets.
			_, _ = fmt.Fprintln(out, "▸ starting fresh daemon...")
			if err := startDetached(); err != nil {
				return fmt.Errorf("daemon start: %w", err)
			}
			_, _ = fmt.Fprintln(out, "")
			_, _ = fmt.Fprintln(out, "✓ install complete.")
			return nil
		},
	}
	cmd.Flags().StringVar(&sourceDir, "source", "", "path to the cli source dir (defaults to <default-stack>/cli)")
	return cmd
}

// defaultStackPath returns the filesystem root of the default stack — the
// first entry of $A_NOVEL_STACKS, per the stacks.Parse contract. Errors only
// when parsing itself fails; whether the path exists on disk is the caller's
// question to ask, since each one has a better error to give for it.
func defaultStackPath() (string, error) {
	stk, err := stacks.ParseEnv()
	if err != nil {
		return "", err
	}
	if len(stk) == 0 {
		return "", errors.New("no stacks registered")
	}
	return stk[0].Path, nil
}

// defaultCLISource returns "<default-stack-path>/cli". A missing path on disk
// is caught later by the go.mod stat.
func defaultCLISource() (string, error) {
	root, err := defaultStackPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "cli"), nil
}

// waitDaemonGone polls the socket until Ping fails — i.e., the daemon
// process has actually exited. Bounded by timeout. Returns nil even on
// timeout; callers decide whether the gap matters.
func waitDaemonGone(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, err := rpc.New("").Ping(ctx)
		cancel()
		if err != nil {
			// A Ping error means the socket is closed and the daemon
			// has stopped, which is the condition being polled for.
			// Report success.
			return nil //nolint:nilerr // intentional: error here means daemon is gone
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon still responding after %v", timeout)
}
