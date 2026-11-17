package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func newStartCmd() *cobra.Command {
	var stack string
	mode := rpc.Word(anovelv1.Mode_MODE_GO_EXEC)
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
			req := &anovelv1.StartTargetRequest{
				TargetId: parseEntityID(args[0], stack).ID,
				Mode:     rpc.Parse[anovelv1.Mode](mode),
			}
			resp, err := rpc.Call(cmd.Context(), rpc.New().StartTarget, req)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "started "+targetSummary(resp.GetTarget()))
			return nil
		},
	}
	addStackFlag(cmd, &stack)
	addEnumFlag(cmd, &enumFlag{value: &mode, allowed: modeWords}, "mode", "", "execution mode (go-exec | container)")
	return cmd
}

// targetSummary renders the target a start or a restart answers with: its ID,
// mode and phase, then `pid=<n>` in go-exec mode, or `container=<short>`
// (12-char hex) in container mode, which has no meaningful pid.
func targetSummary(t *anovelv1.Target) string {
	ident := "(no runtime ident)"
	switch t.GetMode() {
	case anovelv1.Mode_MODE_GO_EXEC:
		ident = fmt.Sprintf("pid=%d", t.GetPid())
	case anovelv1.Mode_MODE_CONTAINER:
		ident = "container=" + t.GetContainerId()[:min(12, len(t.GetContainerId()))]
	}
	return fmt.Sprintf("%s (mode=%s, phase=%s, %s)", t.GetId(), rpc.Word(t.GetMode()), rpc.Word(t.GetPhase()), ident)
}
