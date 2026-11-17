package server

import (
	"context"

	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	"github.com/a-novel-kit/stack/cli/internal/shared/enumword"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// Watch streams every phase transition the runner emits, filtered by the
// caller's (stack, service, target_id) tuple. An empty filter field matches any
// value, so (stack, service, "") watches one service and three empty fields
// watch everything.
//
// The stream stays open until the client cancels its side or the daemon stops.
// Nothing is emitted up front, so a caller needing the initial state pairs
// Watch with a ListServices snapshot.
func (s *Server) Watch(ctx context.Context, req *anovelv1.WatchRequest, stream anovelv1connect.CoreServiceWatchServerStream) error {
	ctx, cancel := s.streamContext(ctx)
	defer cancel()
	filter := s.watchFilter(req)
	ch, unsub := s.runner.SubscribePhases(func(ev runner.PhaseEvent) bool { return watches(filter, ev) })
	defer unsub()
	for {
		select {
		case <-ctx.Done():
			return streamEnd(ctx)
		case ev, ok := <-ch:
			if !ok {
				return nil // runner closed our subscription
			}
			if err := stream.Send(stateEventToProto(ev)); err != nil {
				return err
			}
		}
	}
}

// watchFilter is req with its target ID resolved like every other request's
// (see canonicalID), since events carry canonical IDs.
func (s *Server) watchFilter(req *anovelv1.WatchRequest) *anovelv1.WatchRequest {
	return &anovelv1.WatchRequest{Stack: req.GetStack(), Service: req.GetService(), TargetId: s.canonicalID(req.GetTargetId())}
}

// watches reports whether a Watch request covers ev: every field the request
// sets must match, and the stack "*" matches any stack.
func watches(req *anovelv1.WatchRequest, ev runner.PhaseEvent) bool {
	stack, service, targetID := req.GetStack(), req.GetService(), req.GetTargetId()
	return (stack == "" || stack == allStacks || ev.Stack == stack) &&
		(service == "" || ev.Service == service) &&
		(targetID == "" || ev.TargetID == targetID)
}

// describePhaseEvent renders the one-line summary WatchResponse.description
// carries, such as "<id> running" or "<id> terminated (killed)".
func describePhaseEvent(ev runner.PhaseEvent) string {
	description := ev.TargetID + " " + enumword.Word(ev.NewPhase)
	if ev.NewPhase == anovelv1.Phase_PHASE_TERMINATED && ev.ExitReason != anovelv1.ExitReason_EXIT_REASON_UNSPECIFIED {
		description += " (" + enumword.Word(ev.ExitReason) + ")"
	}
	return description
}
