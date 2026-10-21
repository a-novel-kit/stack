package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func newStartCmd() *cobra.Command {
	var ss stackScope
	var mode string
	cmd := &cobra.Command{
		Use:   "start <target>",
		Short: "Start a target in go-exec (default) or container mode",
		Long: `Start a target, named '<stack>/<service>/<name>', or '<service>/<name>' in the
--stack or default stack. Its dependencies come up first: the infra waits for
health, and one-shots such as migrations run to success.

The target runs from source (go-exec), or from its image with --mode=container.
A target running in the other mode is refused, and one running in the same
mode is left as it is.`,
		Example: `  a-novel run start default/service-json-keys/rest
  a-novel run start service-json-keys/rest                     # default stack inferred
  a-novel run start service-json-keys/rest --mode=container
  a-novel run start service-template/rest --stack=branch-foo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return startTarget(cmd, parseEntityID(args[0], ss.stack), mode)
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&mode, "mode", modeGoExec, "execution mode (go-exec | container)")
	return cmd
}

// startTarget starts the target ref names, in the mode the --mode token names.
func startTarget(cmd *cobra.Command, ref entityRef, mode string) error {
	resp, err := rpc.New("").StartTarget(cmd.Context(), ref.ID, parseMode(mode, anovelv1.Mode_MODE_GO_EXEC))
	if err != nil {
		return err
	}
	t := resp.GetTarget()
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "started %s (mode=%s, phase=%s, %s)\n",
		t.GetId(), modeLabel(t.GetMode()), phaseLabel(t.GetPhase()), runtimeIdentLabel(t))
	return nil
}

// runtimeIdentLabel renders the right-side identifier for `start`/`restart`
// output: `pid=<n>` in go-exec mode, `container=<short>` (12-char hex) in
// container mode, which has no meaningful pid.
func runtimeIdentLabel(t *anovelv1.Target) string {
	switch t.GetMode() {
	case anovelv1.Mode_MODE_GO_EXEC:
		return fmt.Sprintf("pid=%d", t.GetPid())
	case anovelv1.Mode_MODE_CONTAINER:
		cid := t.GetContainerId()
		if len(cid) > 12 {
			cid = cid[:12]
		}
		return "container=" + cid
	default:
		return "(no runtime ident)"
	}
}

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
