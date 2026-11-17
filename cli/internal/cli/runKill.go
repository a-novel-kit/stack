package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
	"github.com/a-novel-kit/stack/cli/internal/shared/enumword"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func newKillCmd() *cobra.Command {
	var stack string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "kill <target-or-infra>",
		Short: "Stop a running target or infra container",
		Long: `Stop a target, named as 'run start' takes it, or an infra container, named
'<service>/infra/<name>'. A target gets SIGTERM, then SIGKILL once --timeout
passes, and an infra container gets 'podman stop'. Stopping what already
stopped succeeds.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return killEntity(cmd, parseEntityID(args[0], stack), timeout)
		},
	}
	addStackFlag(cmd, &stack)
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "SIGTERM-to-SIGKILL grace (0: the daemon's default, 10s)")
	return cmd
}

// killEntity stops the target or the infra container ref names. A target gets
// timeout between SIGTERM and SIGKILL, or the daemon's default when it is 0.
func killEntity(cmd *cobra.Command, ref entityRef, timeout time.Duration) error {
	ctx, out, c := cmd.Context(), cmd.OutOrStdout(), rpc.New()
	if ref.IsInfra {
		req := &anovelv1.KillInfraContainerRequest{Stack: ref.Stack, Service: ref.Service, Name: ref.Infra}
		resp, err := rpc.Call(ctx, c.KillInfraContainer, req)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "stopped infra %s\n", infraID(resp.GetInfra()))
		return nil
	}

	req := &anovelv1.KillTargetRequest{TargetId: ref.ID}
	if timeout > 0 {
		req.Timeout = durationpb.New(timeout)
	}
	resp, err := rpc.Call(ctx, c.KillTarget, req)
	if err != nil {
		return err
	}
	t := resp.GetTarget()
	if t.GetId() == "" {
		_, _ = fmt.Fprintf(out, "%s: not running (no-op)\n", ref.ID)
		return nil
	}
	_, _ = fmt.Fprintf(out, "killed %s (phase=%s, exit=%s)\n", t.GetId(), enumword.Word(t.GetPhase()), enumword.Word(t.GetExitReason()))
	return nil
}
