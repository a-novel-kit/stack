package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// watchOpts holds the flags of `run watch`.
type watchOpts struct {
	scope   stackScope
	service string
	target  string
	jsonOut bool
}

func newWatchCmd() *cobra.Command {
	var o watchOpts
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream the targets' phase changes",
		Long: `Print each phase change of the daemon's targets as it happens, one JSON object
per line, or one line of text with --json=false. --service and --target narrow
the stream.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return watchPhases(cmd, o)
		},
	}
	o.scope.bindAll(cmd)
	cmd.Flags().StringVar(&o.service, "service", "", "filter to one service")
	cmd.Flags().StringVar(&o.target, "target", "", "filter to one target")
	cmd.Flags().BoolVar(&o.jsonOut, "json", true, "print JSON lines (--json=false prints text)")
	return cmd
}

// watchPhases prints each state event of the targets the flags select, as a
// JSON line or as text, until the command's context ends.
func watchPhases(cmd *cobra.Command, o watchOpts) error {
	out := cmd.OutOrStdout()
	write := printEvent
	if o.jsonOut {
		write = encodeEvent
	}
	handle := func(ev *anovelv1.WatchResponse) { write(out, ev) }
	return watchEvents(cmd.Context(), rpc.New(""), o.scope.selector(), o.service, o.target, handle)
}

// watchEvents streams the daemon's state events to handle until ctx ends.
func watchEvents(ctx context.Context, c *rpc.Client, stack, service, target string, handle func(*anovelv1.WatchResponse)) error {
	stream, err := c.Watch(ctx, stack, service, target)
	if err != nil {
		return err
	}
	for ev, err := range stream {
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

// encodeEvent writes a state event as one JSON line.
func encodeEvent(w io.Writer, ev *anovelv1.WatchResponse) {
	event := map[string]any{
		"ts":          ev.GetTs().AsTime().Format(time.RFC3339Nano),
		stackLabel:    ev.GetStack(),
		"service":     ev.GetService(),
		"target_id":   ev.GetTargetId(),
		"old_phase":   ev.GetOldPhase().String(),
		"new_phase":   ev.GetNewPhase().String(),
		"description": ev.GetDescription(),
	}
	_ = json.NewEncoder(w).Encode(event)
}
