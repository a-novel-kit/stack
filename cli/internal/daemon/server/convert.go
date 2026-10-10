package server

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// stacksToProto lists the discovered stacks, the ones the daemon manages, in
// registration order, so the default stack comes first.
func stacksToProto(stacks discovery.Stacks) []*anovelv1.Stack {
	out := make([]*anovelv1.Stack, 0, len(stacks))
	for _, st := range stacks {
		out = append(out, &anovelv1.Stack{Name: st.Name, Path: st.Path, IsDefault: st.Default})
	}
	return out
}

// serviceToProto converts a discovery.Service into the proto Service, embedding
// its targets with whatever live state the runner holds, plus its infra and
// volumes. infraStates is the stack's live-state map from liveInfraStates.
func (s *Server) serviceToProto(svc *discovery.Service, infraStates map[string]runner.InfraState) *anovelv1.Service {
	out := &anovelv1.Service{
		Name:            svc.Name,
		Stack:           svc.Stack,
		ComposeFilePath: svc.ComposePath,
	}
	for _, t := range svc.Targets {
		var inst *runner.Instance
		if live, ok := s.runner.Instance(t.ID()); ok {
			inst = &live
		}
		out.Targets = append(out.Targets, targetToProto(t, inst))
	}
	for _, in := range svc.Infra {
		out.Infra = append(out.Infra, infraToProto(in, infraStates))
	}
	// SizeBytes and BackupCount stay zero here; ListVolumes reports them.
	for _, v := range svc.Volumes {
		out.Volumes = append(out.Volumes, &anovelv1.Volume{Name: v.Name, Service: v.Service, Stack: v.Stack})
	}
	return out
}

// infraToProto renders a discovery.Infra with its podman container's live
// phase and ID from infraStates, so an infra row in `ps` never reads "idle"
// while the container is up.
func infraToProto(in *discovery.Infra, infraStates map[string]runner.InfraState) *anovelv1.Infra {
	st := infraStates[in.Service+"/"+in.Name]
	return &anovelv1.Infra{
		Id:          in.Stack + "/" + in.Service + "/" + in.Name,
		Name:        in.Name,
		Service:     in.Service,
		Stack:       in.Stack,
		Phase:       st.Phase,
		ContainerId: st.ContainerID,
	}
}

// liveInfraStates returns the live container states for a stack, ready to pass
// to serviceToProto. One batched podman call builds it.
func (s *Server) liveInfraStates(stack string) map[string]runner.InfraState {
	// A 5s budget for the batched scan. The TUI polls every 2s, but the RPC
	// can afford the headroom, and being slow beats being wrong.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.runner.InfraStatesOf(ctx, stack)
}

// targetToProto renders t with inst's live state layered on, so the proto
// carries the real phase, pid, mode, and exit reason. A nil inst gives the
// discovery-only view of a target the runner never started.
func targetToProto(t *discovery.Target, inst *runner.Instance) *anovelv1.Target {
	out := &anovelv1.Target{
		Id:      t.ID(),
		Name:    t.Name,
		Service: t.Service,
		Stack:   t.Stack,
		Kind:    kindToProto(t.Kind),
		Deps:    t.DependsOn,
	}
	if inst == nil {
		return out
	}
	out.Phase = inst.Phase
	out.ExitReason = inst.ExitReason
	out.Mode = inst.Mode
	out.Health = inst.Health
	out.Pid = inst.PID
	out.ContainerId = inst.ContainerID
	if !inst.StartedAt.IsZero() {
		out.StartedAt = timestamppb.New(inst.StartedAt)
	}
	if !inst.TerminatedAt.IsZero() {
		out.TerminatedAt = timestamppb.New(inst.TerminatedAt)
	}
	return out
}

func kindToProto(k discovery.TargetKind) anovelv1.TargetKind {
	switch k {
	case discovery.TargetKindOneShot:
		return anovelv1.TargetKind_TARGET_KIND_ONE_SHOT
	case discovery.TargetKindLongRunner:
		return anovelv1.TargetKind_TARGET_KIND_LONG_RUNNER
	default:
		return anovelv1.TargetKind_TARGET_KIND_UNSPECIFIED
	}
}
