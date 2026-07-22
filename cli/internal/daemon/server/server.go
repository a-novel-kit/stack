// Package server implements the daemon-side handler for the
// anovel.v1.CoreService connect-rpc API. The Server type holds the
// long-lived daemon state — registered stacks, the target supervisor,
// the env allocator and builder, and the log hub — and exposes each RPC
// as a method that reads or mutates that state.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	"github.com/a-novel-kit/stack/cli/internal/daemon/reinstall"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	"github.com/a-novel-kit/stack/cli/internal/daemon/volumes"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// Server must satisfy the generated handler interface, so an RPC added to the
// proto without a handler fails the build.
var _ anovelv1connect.CoreServiceHandler = (*Server)(nil)

// Server is the daemon's connect-rpc handler, owning the state that survives
// across RPC calls.
type Server struct {
	mu         sync.RWMutex
	version    string             // daemon binary version (from build info)
	startedAt  time.Time          // for uptime calculation
	socketPath string             // for Status responses
	stacks     []stacks.Stack     // raw config from $A_NOVEL_STACKS
	discovered []*discovery.Stack // per-stack service tree
	runner     *runner.Runner     // process / container supervisor
	envAlloc   *env.Allocator     // port allocator + refcount
	envBuilder *env.Builder       // env block synthesis
	logs       *logs.Store        // per-target log files + streaming hub
	// shutdownCh closes to tell the daemon's main loop to exit cleanly.
	// SignalShutdown fires it, idempotently.
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
}

// New constructs a Server with every daemon-side subsystem wired up.
func New(version, socketPath string, stk []stacks.Stack, disc []*discovery.Stack, run *runner.Runner, alloc *env.Allocator, builder *env.Builder, logStore *logs.Store) *Server {
	return &Server{
		version:    version,
		startedAt:  time.Now(),
		socketPath: socketPath,
		stacks:     stk,
		discovered: disc,
		runner:     run,
		envAlloc:   alloc,
		envBuilder: builder,
		logs:       logStore,
		shutdownCh: make(chan struct{}),
	}
}

// ShutdownCh returns the channel the daemon's main loop waits on. It closes
// when SignalShutdown is called, as PrepareReinstall does once its checkpoint
// is durable.
func (s *Server) ShutdownCh() <-chan struct{} { return s.shutdownCh }

// SignalShutdown closes the shutdown channel. Idempotent.
func (s *Server) SignalShutdown() {
	s.shutdownOnce.Do(func() { close(s.shutdownCh) })
}

// allServiceNames returns every service name across every stack, which the env
// builder needs to detect cross-service prefixes.
func (s *Server) allServiceNames() []string {
	var out []string
	for _, st := range s.discovered {
		for _, svc := range st.Services {
			out = append(out, svc.Name)
		}
	}
	return out
}

// findStack returns the discovered Stack named `name`, or the default first
// stack when the name is empty. An unknown name yields a connect.Error.
func (s *Server) findStack(name string) (*discovery.Stack, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.discovered) == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("no stacks registered (set A_NOVEL_STACKS)"))
	}
	if name == "" {
		return s.discovered[0], nil
	}
	for _, st := range s.discovered {
		if st.Name == name {
			return st, nil
		}
	}
	return nil, connect.NewError(connect.CodeNotFound,
		fmt.Errorf("stack %q not registered", name))
}

// findService returns the named service in the named stack (or default
// stack if name is empty).
func (s *Server) findService(stackName, serviceName string) (*discovery.Service, error) {
	st, err := s.findStack(stackName)
	if err != nil {
		return nil, err
	}
	for _, svc := range st.Services {
		if svc.Name == serviceName {
			return svc, nil
		}
	}
	return nil, connect.NewError(connect.CodeNotFound,
		fmt.Errorf("service %q not found in stack %q", serviceName, st.Name))
}

// =============================================================================
// Daemon control
// =============================================================================

// Ping is the cheap handshake clients use to verify the daemon is alive.
// `core start` uses it to detect an already-running instance.
func (s *Server) Ping(_ context.Context, _ *connect.Request[anovelv1.PingRequest]) (*connect.Response[anovelv1.PingResponse], error) {
	return connect.NewResponse(&anovelv1.PingResponse{
		DaemonVersion: s.version,
		Now:           timestamppb.Now(),
	}), nil
}

// Status reports everything `a-novel core status` needs in one round-trip.
func (s *Server) Status(_ context.Context, _ *connect.Request[anovelv1.StatusRequest]) (*connect.Response[anovelv1.StatusResponse], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := &anovelv1.StatusResponse{
		DaemonVersion: s.version,
		SocketPath:    s.socketPath,
		StartedAt:     timestamppb.New(s.startedAt),
		Uptime:        durationpb.New(time.Since(s.startedAt)),
		Stacks:        make([]*anovelv1.Stack, 0, len(s.stacks)),
	}
	for _, st := range s.stacks {
		out.Stacks = append(out.Stacks, &anovelv1.Stack{
			Name:      st.Name,
			Path:      st.Path,
			IsDefault: st.IsDefault,
		})
	}
	out.ReinstallCheckpointPending = reinstall.Exists()
	return connect.NewResponse(out), nil
}

// =============================================================================
// RPC handlers
// =============================================================================

