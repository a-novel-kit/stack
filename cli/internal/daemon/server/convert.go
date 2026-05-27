package server

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// targetIDFor reconstructs the stack/service/target ID that uniquely
// addresses a target across every stack. Single source for this rule so
// every RPC handler that returns IDs agrees with every parser. Matches
// runner.targetID exactly.
func targetIDFor(stack, service, target string) string {
	return stack + "/" + service + "/" + target
}
func infraIDFor(stack, service, name string) string {
	return stack + "/" + service + "/" + name
}

// convertService converts a discovery.Service into the proto Service,
// embedding its targets (with live state from the runner if present),
// infra, and volumes.
//
// infraStates is the per-stack live-state cache built once per
// ListServices call by liveInfraStates — pass nil to fall back to a
// fresh single-infra query per row (slow, used by code paths that
// convert a single service in isolation).
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

// convertInfraWithLive enriches a discovery.Infra with the matching
// podman container's live state (Phase + Health + ContainerID). Without
// this, infra rows in `ps` show "idle" even when the container is up.
//
// Reads from the pre-built infraStates map when provided (one podman
// call total for the whole ListServices RPC); falls back to a single
// InfraStateOf call when the map is nil (slower per-call cost but
// keeps single-service handlers honest).
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
	// Fallback path — single-shot query. Generous 3s timeout because
	// rootless podman cold start can be ~1s and we'd rather be slow
	// than report a misleading "idle".
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	phase, health, cid := s.runner.InfraStateOf(ctx, in.Stack, in.Service, in.Name)
	out.Phase = phase
	out.Health = health
	out.ContainerId = cid
	return out
}

// liveInfraStates returns one map of live container states for the
// given stack, suitable to pass to convertService. Built via a single
// batched podman call — see runner.InfraStatesOf.
func (s *Server) liveInfraStates(stack string) map[string]runner.InfraState {
	if s.runner == nil {
		return nil
	}
	// 5s budget for the batched scan: one podman ps + one inspect
	// per container. The TUI polls every 2s but the underlying RPC
	// can afford the headroom; better to be slow than wrong.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.runner.InfraStatesOf(ctx, stack)
}

// convertTargetWithLive enriches a discovery.Target with the matching
// runner.Instance (if any) so the proto carries real phase / pid /
// mode / exit-reason instead of zero values.
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

// convertTargetStatic produces the discovery-only view (no live state) —
// used when no runner.Instance is recorded for the target.
func convertTargetStatic(t *discovery.Target) *anovelv1.Target {
	out := &anovelv1.Target{
		Id:      targetIDFor(t.Stack, t.Service, t.Name),
		Name:    t.Name,
		Service: t.Service,
		Stack:   t.Stack,
		Kind:    convertKind(t.Kind),
	}
	for _, dep := range t.DependsOn {
		out.Deps = append(out.Deps, dep)
	}
	return out
}

// instanceToProto builds a Target message from a live Instance plus its
// backing discovery.Target. `t` may be nil (e.g., the instance was
// constructed before discovery was re-run); in that case the static
// metadata is sparse but the live fields are still present.
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
		for _, dep := range t.DependsOn {
			out.Deps = append(out.Deps, dep)
		}
	}
	return out
}

func convertInfra(in *discovery.Infra) *anovelv1.Infra {
	return &anovelv1.Infra{
		Id:      infraIDFor(in.Stack, in.Service, in.Name),
		Name:    in.Name,
		Service: in.Service,
		Stack:   in.Stack,
		// Phase / Health populated when infra-supervision lands (next chunk).
	}
}

func convertVolume(v *discovery.Volume) *anovelv1.Volume {
	return &anovelv1.Volume{
		Name:    v.Name,
		Service: v.Service,
		Stack:   v.Stack,
		// SizeBytes / BackupCount populated by phase 6 (volume ops).
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
