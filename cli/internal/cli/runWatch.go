package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// watchOpts holds the flags of `run watch`.
type watchOpts struct {
	scope   stackScope
	service string
	target  string
	format  string
}

func newWatchCmd() *cobra.Command {
	o := watchOpts{format: formatJSON}
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream the targets' phase changes",
		Long: `Print each phase change of the daemon's targets as it happens, one JSON object
per line, or one line of text with --format=text. --service and --target
narrow the stream.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return watchPhases(cmd, o)
		},
	}
	o.scope.bind(cmd)
	cmd.Flags().StringVar(&o.service, "service", "", "filter to one service")
	cmd.Flags().StringVar(&o.target, "target", "", "filter to one target")
	addEnumFlag(cmd, &enumFlag{value: &o.format, allowed: []string{formatJSON, formatText}}, "format", "",
		"output format (json | text)")
	addJSONAlias(cmd, &o.format)
	return cmd
}

// watchPhases prints each state event of the targets the flags select, as a
// JSON line or as text, until the command's context ends.
func watchPhases(cmd *cobra.Command, o watchOpts) error {
	out := cmd.OutOrStdout()
	handle := func(ev *anovelv1.WatchResponse) { printEvent(out, ev) }
	if o.format == formatJSON {
		enc := json.NewEncoder(out)
		handle = func(ev *anovelv1.WatchResponse) { encodeEvent(enc, ev) }
	}
	req := &anovelv1.WatchRequest{Stack: o.scope.selector(), Service: o.service, TargetId: o.target}
	return watchEvents(cmd.Context(), rpc.New(), req, handle)
}

// watchEvents streams the daemon's state events to handle until ctx ends.
func watchEvents(ctx context.Context, c anovelv1connect.CoreServiceClient, req *anovelv1.WatchRequest, handle func(*anovelv1.WatchResponse)) error {
	for ev, err := range rpc.Stream(ctx, c.Watch, req) {
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		handle(ev)
	}
	return nil
}

// printEvent writes a state event as one line of text: its time and its
// description.
func printEvent(w io.Writer, ev *anovelv1.WatchResponse) {
	_, _ = fmt.Fprintf(w, "%s  %s\n", ev.GetTs().AsTime().Format("15:04:05.000"), ev.GetDescription())
}

// watchEvent is the JSON line of a state event. Its fields stay in alphabetical
// order, the key order the command has always printed.
type watchEvent struct {
	Description string `json:"description"`
	NewPhase    string `json:"new_phase"`
	OldPhase    string `json:"old_phase"`
	Service     string `json:"service"`
	Stack       string `json:"stack"`
	TargetID    string `json:"target_id"`
	Ts          string `json:"ts"`
}

// encodeEvent writes a state event as one JSON line.
func encodeEvent(enc *json.Encoder, ev *anovelv1.WatchResponse) {
	_ = enc.Encode(watchEvent{
		Description: ev.GetDescription(),
		NewPhase:    ev.GetNewPhase().String(),
		OldPhase:    ev.GetOldPhase().String(),
		Service:     ev.GetService(),
		Stack:       ev.GetStack(),
		TargetID:    ev.GetTargetId(),
		Ts:          ev.GetTs().AsTime().Format(time.RFC3339Nano),
	})
}