// PrepareReinstall writes a checkpoint listing every running go-exec target
// with the env to relaunch it, fsyncs it, then signals the daemon to shut down.
// Containers stay out, surviving the daemon's death on their own.
//
// A second PrepareReinstall is rejected while one is pending, guarded by the
// checkpoint file's existence.
func (s *Server) PrepareReinstall(_ context.Context, _ *connect.Request[anovelv1.PrepareReinstallRequest]) (*connect.Response[anovelv1.PrepareReinstallResponse], error) {
	if err := reinstall.EnsureSinglePending(); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	// Gather the running go-exec instances. An instance never stores the env
	// it started with, so relaunch re-derives it from the env builder and the
	// target comes back on freshly allocated ports, as a manual restart does.
	cp := reinstall.Checkpoint{}
	for _, inst := range s.runner.AllInstances() {
		if inst.Mode != runner.ModeGoExec {
			continue
		}
		if inst.Phase != anovelv1.Phase_PHASE_RUNNING && inst.Phase != anovelv1.Phase_PHASE_STARTING {
			continue
		}
		// Re-derive env for relaunch.
		tgt, _, err := s.findTargetByID(inst.ID)
		if err != nil {
			continue
		}
		envEntries, _, err := s.envBuilder.ForTarget(tgt, s.allServiceNames())
		if err != nil {
			continue
		}
		envList := osEnviron()
		for _, e := range envEntries {
			envList = append(envList, e.Key+"="+e.Value)
		}
		cp.GoExec = append(cp.GoExec, reinstall.GoExecCheckpoint{
			TargetID: inst.ID,
			Env:      envList,
		})
	}
	if err := reinstall.Write(cp); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("write checkpoint: %w", err))
	}
	resp := &anovelv1.PrepareReinstallResponse{
		CheckpointPath:    reinstall.Path(),
		GoExecTargetCount: int32(len(cp.GoExec)),
	}
	// Delay the signal so the client has the response before the socket
	// closes. The daemon's main select loop then wakes and shuts down
	// gracefully, SIGTERMing the go-exec children and leaving containers.
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.SignalShutdown()
	}()
	return connect.NewResponse(resp), nil
}

// Shutdown is the no-checkpoint daemon stop. With force=false it SIGTERMs every
// running go-exec target with a 10s grace each and leaves containers alone.
// With force=true it also calls KillInfra(force=true) on every service holding
// an active infra session, cascade-killing the remaining targets and tearing
// down their infra containers.
//
// The shutdown signal fires after the response is sent, so the client gets a
// clean reply.
func (s *Server) Shutdown(ctx context.Context, req *connect.Request[anovelv1.ShutdownRequest]) (*connect.Response[anovelv1.ShutdownResponse], error) {
	force := req.Msg.GetForce()
	// 1) Snapshot the go-exec instances that need stopping.
	var goExecIDs []string
	for _, inst := range s.runner.AllInstances() {
		if inst.Mode != runner.ModeGoExec {
			continue
		}
		if inst.Phase != anovelv1.Phase_PHASE_RUNNING && inst.Phase != anovelv1.Phase_PHASE_STARTING {
			continue
		}
		goExecIDs = append(goExecIDs, inst.ID)
	}
	// 2) Kill them in parallel: runner.Kill blocks until its grace expires,
	//    so one goroutine per target bounds the total by the longest grace.
	//    The background context keeps a cancelled RPC from leaving
	//    half-killed targets, since shutdown is a commitment.
	killCtx, cancelKill := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelKill()
	var killWG sync.WaitGroup
	killWG.Add(len(goExecIDs))
	for _, id := range goExecIDs {
		go func(id string) {
			defer killWG.Done()
			_ = s.runner.Kill(killCtx, id, 10*time.Second)
		}(id)
	}
	killWG.Wait()
	// 3) If force, tear down every active infra session.
	infraTorn := 0
	if force {
		for _, ref := range s.runner.ActiveInfraSessions() {
			if err := s.runner.KillInfra(killCtx, ref.Stack, ref.Service, true /* force */); err == nil {
				infraTorn++
			}
		}
	}
	resp := &anovelv1.ShutdownResponse{
		GoExecKilled:          int32(len(goExecIDs)),
		InfraServicesTornDown: int32(infraTorn),
	}
	// 4) Fire the shutdown signal after the response is on the wire; the 50ms
	//    delay lets the runtime flush it before the listener closes.
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.SignalShutdown()
	}()
	_ = ctx // keep signature parity with the rest of the handlers
	return connect.NewResponse(resp), nil
}

// ListStacks returns every registered stack. Stacks are fixed at startup,
// so this reads them straight from state.
func (s *Server) ListStacks(_ context.Context, _ *connect.Request[anovelv1.ListStacksRequest]) (*connect.Response[anovelv1.ListStacksResponse], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := &anovelv1.ListStacksResponse{
		Stacks: make([]*anovelv1.Stack, 0, len(s.stacks)),
	}
	for _, st := range s.stacks {
		out.Stacks = append(out.Stacks, &anovelv1.Stack{
			Name:      st.Name,
			Path:      st.Path,
			IsDefault: st.IsDefault,
		})
	}
	return connect.NewResponse(out), nil
}

// ListServices returns every service in the requested stack. An empty
// Request.Stack means the default stack, and "*" the union across every
// registered stack, where each returned Service keeps its stack name.
func (s *Server) ListServices(_ context.Context, req *connect.Request[anovelv1.ListServicesRequest]) (*connect.Response[anovelv1.ListServicesResponse], error) {
	out := &anovelv1.ListServicesResponse{}
	if req.Msg.GetStack() == "*" {
		s.mu.RLock()
		defer s.mu.RUnlock()
		// Build one infraStates cache per stack so each service in the
		// stack reuses the same podman scan.
		stackCaches := make(map[string]map[string]runner.InfraState, len(s.discovered))
		for _, st := range s.discovered {
			stackCaches[st.Name] = s.liveInfraStates(st.Name)
		}
		for _, st := range s.discovered {
			cache := stackCaches[st.Name]
			for _, svc := range st.Services {
				out.Services = append(out.Services, s.convertService(svc, cache))
			}
		}
		// Stable order: stack name, then service name.
		sort.Slice(out.Services, func(i, j int) bool {
			if out.Services[i].GetStack() != out.Services[j].GetStack() {
				return out.Services[i].GetStack() < out.Services[j].GetStack()
			}
			return out.Services[i].GetName() < out.Services[j].GetName()
		})
		return connect.NewResponse(out), nil
	}
	st, err := s.findStack(req.Msg.GetStack())
	if err != nil {
		return nil, err
	}
	cache := s.liveInfraStates(st.Name)
	for _, svc := range st.Services {
		out.Services = append(out.Services, s.convertService(svc, cache))
	}
	sort.Slice(out.Services, func(i, j int) bool {
		return out.Services[i].GetName() < out.Services[j].GetName()
	})
	return connect.NewResponse(out), nil
}

