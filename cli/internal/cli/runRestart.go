package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func newRestartCmd() *cobra.Command {
	var ss stackScope
	var mode string
	cmd := &cobra.Command{
		Use:   "restart <target-or-infra>",
		Short: "Kill then start (optionally swapping mode for targets)",
		Long: `Stop then start a target in one step, in the mode --mode names, or its current
one. An infra container gets 'podman restart', which keeps the container and
its data.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return restartEntity(cmd, parseEntityID(args[0], ss.stack), mode)
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&mode, "mode", "", "switch to this mode (go-exec | container); empty keeps current")
	return cmd
}

// restartEntity restarts the target ref names, in the mode the --mode token
// names or its current one, or restarts the infra container in place.
func restartEntity(cmd *cobra.Command, ref entityRef, mode string) error {
	ctx, out, c := cmd.Context(), cmd.OutOrStdout(), rpc.New("")
	if ref.IsInfra {
		if _, err := c.RestartInfraContainer(ctx, ref.Stack, ref.Service, ref.Infra); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "restarted infra %s\n", ref.ID)
		return nil
	}
	resp, err := c.RestartTarget(ctx, ref.ID, parseMode(mode, anovelv1.Mode_MODE_UNSPECIFIED))
	if err != nil {
		return err
	}
	t := resp.GetTarget()
	_, _ = fmt.Fprintf(out, "restarted %s (mode=%s, phase=%s, %s)\n",
		t.GetId(), modeLabel(t.GetMode()), phaseLabel(t.GetPhase()), runtimeIdentLabel(t))
	return nil
}
