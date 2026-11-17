package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
)

func newTopologyCmd() *cobra.Command {
	var ss stackScope
	var service string
	cmd := &cobra.Command{
		Use:   "topology",
		Short: "Show the dependency graph as ASCII",
		Long: `Print each service's dependency graph as a text tree, read from its compose
depends_on entries: the infra first, then the one-shots, then the
long-runners, each with its kind and what it depends on.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return printTopology(cmd, ss.stack, service)
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&service, "service", "", "limit to one service")
	return cmd
}

// printTopology prints the dependency graph of one service, or of every
// service of the stack when service is empty.
func printTopology(cmd *cobra.Command, stack, service string) error {
	resp, err := rpc.New("").GetTopology(cmd.Context(), stack, service)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprint(cmd.OutOrStdout(), resp.GetRendered())
	return nil
}