// DescribeService returns one service by (stack, service) lookup.
func (s *Server) DescribeService(_ context.Context, req *connect.Request[anovelv1.DescribeServiceRequest]) (*connect.Response[anovelv1.DescribeServiceResponse], error) {
	svc, err := s.findService(req.Msg.GetStack(), req.Msg.GetService())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&anovelv1.DescribeServiceResponse{
		Service: s.convertService(svc, s.liveInfraStates(svc.Stack)),
	}), nil
}

// GetTopology renders the dependency graph as ASCII text. An empty Service
// emits every service in the stack, one below the next.
func (s *Server) GetTopology(_ context.Context, req *connect.Request[anovelv1.GetTopologyRequest]) (*connect.Response[anovelv1.GetTopologyResponse], error) {
	if req.Msg.GetService() != "" {
		svc, err := s.findService(req.Msg.GetStack(), req.Msg.GetService())
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(&anovelv1.GetTopologyResponse{
			Rendered: discovery.RenderTopology(svc),
		}), nil
	}
	st, err := s.findStack(req.Msg.GetStack())
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	for i, svc := range st.Services {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(discovery.RenderTopology(svc))
	}
	return connect.NewResponse(&anovelv1.GetTopologyResponse{Rendered: b.String()}), nil
}

// StartTarget brings up the named target in the requested mode, defaulting to
// go-exec, and synthesizes the target's env before launch.
func (s *Server) StartTarget(ctx context.Context, req *connect.Request[anovelv1.StartTargetRequest]) (*connect.Response[anovelv1.StartTargetResponse], error) {
	mode := req.Msg.GetMode()
	if mode == anovelv1.Mode_MODE_UNSPECIFIED {
		mode = anovelv1.Mode_MODE_GO_EXEC
	}
	// The discovery target backs both the dependency walk and the env
	// synthesis.
	tgt, svc, err := s.findTargetByID(req.Msg.GetTargetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	// Gate on the dependency walk: infra up, one-shots satisfied, and a
	// refusal-with-hint for a long-runner dep that is not already running. It
	// runs before the env build, so the `${*_PORT}` slots it allocates land in
	// the snapshot the builder reads next.
	depMode := convertModeFromProto(mode) // mode for any auto-run one-shots
	// The dep walker's env argument goes unused, since runner.StartInfra
	// builds its own; the inherited daemon env satisfies the signature.
	if err := s.runner.EnsureDepsReady(ctx, tgt, svc, depMode, osEnviron()); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	// Rebuild the env now that infra-up has allocated the service-level ports:
	// the builder's snapshot fill picks up POSTGRES_PORT from the allocator and
	// synthesizes POSTGRES_DSN with localhost:<port>.
	envEntries, warnings, err := s.envBuilder.ForTarget(tgt, s.allServiceNames())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("env build: %w", err))
	}
	envList := osEnviron()
	for _, e := range envEntries {
		envList = append(envList, e.Key+"="+e.Value)
	}
	switch mode {
	case anovelv1.Mode_MODE_GO_EXEC:
		inst, err := s.runner.StartGoExec(ctx, req.Msg.GetTargetId(), envList, warnings)
		if err != nil {
			s.envAlloc.Release(req.Msg.GetTargetId())
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return connect.NewResponse(&anovelv1.StartTargetResponse{
			Target: instanceToProto(inst, s.lookupTargetForInstance(inst)),
		}), nil
	case anovelv1.Mode_MODE_CONTAINER:
		inst, err := s.runner.StartContainer(ctx, req.Msg.GetTargetId(), envList, warnings)
		if err != nil {
			s.envAlloc.Release(req.Msg.GetTargetId())
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return connect.NewResponse(&anovelv1.StartTargetResponse{
			Target: instanceToProto(inst, s.lookupTargetForInstance(inst)),
		}), nil
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("unknown mode %v", mode))
	}
}

// findTargetByID resolves a "<stack>/<service>/<target>" ID into its discovery
// objects.
func (s *Server) findTargetByID(id string) (*discovery.Target, *discovery.Service, error) {
	for _, st := range s.discovered {
		for _, svc := range st.Services {
			for _, t := range svc.Targets {
				if t.ID() == id {
					return t, svc, nil
				}
			}
		}
	}
	return nil, nil, fmt.Errorf("unknown target %q", id)
}

// KillTarget stops the named instance with the requested SIGTERM grace,
// defaulting to 10s, where 0 means an immediate SIGKILL. It is idempotent.
func (s *Server) KillTarget(ctx context.Context, req *connect.Request[anovelv1.KillTargetRequest]) (*connect.Response[anovelv1.KillTargetResponse], error) {
	grace := req.Msg.GetTimeout().AsDuration()
	if grace == 0 && req.Msg.GetTimeout() == nil {
		grace = 10 * time.Second
	}
	if err := s.runner.Kill(ctx, req.Msg.GetTargetId(), grace); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	inst, _ := s.runner.Instance(req.Msg.GetTargetId())
	return connect.NewResponse(&anovelv1.KillTargetResponse{
		Target: instanceToProto(&inst, s.lookupTargetForInstance(&inst)),
	}), nil
}

