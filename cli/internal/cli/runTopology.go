package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func newTopologyCmd() *cobra.Command {
	req := &anovelv1.GetTopologyRequest{}
	cmd := &cobra.Command{
		Use:   "topology",
		Short: "Show the dependency graph as ASCII",
		Long: `Print each service's dependency graph as a text tree, read from its compose
depends_on entries: the infra first, then the one-shots, then the
long-runners, each with its kind and what it depends on.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resp, err := rpc.Call(cmd.Context(), rpc.New().GetTopology, req)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), resp.GetRendered())
			return nil
		},
	}
	addStackFlag(cmd, &req.Stack)
	cmd.Flags().StringVar(&req.Service, "service", "", "limit to one service")
	return cmd
}
