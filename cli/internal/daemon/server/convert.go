package server

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// targetIDFor builds the stack/service/target ID that addresses a target across
// every stack. It is the one place this rule lives on the server side, and it
// must match the runner's own form exactly.
func targetIDFor(stack, service, target string) string {
	return stack + "/" + service + "/" + target
}

// infraIDFor builds the stack/service/name ID that addresses an infra
// container, mirroring targetIDFor for targets.
func infraIDFor(stack, service, name string) string {
	return stack + "/" + service + "/" + name
}

// convertService converts a discovery.Service into the proto Service, embedding
// its targets with whatever live state the runner holds, plus its infra and
// volumes.
//
// infraStates is the per-stack live-state cache liveInfraStates builds once per
// ListServices call. A nil map falls back to a fresh query per infra row, which
// is slower and suits a handler converting one service in isolation.
func (s *Server) convertService(svc *discovery.Service, infraStates map[string]runner.InfraState) *anovelv1.Service {
	out := &anovelv1.Service{
		Name:            svc.Name,
		Stack:           svc.Stack,
		ComposeFilePath: svc.ComposePath,
	}
	for _, t := range svc.Targets {
		out.Targets = append(out.Targets, s.convertTargetWithLive(t))
	}
	for _, in := range svc.Infra {
		out.Infra = append(out.Infra, s.convertInfraWithLive(in, infraStates))
	}
	for _, v := range svc.Volumes {
		out.Volumes = append(out.Volumes, convertVolume(v))
	}
	return out
}

// convertInfraWithLive enriches a discovery.Infra with its podman container's
// live phase, health, and ID, so an infra row in `ps` never reads "idle" while
// the container is up.
//
// A pre-built infraStates map costs one podman call for the whole ListServices
// RPC; a nil map falls back to a single InfraStateOf call, slower but honest
// for a single-service handler.
func (s *Server) convertInfraWithLive(in *discovery.Infra, infraStates map[string]runner.InfraState) *anovelv1.Infra {
	out := convertInfra(in)
	if s.runner == nil {
		return out
	}
	if infraStates != nil {
		if st, ok := infraStates[in.Service+"/"+in.Name]; ok {
			out.Phase = st.Phase
			out.Health = st.Health
			out.ContainerId = st.ContainerID
		}
		return out
	}
	// The single-shot query gets a 3s budget: a rootless podman cold start
	// costs about 1s, and being slow beats reporting a misleading "idle".
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	phase, health, cid := s.runner.InfraStateOf(ctx, in.Stack, in.Service, in.Name)
	out.Phase = phase
	out.Health = health
	out.ContainerId = cid
	return out
}

// liveInfraStates returns the live container states for a stack, ready to pass
// to convertService. One batched podman call builds it.
func (s *Server) liveInfraStates(stack string) map[string]runner.InfraState {
	if s.runner == nil {
		return nil
	}
	// A 5s budget for the batched scan. The TUI polls every 2s, but the RPC
	// can afford the headroom, and being slow beats being wrong.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.runner.InfraStatesOf(ctx, stack)
}

// convertTargetWithLive enriches a discovery.Target with its runner.Instance,
// where one exists, so the proto carries the real phase, pid, mode, and exit
// reason.
func (s *Server) convertTargetWithLive(t *discovery.Target) *anovelv1.Target {
	id := targetIDFor(t.Stack, t.Service, t.Name)
	inst, ok := s.runner.Instance(id)
	if !ok {
		return convertTargetStatic(t)
	}
	out := convertTargetStatic(t)
	out.Phase = inst.Phase
	out.ExitReason = inst.ExitReason
	out.Mode = modeToProto(inst.Mode)
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

// convertTargetStatic produces the discovery-only view, for a target with no
// runner.Instance recorded.
func convertTargetStatic(t *discovery.Target) *anovelv1.Target {
	out := &anovelv1.Target{
		Id:      targetIDFor(t.Stack, t.Service, t.Name),
		Name:    t.Name,
		Service: t.Service,
		Stack:   t.Stack,
		Kind:    convertKind(t.Kind),
	}
	out.Deps = append(out.Deps, t.DependsOn...)
	return out
}

// instanceToProto builds a Target message from a live Instance and its backing
// discovery.Target. A nil t, as when the instance predates the latest
// discovery, leaves the static metadata sparse and the live fields intact.
func instanceToProto(inst *runner.Instance, t *discovery.Target) *anovelv1.Target {
	if inst == nil || inst.ID == "" {
		return nil
	}
	out := &anovelv1.Target{
		Id:          inst.ID,
		Name:        inst.Target,
		Service:     inst.Service,
		Stack:       inst.Stack,
		Phase:       inst.Phase,
		ExitReason:  inst.ExitReason,
		Mode:        modeToProto(inst.Mode),
		Health:      inst.Health,
		Pid:         inst.PID,
		ContainerId: inst.ContainerID,
	}
	if !inst.StartedAt.IsZero() {
		out.StartedAt = timestamppb.New(inst.StartedAt)
	}
	if !inst.TerminatedAt.IsZero() {
		out.TerminatedAt = timestamppb.New(inst.TerminatedAt)
	}
	if t != nil {
		out.Kind = convertKind(t.Kind)
		out.Deps = append(out.Deps, t.DependsOn...)
	}
	return out
}

func convertInfra(in *discovery.Infra) *anovelv1.Infra {
	return &anovelv1.Infra{
		Id:      infraIDFor(in.Stack, in.Service, in.Name),
		Name:    in.Name,
		Service: in.Service,
		Stack:   in.Stack,
		// Phase and Health stay zero here; convertInfraWithLive fills them
		// from the live container state.
	}
}

func convertVolume(v *discovery.Volume) *anovelv1.Volume {
	return &anovelv1.Volume{
		Name:    v.Name,
		Service: v.Service,
		Stack:   v.Stack,
		// SizeBytes and BackupCount stay zero here; ListVolumes reports
		// them separately via the volumes package.
	}
}

func convertKind(k discovery.TargetKind) anovelv1.TargetKind {
	switch k {
	case discovery.TargetKindOneShot:
		return anovelv1.TargetKind_TARGET_KIND_ONE_SHOT
	case discovery.TargetKindLongRunner:
		return anovelv1.TargetKind_TARGET_KIND_LONG_RUNNER
	default:
		return anovelv1.TargetKind_TARGET_KIND_UNSPECIFIED
	}
}

func modeToProto(m runner.Mode) anovelv1.Mode {
	switch m {
	case runner.ModeGoExec:
		return anovelv1.Mode_MODE_GO_EXEC
	case runner.ModeContainer:
		return anovelv1.Mode_MODE_CONTAINER
	default:
		return anovelv1.Mode_MODE_UNSPECIFIED
	}
}