// RestartTarget is Kill and Start in one RPC. The kill completes before the
// start claims the slot, so mutual exclusion holds.
func (s *Server) RestartTarget(ctx context.Context, req *connect.Request[anovelv1.RestartTargetRequest]) (*connect.Response[anovelv1.RestartTargetResponse], error) {
	id := req.Msg.GetTargetId()
	// A 10s grace, matching KillTarget.
	if err := s.runner.Kill(ctx, id, 10*time.Second); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	mode := req.Msg.GetMode()
	if mode == anovelv1.Mode_MODE_UNSPECIFIED {
		// Keep the previous mode when an instance record survives,
		// defaulting to go-exec otherwise.
		if prev, ok := s.runner.Instance(id); ok && prev.Mode != 0 {
			mode = modeToProto(prev.Mode)
		} else {
			mode = anovelv1.Mode_MODE_GO_EXEC
		}
	}
	startResp, err := s.StartTarget(ctx, connect.NewRequest(&anovelv1.StartTargetRequest{
		TargetId: id,
		Mode:     mode,
	}))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&anovelv1.RestartTargetResponse{
		Target: startResp.Msg.GetTarget(),
	}), nil
}

// lookupTargetForInstance returns the discovery.Target behind an Instance, so a
// proto response can carry the static metadata the runner does not track.
func (s *Server) lookupTargetForInstance(inst *runner.Instance) *discovery.Target {
	if inst.ID == "" {
		return nil
	}
	for _, st := range s.discovered {
		if st.Name != inst.Stack {
			continue
		}
		for _, svc := range st.Services {
			if svc.Name != inst.Service {
				continue
			}
			for _, t := range svc.Targets {
				if t.Name == inst.Target {
					return t
				}
			}
		}
	}
	return nil
}

// StartInfra brings up a service's infrastructure containers and auto-runs
// every one-shot target the long-runners depend on. It is idempotent.
func (s *Server) StartInfra(ctx context.Context, req *connect.Request[anovelv1.StartInfraRequest]) (*connect.Response[anovelv1.StartInfraResponse], error) {
	stack := req.Msg.GetStack()
	if stack == "" && len(s.discovered) > 0 {
		stack = s.discovered[0].Name
	}
	svc, err := s.findService(stack, req.Msg.GetService())
	if err != nil {
		return nil, err
	}
	// ForServiceUp allocates the `${*_PORT}` slots infra services reference
	// under the service-level consumer ID, so KillInfra's Release fires
	// cleanly and compose's substitution sees a real port number.
	consumer := stack + "/" + svc.Name + "-infra"
	envEntries, err := s.envBuilder.ForServiceUp(svc, s.allServiceNames(), consumer)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("env build: %w", err))
	}
	envList := osEnviron()
	for _, e := range envEntries {
		envList = append(envList, e.Key+"="+e.Value)
	}
	oneShotsMode := convertModeFromProto(req.Msg.GetOneShotsMode())
	if err := s.runner.StartInfra(ctx, stack, svc.Name, oneShotsMode, envList); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&anovelv1.StartInfraResponse{
		Service: s.convertService(svc, s.liveInfraStates(svc.Stack)),
	}), nil
}

// KillInfra refuses while any long-runner of the service is still running.
// --force cascade-kills those targets first.
func (s *Server) KillInfra(ctx context.Context, req *connect.Request[anovelv1.KillInfraRequest]) (*connect.Response[anovelv1.KillInfraResponse], error) {
	stack := req.Msg.GetStack()
	if stack == "" && len(s.discovered) > 0 {
		stack = s.discovered[0].Name
	}
	svc, err := s.findService(stack, req.Msg.GetService())
	if err != nil {
		return nil, err
	}
	if err := s.runner.KillInfra(ctx, stack, svc.Name, req.Msg.GetForce()); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&anovelv1.KillInfraResponse{
		Service: s.convertService(svc, s.liveInfraStates(svc.Stack)),
	}), nil
}

// KillInfraContainer stops one infra container by (stack, service, name),
// leaving the rest of the service's infra and its running targets untouched.
// The TUI uses it to manage infra entries like any other tab.
func (s *Server) KillInfraContainer(ctx context.Context, req *connect.Request[anovelv1.KillInfraContainerRequest]) (*connect.Response[anovelv1.KillInfraContainerResponse], error) {
	stack := req.Msg.GetStack()
	if stack == "" && len(s.discovered) > 0 {
		stack = s.discovered[0].Name
	}
	svc, err := s.findService(stack, req.Msg.GetService())
	if err != nil {
		return nil, err
	}
	in := findInfra(svc, req.Msg.GetName())
	if in == nil {
		return nil, connect.NewError(connect.CodeNotFound,
			fmt.Errorf("infra %q not declared in %s/%s", req.Msg.GetName(), stack, svc.Name))
	}
	if err := s.runner.KillInfraContainer(ctx, stack, svc.Name, in.Name); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&anovelv1.KillInfraContainerResponse{
		Infra: s.convertInfraWithLive(in, s.liveInfraStates(svc.Stack)),
	}), nil
}

// RestartInfraContainer restarts one infra container in place with `podman
// restart`, preserving its volume bindings.
func (s *Server) RestartInfraContainer(ctx context.Context, req *connect.Request[anovelv1.RestartInfraContainerRequest]) (*connect.Response[anovelv1.RestartInfraContainerResponse], error) {
	stack := req.Msg.GetStack()
	if stack == "" && len(s.discovered) > 0 {
		stack = s.discovered[0].Name
	}
	svc, err := s.findService(stack, req.Msg.GetService())
	if err != nil {
		return nil, err
	}
	in := findInfra(svc, req.Msg.GetName())
	if in == nil {
		return nil, connect.NewError(connect.CodeNotFound,
			fmt.Errorf("infra %q not declared in %s/%s", req.Msg.GetName(), stack, svc.Name))
	}
	if err := s.runner.RestartInfraContainer(ctx, stack, svc.Name, in.Name); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&anovelv1.RestartInfraContainerResponse{
		Infra: s.convertInfraWithLive(in, s.liveInfraStates(svc.Stack)),
	}), nil
}

// findInfra returns the named infra in a discovery.Service, or nil when the
// service does not declare it.
func findInfra(svc *discovery.Service, name string) *discovery.Infra {
	for _, in := range svc.Infra {
		if in.Name == name {
			return in
		}
	}
	return nil
}

