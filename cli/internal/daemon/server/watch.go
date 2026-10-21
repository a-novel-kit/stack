package server

import (
	"context"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// Watch streams every phase transition the runner emits, filtered by the
// caller's (stack, service, target_id) tuple. An empty filter field matches any
// value, so (stack, service, "") watches one service and three empty fields
// watch everything.
//
// The stream stays open until the client cancels its side. Nothing is emitted
// up front, so a caller needing the initial state pairs Watch with a
// ListServices snapshot.
func (s *Server) Watch(ctx context.Context, req *anovelv1.WatchRequest, stream anovelv1connect.CoreServiceWatchServerStream) error {
	ch, unsub := s.runner.SubscribePhases(func(ev runner.PhaseEvent) bool { return watches(req, ev) })
	defer unsub()
	for {
		select {
		case <-ctx.Done():
			return nil
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

// watches reports whether a Watch request covers ev: every field the request
// sets must match, and the stack "*" matches any stack.
func watches(req *anovelv1.WatchRequest, ev runner.PhaseEvent) bool {
	stack, service, targetID := req.GetStack(), req.GetService(), req.GetTargetId()
	return (stack == "" || stack == allStacks || ev.Stack == stack) &&
		(service == "" || ev.Service == service) &&
		(targetID == "" || ev.TargetID == targetID)
}

// describePhaseEvent renders the one-line summary WatchResponse.description
// carries, such as "<id> running" or "<id> terminated (killed)". A phase with
// no summary, such as PENDING, renders as its enum name.
func describePhaseEvent(ev runner.PhaseEvent) string {
	switch ev.NewPhase {
	case anovelv1.Phase_PHASE_STARTING, anovelv1.Phase_PHASE_RUNNING, anovelv1.Phase_PHASE_STOPPING:
		return ev.TargetID + " " + enumWord(ev.NewPhase.String(), "PHASE_")
	case anovelv1.Phase_PHASE_TERMINATED:
		if ev.ExitReason == anovelv1.ExitReason_EXIT_REASON_UNSPECIFIED {
			return ev.TargetID + " terminated"
		}
		return ev.TargetID + " terminated (" + enumWord(ev.ExitReason.String(), "EXIT_REASON_") + ")"
	default:
		return ev.TargetID + " " + ev.NewPhase.String()
	}
}

// enumWord turns a proto enum name into the lowercase word after its prefix,
// as PHASE_RUNNING becomes "running".
func enumWord(name, prefix string) string {
	return strings.ToLower(strings.TrimPrefix(name, prefix))
}
