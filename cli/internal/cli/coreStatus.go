package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
)

func newCoreStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether the daemon runs, with its socket, stacks and uptime",
		Long: `Print whether the daemon runs, with its version, socket, uptime and stacks,
and whether a reinstall checkpoint waits. Each problem discovery found in a
stack follows its row, marked '!'. A stopped daemon is a normal state, and the
command exits 0 for it.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return printDaemonStatus(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

// printDaemonStatus prints whether the daemon runs and, when it does, its
// version, socket, uptime and stacks, the default one starred and each one
// followed by its discovery problems.
func printDaemonStatus(ctx context.Context, out io.Writer) error {
	c := rpc.New("")
	s, err := c.Status(ctx)
	if rpc.IsNotRunning(err) {
		_, _ = fmt.Fprintln(out, "a-novel daemon: not running")
		_, _ = fmt.Fprintf(out, "  socket: %s (no listener)\n", c.SocketPath())
		return nil
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, "a-novel daemon: running")
	_, _ = fmt.Fprintf(out, "  version: %s\n", s.GetDaemonVersion())
	_, _ = fmt.Fprintf(out, "  socket : %s\n", s.GetSocketPath())
	_, _ = fmt.Fprintf(out, "  started: %s (%.0fs ago)\n",
		s.GetStartedAt().AsTime().Format(time.RFC3339), s.GetUptime().AsDuration().Seconds())
	_, _ = fmt.Fprintf(out, "  stacks : %d\n", len(s.GetStacks()))
	for _, st := range s.GetStacks() {
		marker := " "
		if st.GetIsDefault() {
			marker = "*"
		}
		_, _ = fmt.Fprintf(out, "    %s %s\t%s\n", marker, st.GetName(), st.GetPath())
		printDiscoveryErrors(out, "        ", st)
	}
	if s.GetReinstallCheckpointPending() {
		_, _ = fmt.Fprintln(out, "  reinstall checkpoint: pending")
	}
	return nil
}