// convertModeFromProto maps proto Mode → runner.Mode.
func convertModeFromProto(m anovelv1.Mode) runner.Mode {
	switch m {
	case anovelv1.Mode_MODE_CONTAINER:
		return runner.ModeContainer
	default:
		return runner.ModeGoExec
	}
}

// StreamLogs server-streams log lines for a target:
//   - by default, one snapshot of current.log from start to end
//   - with --follow, that snapshot plus every new line until the process
//     terminates or the client disconnects
//   - with --run-id, an archived run, never followed
//
// Snapshot lines come first in file order, and followed lines after. Each
// LogLine carries its original timestamp and stream tag.
func (s *Server) StreamLogs(ctx context.Context, req *connect.Request[anovelv1.StreamLogsRequest], stream *connect.ServerStream[anovelv1.LogLine]) error {
	tid := req.Msg.GetTargetId()
	// An infra log ID, "<stack>/<service>/infra/<name>", streams an infra
	// container's stdout through this same RPC. Those containers belong to
	// podman, so the branch reads `podman logs -f <cid>`.
	if stack, service, name, ok := parseInfraLogID(tid); ok {
		st, found := s.runner.InfraStatesOf(ctx, stack)[service+"/"+name]
		if !found || st.ContainerID == "" {
			return connect.NewError(connect.CodeFailedPrecondition,
				fmt.Errorf("no container for %s/%s/%s — has infra-start been run?", stack, service, name))
		}
		return streamPodmanLogs(ctx, st.ContainerID, req.Msg.GetFollow(), stream)
	}
	tgt, _, err := s.findTargetByID(tid)
	if err != nil {
		return connect.NewError(connect.CodeNotFound, err)
	}
	// Pick the file to stream.
	var path string
	if req.Msg.GetRunId() != "" {
		path = s.logs.RunPath(tgt.Stack, tgt.Service, tgt.Name, req.Msg.GetRunId())
	} else {
		path = s.logs.CurrentPath(tgt.Stack, tgt.Service, tgt.Name)
	}
	follow := req.Msg.GetFollow() && req.Msg.GetRunId() == ""

	// Under --follow, subscribe before reading the file so no line written
	// between snapshot and subscribe is lost. The unsub fires on handler exit,
	// keeping dead channels out of the target's subscriber slice.
	var sub <-chan logs.Line
	var unsubLogs func()
	if follow {
		var ok bool
		sub, unsubLogs, ok = s.logs.Subscribe(tid)
		if ok && unsubLogs != nil {
			defer unsubLogs()
		}
	}

	if err := streamFileToClient(ctx, path, stream, req.Msg.GetStream()); err != nil {
		// A missing file means no run yet, so fall through to the follow
		// path when one was requested.
		if !follow {
			return connect.NewError(connect.CodeNotFound,
				fmt.Errorf("read log %s: %w", path, err))
		}
	}
	if !follow || sub == nil {
		return nil
	}
	// Forward new lines until subscription closes or client disconnects.
	for {
		select {
		case <-ctx.Done():
			return nil
		case ln, ok := <-sub:
			if !ok {
				return nil
			}
			if req.Msg.GetStream() != anovelv1.LogStream_LOG_STREAM_UNSPECIFIED &&
				lineStreamToProto(ln.Stream) != req.Msg.GetStream() {
				continue
			}
			if err := stream.Send(&anovelv1.LogLine{
				Ts:     timestamppb.New(ln.Ts),
				Stream: lineStreamToProto(ln.Stream),
				Line:   ln.Line,
			}); err != nil {
				return err
			}
		}
	}
}

// ListRuns returns the timestamps of archived runs for the target,
// newest first.
func (s *Server) ListRuns(_ context.Context, req *connect.Request[anovelv1.ListRunsRequest]) (*connect.Response[anovelv1.ListRunsResponse], error) {
	tgt, _, err := s.findTargetByID(req.Msg.GetTargetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&anovelv1.ListRunsResponse{
		RunIds: s.logs.ListRuns(tgt.Stack, tgt.Service, tgt.Name),
	}), nil
}

// streamFileToClient reads a JSON-lines log file and sends every line matching
// the optional stream filter to the client, on both the snapshot and the
// archived-run path.
func streamFileToClient(ctx context.Context, path string, stream *connect.ServerStream[anovelv1.LogLine], filter anovelv1.LogStream) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	dec := json.NewDecoder(f)
	for {
		if ctx.Err() != nil {
			return nil
		}
		var ln logs.Line
		if err := dec.Decode(&ln); err != nil {
			break
		}
		ps := lineStreamToProto(ln.Stream)
		if filter != anovelv1.LogStream_LOG_STREAM_UNSPECIFIED && ps != filter {
			continue
		}
		if err := stream.Send(&anovelv1.LogLine{
			Ts:     timestamppb.New(ln.Ts),
			Stream: ps,
			Line:   ln.Line,
		}); err != nil {
			return err
		}
	}
	return nil
}

func lineStreamToProto(s logs.Stream) anovelv1.LogStream {
	switch s {
	case logs.StreamStdout:
		return anovelv1.LogStream_LOG_STREAM_STDOUT
	case logs.StreamStderr:
		return anovelv1.LogStream_LOG_STREAM_STDERR
	default:
		return anovelv1.LogStream_LOG_STREAM_UNSPECIFIED
	}
}

