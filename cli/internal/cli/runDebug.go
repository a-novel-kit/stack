package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
)

func newDebugCmd() *cobra.Command {
	var ss stackScope
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
			return printDebugHint(cmd, parseEntityID(args[0], ss.stack))
		},
	}
	ss.bind(cmd)
	return cmd
}

// printDebugHint prints the daemon's dlv attach command for the target ref
// names.
func printDebugHint(cmd *cobra.Command, ref entityRef) error {
	if ref.IsInfra {
		return fmt.Errorf("debug: %s is an infra container; dlv attach is only meaningful on go-exec targets", ref.ID)
	}
	resp, err := rpc.New("").Debug(cmd.Context(), ref.ID)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), resp.GetHint())
	return nil
}
