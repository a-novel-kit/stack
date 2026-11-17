package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
)

func newKillCmd() *cobra.Command {
	var ss stackScope
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
			return killEntity(cmd, parseEntityID(args[0], ss.stack), timeout)
		},
	}
	ss.bind(cmd)
	cmd.Flags().DurationVar(&timeout, "timeout", killGrace, "SIGTERM-to-SIGKILL grace")
	return cmd
}

// killEntity stops the target or the infra container ref names. A target gets
// timeout between SIGTERM and SIGKILL.
func killEntity(cmd *cobra.Command, ref entityRef, timeout time.Duration) error {
	ctx, out, c := cmd.Context(), cmd.OutOrStdout(), rpc.New("")
	if ref.IsInfra {
		if _, err := c.KillInfraContainer(ctx, ref.Stack, ref.Service, ref.Infra); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "stopped infra %s\n", ref.ID)
		return nil
	}
	resp, err := c.KillTarget(ctx, ref.ID, timeout)
	if err != nil {
		return err
	}
	t := resp.GetTarget()
	if t == nil || t.GetId() == "" {
		_, _ = fmt.Fprintf(out, "%s: not running (no-op)\n", ref.ID)
		return nil
	}
	_, _ = fmt.Fprintf(out, "killed %s (phase=%s, exit=%s)\n",
		t.GetId(), phaseLabel(t.GetPhase()), exitLabel(t.GetExitReason()))
	return nil
}