// GetEnv assembles the env block for one service, or for every service in
// scope. It is read-only and never allocates a port, so a variable whose slot
// is unallocated appears with an empty value and the user still sees the shape.
func (s *Server) GetEnv(_ context.Context, req *connect.Request[anovelv1.GetEnvRequest]) (*connect.Response[anovelv1.GetEnvResponse], error) {
	allNames := s.allServiceNames()
	out := &anovelv1.GetEnvResponse{}
	gather := func(svc *discovery.Service) error {
		entries, err := s.envBuilder.ForService(svc, allNames)
		if err != nil {
			return err
		}
		for _, e := range entries {
			out.Entries = append(out.Entries, &anovelv1.EnvEntry{
				Stack:   svc.Stack,
				Service: svc.Name,
				Key:     e.Key,
				Value:   e.Value,
			})
		}
		return nil
	}
	switch {
	case req.Msg.GetAllStacks():
		for _, st := range s.discovered {
			for _, svc := range st.Services {
				if err := gather(svc); err != nil {
					return nil, connect.NewError(connect.CodeInternal, err)
				}
			}
		}
	case req.Msg.GetService() != "":
		svc, err := s.findService(req.Msg.GetStack(), req.Msg.GetService())
		if err != nil {
			return nil, err
		}
		if err := gather(svc); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	default:
		st, err := s.findStack(req.Msg.GetStack())
		if err != nil {
			return nil, err
		}
		for _, svc := range st.Services {
			if err := gather(svc); err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
		}
	}
	return connect.NewResponse(out), nil
}

// osEnviron returns the daemon process's environment as a fresh slice, the base
// layer a spawned target inherits PATH, HOME, and GOPATH through. The
// synthesized env layers over it and overrides any shared key.
func osEnviron() []string {
	return append([]string(nil), os.Environ()...)
}

// ListVolumes returns one read-only Volume row per compose-declared volume on
// the service, with its size and backup count.
func (s *Server) ListVolumes(_ context.Context, req *connect.Request[anovelv1.ListVolumesRequest]) (*connect.Response[anovelv1.ListVolumesResponse], error) {
	svc, err := s.findService(req.Msg.GetStack(), req.Msg.GetService())
	if err != nil {
		return nil, err
	}
	vols, err := volumes.List(svc)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &anovelv1.ListVolumesResponse{}
	for _, v := range vols {
		out.Volumes = append(out.Volumes, &anovelv1.Volume{
			Name:        v.Name,
			Service:     v.Service,
			Stack:       v.Stack,
			SizeBytes:   v.SizeBytes,
			BackupCount: v.BackupCount,
		})
	}
	return connect.NewResponse(out), nil
}

// BackupVolume writes a tar.zst snapshot per volume. It refuses while the
// service is up, unless --force cascade-kills it first.
func (s *Server) BackupVolume(ctx context.Context, req *connect.Request[anovelv1.BackupVolumeRequest]) (*connect.Response[anovelv1.BackupVolumeResponse], error) {
	svc, err := s.findService(req.Msg.GetStack(), req.Msg.GetService())
	if err != nil {
		return nil, err
	}
	if err := s.ensureServiceDown(ctx, svc, req.Msg.GetForce(), "backup"); err != nil {
		return nil, err
	}
	paths, err := volumes.Backup(svc, req.Msg.GetTag())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&anovelv1.BackupVolumeResponse{ArchivePaths: paths}), nil
}

// RestoreVolume replaces each volume from its matching backup. It refuses while
// the service is up.
func (s *Server) RestoreVolume(ctx context.Context, req *connect.Request[anovelv1.RestoreVolumeRequest]) (*connect.Response[anovelv1.RestoreVolumeResponse], error) {
	svc, err := s.findService(req.Msg.GetStack(), req.Msg.GetService())
	if err != nil {
		return nil, err
	}
	if err := s.ensureServiceDown(ctx, svc, req.Msg.GetForce(), "restore"); err != nil {
		return nil, err
	}
	restored, err := volumes.Restore(svc, req.Msg.GetFrom())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&anovelv1.RestoreVolumeResponse{RestoredVolumes: restored}), nil
}

// ClearVolume destroys the service's volumes, backing them up first unless
// --no-backup. It refuses while the service is up.
func (s *Server) ClearVolume(ctx context.Context, req *connect.Request[anovelv1.ClearVolumeRequest]) (*connect.Response[anovelv1.ClearVolumeResponse], error) {
	svc, err := s.findService(req.Msg.GetStack(), req.Msg.GetService())
	if err != nil {
		return nil, err
	}
	if err := s.ensureServiceDown(ctx, svc, req.Msg.GetForce(), "clear"); err != nil {
		return nil, err
	}
	cleared, err := volumes.Clear(svc, req.Msg.GetNoBackup())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&anovelv1.ClearVolumeResponse{ClearedVolumes: cleared}), nil
}

// ensureServiceDown is the pre-check every destructive volume operation shares.
// It refuses with a hint while any target or infra is up; with force it
// cascade-kills both first.
func (s *Server) ensureServiceDown(ctx context.Context, svc *discovery.Service, force bool, op string) error {
	running := s.runningInstances(svc.Stack, svc.Name)
	sess, _ := s.runner.InfraSession(svc.Stack, svc.Name)
	infraUp := sess.Up
	if len(running) == 0 && !infraUp {
		return nil // all clear
	}
	if !force {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf(
			"refusing to %s while %s/%s is up — kill targets + infra first (or pass --force):\n  running targets: %v\n  infra session up: %v",
			op, svc.Stack, svc.Name, runningNames(running), infraUp))
	}
	// A forced KillInfra cascades through the targets and the infra alike.
	if err := s.runner.KillInfra(ctx, svc.Stack, svc.Name, true /* force */); err != nil {
		return connect.NewError(connect.CodeInternal,
			fmt.Errorf("force-stop before %s: %w", op, err))
	}
	return nil
}

// runningInstances returns the service's runner Instances that have yet to
// reach TERMINATED.
func (s *Server) runningInstances(stack, service string) []runner.Instance {
	var out []runner.Instance
	for _, inst := range s.runner.AllInstances() {
		if inst.Stack == stack && inst.Service == service &&
			inst.Phase != anovelv1.Phase_PHASE_TERMINATED {
			out = append(out, inst)
		}
	}
	return out
}

func runningNames(insts []runner.Instance) []string {
	out := make([]string, 0, len(insts))
	for _, i := range insts {
		out = append(out, i.Target)
	}
	return out
}

