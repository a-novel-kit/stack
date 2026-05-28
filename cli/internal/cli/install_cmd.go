// `a-novel install` — the safe re-install path.
//
// Why this exists: running `go install ./cmd/a-novel` directly leaves the
// previous daemon process running (with the OLD binary). The user then
// sees inconsistent behavior — new CLI talking to old daemon — and
// `core status` shows the old version's `started:` timestamp.
//
// `a-novel install` is the all-in-one wrapper that:
//
//  1. asks the running daemon (if any) to checkpoint via PrepareReinstall;
//  2. runs `go install ./cmd/a-novel` from the resolved CLI source dir;
//  3. starts a fresh daemon — which reads the checkpoint and re-launches
//     the recorded go-exec targets, then deletes the checkpoint.
//
// End-state: same containers + same go-exec targets running, but now on
// the NEW binary. Mirrors what scripts/install.sh does, but invokable
// from anywhere (not just inside the cli/ dir) and without remembering
// the script path.

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
		Use:   "install",
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
			if _, err := os.Stat(filepath.Join(sourceDir, "go.mod")); err != nil {
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
			install := exec.Command("go", "install", "./cmd/a-novel")
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

// defaultCLISource returns "<default-stack-path>/cli" from the first
// registered stack. Errors only when stack parsing itself fails — a
// missing path on disk is caught later by the go.mod stat.
func defaultCLISource() (string, error) {
	stk, err := stacks.ParseEnv()
	if err != nil {
		return "", err
	}
	if len(stk) == 0 {
		return "", errors.New("no stacks registered")
	}
	// First entry is always the default per stacks.Parse contract.
	return filepath.Join(stk[0].Path, "cli"), nil
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
			// Ping error == socket closed == daemon stopped, which IS
			// the success condition we're polling for. Returning the
			// transport error would mean "couldn't talk to daemon" =
			// failure, the opposite of what we want.
			return nil //nolint:nilerr // intentional: error here means daemon is gone
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon still responding after %v", timeout)
}
