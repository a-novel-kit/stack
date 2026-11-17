package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func newDebugCmd() *cobra.Command {
	var stack string
	cmd := &cobra.Command{
		Use:   "debug <target>",
		Short: "Print the dlv attach command for a running go-exec target",
		Long: `Print the 'dlv attach' command for a running go-exec target, with its PID and a
headless port. Run it in your own terminal, then connect your IDE to that
port. Container targets are refused.`,
		Example: `  a-novel run debug service-json-keys/grpc
  a-novel run debug default/service-json-keys/grpc`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := parseEntityID(args[0], stack)
			if ref.IsInfra {
				return fmt.Errorf("debug: %s is an infra container; dlv attach is only meaningful on go-exec targets", ref.ID)
			}
			resp, err := rpc.Call(cmd.Context(), rpc.New().Debug, &anovelv1.DebugRequest{TargetId: ref.ID})
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), resp.GetHint())
			return nil
		},
	}
	addStackFlag(cmd, &stack)
	return cmd
}