// Exec runs an arbitrary command "as if it were a sibling of the target":
//
//   - Container mode: `podman exec <container_id> <cmd...>` — runs inside
//     the live container, so `a-novel exec rest sh` drops you into a
//     shell next to the rest binary. Requires the target to be RUNNING.
//   - Go-exec mode: spawns the command in the target's working directory with
//     the target's resolved env, so `a-novel exec migrations psql` gets the
//     right DSN out of the box. The target need only be discoverable, since the
//     env builder synthesizes a fresh allocation when none is current, and the
//     command works on a cold stack with infra up.
//
// Stdout / stderr are forwarded as LOG_STREAM_STDOUT / _STDERR; the proto
// reuses LogStream so clients render with the same code path as logs.
func (s *Server) Exec(ctx context.Context, req *connect.Request[anovelv1.ExecRequest], stream *connect.ServerStream[anovelv1.ExecOutput]) error {
	id := req.Msg.GetTargetId()
	cmdv := req.Msg.GetCmd()
	if len(cmdv) == 0 {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("exec: empty cmd"))
	}
	if id == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("exec: target_id required"))
	}
	tgt, _, err := s.findTargetByID(id)
	if err != nil {
		return connect.NewError(connect.CodeNotFound, err)
	}
	// A running instance decides the mode. Without one, go-exec is the only
	// mode that works, there being no live container to attach to.
	mode := runner.ModeGoExec
	containerID := ""
	if inst, ok := s.runner.Instance(id); ok && inst.Phase == anovelv1.Phase_PHASE_RUNNING {
		mode = inst.Mode
		containerID = inst.ContainerID
	}
	var execCmd *exec.Cmd
	switch mode {
	case runner.ModeContainer:
		if containerID == "" {
			return connect.NewError(connect.CodeFailedPrecondition,
				errors.New("exec: target running in container mode but containerId is empty"))
		}
		args := append([]string{"exec", containerID}, cmdv...)
		execCmd = exec.CommandContext(ctx, "podman", args...)
	default:
		// A go-exec sibling gets the same directory and env as the target.
		// The builder allocates when needed under the target's own consumer
		// ID, which the usual refcounting releases.
		envEntries, _, err := s.envBuilder.ForTarget(tgt, s.allServiceNames())
		if err != nil {
			return connect.NewError(connect.CodeInternal, fmt.Errorf("exec: env: %w", err))
		}
		envList := osEnviron()
		for _, e := range envEntries {
			envList = append(envList, e.Key+"="+e.Value)
		}
		execCmd = exec.CommandContext(ctx, cmdv[0], cmdv[1:]...)
		// Run in the service directory, one level up from cmd/<target>, so
		// relative paths match what the target sees.
		execCmd.Dir = filepath.Dir(filepath.Dir(tgt.CmdDir))
		execCmd.Env = envList
	}
	stdoutR, err := execCmd.StdoutPipe()
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("exec: stdout pipe: %w", err))
	}
	stderrR, err := execCmd.StderrPipe()
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("exec: stderr pipe: %w", err))
	}
	if err := execCmd.Start(); err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("exec: start: %w", err))
	}
	// Two readers, one stream: a pair of goroutines line-scan stdout and
	// stderr and forward to the connect stream under a mutex, since
	// Stream.Send is unsafe for concurrent calls.
	var sendMu sync.Mutex
	send := func(s anovelv1.LogStream, line string) {
		sendMu.Lock()
		defer sendMu.Unlock()
		_ = stream.Send(&anovelv1.ExecOutput{Stream: s, Line: line})
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stdoutR)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			send(anovelv1.LogStream_LOG_STREAM_STDOUT, sc.Text())
		}
	}()
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderrR)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			send(anovelv1.LogStream_LOG_STREAM_STDERR, sc.Text())
		}
	}()
	wg.Wait()

	waitErr := execCmd.Wait()

	code, err := execExitCode(waitErr)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("exec: wait: %w", err))
	}

	if waitErr != nil {
		// The human-readable line survives being piped to a file, where the
		// process exit status does not. It rides the same stream as the
		// output, after everything the child wrote.
		send(anovelv1.LogStream_LOG_STREAM_STDERR, "[exec exited: "+waitErr.Error()+"]")
	}
	// The terminal message carries the exit code and no line, so the client can
	// propagate the child's status as its own.
	sendMu.Lock()
	defer sendMu.Unlock()
	_ = stream.Send(&anovelv1.ExecOutput{ExitCode: &code})

	return nil
}

// execExitCode maps a [exec.Cmd.Wait] error onto the child's exit status.
//
// A non-nil error that is not an [exec.ExitError] comes from a spawn or IO
// failure, where the child's status was never obtained. That error is returned
// to the caller, and only a real [exec.ExitError] yields a code.
func execExitCode(waitErr error) (int32, error) {
	if waitErr == nil {
		return 0, nil
	}

	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		return 0, waitErr
	}

	return int32(exitErr.ExitCode()), nil
}

// Debug returns instructions for attaching Delve to a running go-exec target.
// The user runs the printed command in their own terminal, so the dlv REPL
// stays interactive and a disconnect never kills the target.
//
// A container-mode target is rejected: attaching to a containerized process
// needs an in-image dlv and a port-forward the daemon does not manage.
//
// DelvePort is a suggestion for `dlv attach --listen=:<port>`, which the daemon
// never binds. It is `PID + 20000`, high but unprivileged, so debugging several
// targets gives a unique port per PID with no allocation bookkeeping.
func (s *Server) Debug(_ context.Context, req *connect.Request[anovelv1.DebugRequest]) (*connect.Response[anovelv1.DebugResponse], error) {
	id := req.Msg.GetTargetId()
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("debug: target_id required"))
	}
	inst, ok := s.runner.Instance(id)
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("debug: target %q not tracked (start it first)", id))
	}
	if inst.Phase != anovelv1.Phase_PHASE_RUNNING {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("debug: target %q is not running (phase=%s)", id, inst.Phase))
	}
	if inst.Mode != runner.ModeGoExec {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("debug: only go-exec targets can be attached to (container-mode dlv requires in-image dlv + port-forward)"))
	}
	port := inst.PID + 20000
	hint := fmt.Sprintf(
		"Attach with:\n  dlv attach %d --listen=:%d --headless --api-version=2\nThen connect from your editor (vscode: 'Connect to server' on port %d).",
		inst.PID, port, port,
	)
	return connect.NewResponse(&anovelv1.DebugResponse{
		DelvePort: port,
		Hint:      hint,
	}), nil
}

