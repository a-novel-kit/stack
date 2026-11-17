package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
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
