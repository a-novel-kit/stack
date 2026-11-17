package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func newRestartCmd() *cobra.Command {
	var stack string
	var mode string
	cmd := &cobra.Command{
		Use:   "restart <target-or-infra>",
		Short: "Kill then start (optionally swapping mode for targets)",
		Long: `Stop then start a target in one step, in the mode --mode names, or its current
one. An infra container gets 'podman restart', which keeps the container and
its data.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return restartEntity(cmd, parseEntityID(args[0], stack), mode)
		},
	}
	addStackFlag(cmd, &stack)
	addEnumFlag(cmd, &enumFlag{value: &mode, allowed: modeWords}, "mode", "",
		"switch to this mode (go-exec | container); empty keeps current")
	return cmd
}

// restartEntity restarts the target ref names, in the mode the --mode word
// names or its current one, or restarts the infra container in place.
func restartEntity(cmd *cobra.Command, ref entityRef, mode string) error {
	ctx, out, c := cmd.Context(), cmd.OutOrStdout(), rpc.New()
	if ref.IsInfra {
		req := &anovelv1.RestartInfraContainerRequest{Stack: ref.Stack, Service: ref.Service, Name: ref.Infra}
		resp, err := rpc.Call(ctx, c.RestartInfraContainer, req)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "restarted infra %s\n", infraID(resp.GetInfra()))
		return nil
	}

	req := &anovelv1.RestartTargetRequest{TargetId: ref.ID, Mode: rpc.Parse[anovelv1.Mode](mode)}
	resp, err := rpc.Call(ctx, c.RestartTarget, req)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, "restarted "+targetSummary(resp.GetTarget()))
	return nil
}
