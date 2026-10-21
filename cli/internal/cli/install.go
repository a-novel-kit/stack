package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

// newInstallCmd rebuilds the CLI and restarts the daemon on the new binary. A
// bare `go install` leaves the old daemon running, so a new CLI talks to an old
// daemon. This verb checkpoints the daemon first, so the same containers and
// go-exec targets end up running on the new binary.
func newInstallCmd() *cobra.Command {
	var source string
	cmd := &cobra.Command{
		Use:   commandInstall,
		Short: "Rebuild and reinstall the CLI, keeping your services up",
		Long: `Build and install the CLI from the default stack's cli/, or from --source, then
restart the daemon on the new binary. The go-exec targets are checkpointed
first and relaunched after, and containers keep running, so your services
stay up.`,
		Example: `  a-novel install
  a-novel install --source ~/forks/stack/cli`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return installCLI(cmd, source)
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "path to the cli source dir (defaults to <default-stack>/cli)")
	return cmd
}

// installCLI checkpoints the daemon, runs `go install` in the CLI source dir,
// and starts a daemon on the new binary, which relaunches the checkpointed
// go-exec targets.
func installCLI(cmd *cobra.Command, source string) error {
	ctx, out := cmd.Context(), cmd.OutOrStdout()
	sourceDir, err := cliSourceDir(source)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "▸ source: %s\n", sourceDir)
	if err := checkpointDaemon(ctx, out); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out, "▸ go install ./cmd/a-novel ...")
	install := exec.CommandContext(ctx, "go", "install", "./cmd/a-novel")
	install.Dir = sourceDir
	install.Stdout = out
	install.Stderr = cmd.ErrOrStderr()
	if err := install.Run(); err != nil {
		return fmt.Errorf("go install: %w", err)
	}
	_, _ = fmt.Fprintln(out, "  ✓ binary built")

	_, _ = fmt.Fprintln(out, "▸ starting fresh daemon...")
	if err := startDetached(); err != nil {
		return fmt.Errorf("daemon start: %w", err)
	}
	_, _ = fmt.Fprintln(out, "\n✓ install complete.")
	return nil
}

// cliSourceDir resolves the CLI source to build: override when set, else the
// default stack's cli/.
func cliSourceDir(override string) (string, error) {
	sourceDir := override
	if sourceDir == "" {
		stack, err := stacks.Default()
		if err != nil {
			return "", fmt.Errorf("resolve cli source dir: %w (pass --source explicitly)", err)
		}
		sourceDir = filepath.Join(stack.Path, "cli")
	}
	if !exists(filepath.Join(sourceDir, "go.mod")) {
		return "", fmt.Errorf("no go.mod at %s — pass --source pointing at the CLI source dir", sourceDir)
	}
	return sourceDir, nil
}

// checkpointDaemon checkpoints a running daemon and waits for it to exit,
// since the old daemon must be gone before `go install` replaces its binary.
func checkpointDaemon(ctx context.Context, out io.Writer) error {
	_, _ = fmt.Fprintln(out, "▸ checkpointing running daemon (if any)...")
	c := rpc.New("")
	if up, _ := daemonUp(ctx, c); !up {
		_, _ = fmt.Fprintln(out, "  ○ no daemon running (skipping checkpoint)")
		return nil
	}
	if err := prepareReinstall(ctx, c, out); err != nil {
		return fmt.Errorf("prepare-reinstall: %w", err)
	}
	if err := waitDaemonGone(ctx, c, out); err != nil {
		_, _ = fmt.Fprintf(out, "  ⚠ %v; continuing anyway\n", err)
	}
	return nil
}