// Watch streams every phase transition the runner emits, filtered by the
// caller's (stack, service, target_id) tuple. An empty filter field matches any
// value, so (stack, service, "") watches one service and three empty fields
// watch everything.
//
// The stream stays open until the client cancels its side, or the daemon shuts
// down and the runner's fanout stops. Nothing is emitted up front, so a caller
// needing the initial state pairs Watch with a ListServices snapshot.
func (s *Server) Watch(ctx context.Context, req *connect.Request[anovelv1.WatchRequest], stream *connect.ServerStream[anovelv1.StateEvent]) error {
	wantStack := req.Msg.GetStack()
	wantService := req.Msg.GetService()
	wantTargetID := req.Msg.GetTargetId()
	filter := func(ev runner.PhaseEvent) bool {
		if wantStack != "" && wantStack != "*" && ev.Stack != wantStack {
			return false
		}
		if wantService != "" && ev.Service != wantService {
			return false
		}
		if wantTargetID != "" && ev.TargetID != wantTargetID {
			return false
		}
		return true
	}
	ch, unsub := s.runner.SubscribePhases(filter)
	defer unsub()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-ch:
			if !ok {
				return nil // runner closed our subscription
			}
			out := &anovelv1.StateEvent{
				Ts:          timestamppb.New(ev.Ts),
				TargetId:    ev.TargetID,
				Service:     ev.Service,
				Stack:       ev.Stack,
				OldPhase:    ev.OldPhase,
				NewPhase:    ev.NewPhase,
				Description: describePhaseEvent(ev),
			}
			if err := stream.Send(out); err != nil {
				return err
			}
		}
	}
}

// describePhaseEvent produces the one-line human summary that lands in
// StateEvent.description. The field is opaque to the daemon, and a client may
// re-render from (old_phase, new_phase, exit_reason) instead, but the pre-baked
// summary is the cheap default for `ps --watch`.
func describePhaseEvent(ev runner.PhaseEvent) string {
	switch ev.NewPhase {
	case anovelv1.Phase_PHASE_STARTING:
		return ev.TargetID + " starting"
	case anovelv1.Phase_PHASE_RUNNING:
		return ev.TargetID + " running"
	case anovelv1.Phase_PHASE_STOPPING:
		return ev.TargetID + " stopping"
	case anovelv1.Phase_PHASE_TERMINATED:
		switch ev.ExitReason {
		case anovelv1.ExitReason_EXIT_REASON_SUCCESS:
			return ev.TargetID + " terminated (success)"
		case anovelv1.ExitReason_EXIT_REASON_ERROR:
			return ev.TargetID + " terminated (error)"
		case anovelv1.ExitReason_EXIT_REASON_KILLED:
			return ev.TargetID + " terminated (killed)"
		case anovelv1.ExitReason_EXIT_REASON_CRASHED:
			return ev.TargetID + " terminated (crashed)"
		}
		return ev.TargetID + " terminated"
	}
	return ev.TargetID + " " + ev.NewPhase.String()
}

// unimplemented returns the uniform error for an RPC with no handler yet, where
// phase names when the feature is expected.
func unimplemented(rpc, phase string) error {
	return connect.NewError(connect.CodeUnimplemented, errors.New(rpc+" not yet implemented (scheduled for "+phase+")"))
}

// parseInfraLogID recognizes the "<stack>/<service>/infra/<name>" form
// StreamLogs uses to address an infra container's log stream. It returns
// (stack, service, name, true) on a match and ("", "", "", false) otherwise.
//
// The "infra" sentinel segment cannot collide with a target, since target IDs
// come from compose service names.
func parseInfraLogID(id string) (string, string, string, bool) {
	parts := strings.Split(id, "/")
	if len(parts) != 4 || parts[2] != "infra" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[3], true
}

// streamPodmanLogs forwards `podman logs [-f] <cid>` output to a client log
// stream. The container's stdout and stderr merge into one pipe, reported as
// stdout, since service images write to stdout anyway.
//
// Each line is stamped at read time, which is accurate enough for the log
// viewer and skips parsing podman's optional --timestamps prefix.
func streamPodmanLogs(ctx context.Context, cid string, follow bool, stream *connect.ServerStream[anovelv1.LogLine]) error {
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, cid)
	cmd := exec.CommandContext(ctx, "podman", args...)
	pipeR, pipeW := io.Pipe()
	cmd.Stdout = pipeW
	cmd.Stderr = pipeW
	if err := cmd.Start(); err != nil {
		return connect.NewError(connect.CodeInternal,
			fmt.Errorf("start podman logs %s: %w", cid, err))
	}
	// Close the writer when the command exits so the scanner sees EOF.
	go func() {
		_ = cmd.Wait()
		_ = pipeW.Close()
	}()
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()
	scanner := bufio.NewScanner(pipeR)
	// Container logs carry long lines, such as panics and JSON dumps, so the
	// per-token cap goes up to keep the scanner from bisecting one.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if err := stream.Send(&anovelv1.LogLine{
			Ts:     timestamppb.Now(),
			Stream: anovelv1.LogStream_LOG_STREAM_STDOUT,
			Line:   scanner.Text(),
		}); err != nil {
			return err
		}
	}
	return nil
}
