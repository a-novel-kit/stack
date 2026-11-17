package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
)

func newCoreKillCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "kill",
		Short: "Stop the a-novel daemon",
		Long: `Stop the daemon and the go-exec targets it runs, each with a 10s grace.
Containers keep running. --force also tears down every service's infra,
stopping the targets left first. The next 'core start' relaunches nothing;
'core restart --preserve-targets' brings the go-exec targets back.`,
		Example: `  a-novel core kill
  a-novel core kill --force`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return killDaemon(cmd, force)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "also tear down all infra (cascade-kill remaining targets)")
	return cmd
}

// killDaemon stops a running daemon and waits for it to exit. force also tears
// down every service's infra. A target the shutdown could not stop fails the
// command, so a script learns its environment is not clean.
func killDaemon(cmd *cobra.Command, force bool) error {
	ctx, out := cmd.Context(), cmd.OutOrStdout()
	c := rpc.New("")
	if up, err := daemonUp(ctx, c); !up {
		if err == nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "a-novel: daemon not running")
		}
		return err
	}
	resp, err := shutdown(ctx, c, force, out)
	if err != nil {
		return err
	}
	// The daemon exits even after a failure, so it is waited on either way.
	waitErr := waitDaemonGone(ctx, c, out)
	if fails := resp.GetFailures(); len(fails) > 0 {
		for _, f := range fails {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "a-novel: shutdown could not stop %s\n", f)
		}
		return fmt.Errorf("shutdown left %d target(s) running", len(fails))
	}
	return waitErr
}
