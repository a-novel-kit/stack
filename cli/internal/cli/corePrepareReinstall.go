package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
)

// prepareReinstall asks the daemon to checkpoint its go-exec targets and exit.
func prepareReinstall(ctx context.Context, c *rpc.Client, out io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := c.PrepareReinstall(ctx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "checkpoint written: %s (%d go-exec target(s))\n",
		resp.GetCheckpointPath(), resp.GetGoExecTargetCount())
	return nil
}

func newCorePrepareReinstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prepare-reinstall",
		Short: "Checkpoint the go-exec targets and stop the daemon",
		Long: `Checkpoint the running go-exec targets and stop the daemon. Containers keep
running. The next 'core start' relaunches the checkpointed targets on their
ports, then deletes the checkpoint. 'a-novel install' and
'core restart --preserve-targets' run this step for you.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return stopWithCheckpoint(cmd)
		},
	}
}

// stopWithCheckpoint has a running daemon checkpoint its go-exec targets and
// exit, and waits until it is gone.
func stopWithCheckpoint(cmd *cobra.Command) error {
	ctx, out := cmd.Context(), cmd.OutOrStdout()
	c := rpc.New("")
	if up, err := daemonUp(ctx, c); !up {
		if err == nil {
			_, _ = fmt.Fprintln(cmd.OutOrStderr(), "a-novel: daemon not running (nothing to checkpoint)")
		}
		return err
	}
	if err := prepareReinstall(ctx, c, out); err != nil {
		return err
	}
	return waitDaemonGone(ctx, c, out)
}
