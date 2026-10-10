// Package server implements the daemon-side handler for the
// anovel.v1.CoreService connect-rpc API. The Server type holds the
// long-lived daemon state — discovered stacks, the target supervisor,
// the env builder, and the log hub — and exposes each RPC as a method
// that reads or mutates that state.
package server

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	"github.com/a-novel-kit/stack/cli/internal/daemon/reinstall"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	"github.com/a-novel-kit/stack/cli/internal/daemon/volumes"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// Server must satisfy the generated handler interface, so an RPC added to the
// proto without a handler fails the build.
var _ anovelv1connect.CoreServiceHandler = (*Server)(nil)

// allStacks is the stack name that addresses every discovered stack at once.
const allStacks = "*"

// shutdownDelay is how long Shutdown and PrepareReinstall wait before stopping
// the daemon, so their response reaches the client before the socket closes.
const shutdownDelay = 50 * time.Millisecond

// Server is the daemon's connect-rpc handler, owning the state that survives
// across RPC calls.
type Server struct {
	version    string           // daemon binary version (from build info)
	startedAt  time.Time        // for uptime calculation
	socketPath string           // for Status responses
	stacks     discovery.Stacks // the managed stacks and their service trees
	runner     *runner.Runner   // process / container supervisor
	envBuilder *env.Builder     // env block synthesis
	logs       *logs.Store      // per-target log files + streaming hub
	// stop tells the daemon's main loop to exit cleanly. It is idempotent.
	stop func()
}

// New constructs a Server with every daemon-side subsystem wired up. stop is
// what Shutdown and PrepareReinstall call to end the daemon.
func New(version, socketPath string, disc discovery.Stacks, run *runner.Runner, builder *env.Builder, logStore *logs.Store, stop func()) *Server {
	return &Server{
		version:    version,
		startedAt:  time.Now(),
		socketPath: socketPath,
		stacks:     disc,
		runner:     run,
		envBuilder: builder,
		logs:       logStore,
		stop:       stop,
	}
}

// findStack returns the discovered Stack named `name`, or the default stack
// when the name is empty. An unknown name yields a connect.Error.
func (s *Server) findStack(name string) (*discovery.Stack, error) {
	if len(s.stacks) == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			"no stacks registered (set A_NOVEL_STACKS)")
	}
	if st := s.stacks.Stack(name); st != nil {
		return st, nil
	}
	return nil, connect.Errorf(connect.CodeNotFound,
		"stack %q not registered", name)
}

// findService returns the named service in the named stack (or default
// stack if name is empty).
func (s *Server) findService(stackName, serviceName string) (*discovery.Service, error) {
	st, err := s.findStack(stackName)
	if err != nil {
		return nil, err
	}
	if svc := s.stacks.Service(st.Name, serviceName); svc != nil {
		return svc, nil
	}
	return nil, connect.Errorf(connect.CodeNotFound,
		"service %q not found in stack %q", serviceName, st.Name)
}

// scope returns the stacks a request names: every stack for "*", otherwise
// the one findStack resolves.
func (s *Server) scope(name string) ([]*discovery.Stack, error) {
	if name == allStacks {
		return s.stacks, nil
	}
	st, err := s.findStack(name)
	if err != nil {
		return nil, err
	}
	return []*discovery.Stack{st}, nil
}

// findTarget returns the discovered target with the given ID.
func (s *Server) findTarget(id string) (*discovery.Target, *discovery.Service, error) {
	if t, svc := s.stacks.Target(id); t != nil {
		return t, svc, nil
	}
	return nil, nil, connect.Errorf(connect.CodeNotFound, "unknown target %q", id)
}

// =============================================================================
// Daemon control
// =============================================================================

// Ping is the cheap handshake clients use to verify the daemon is alive.
// `core start` uses it to detect an already-running instance.
func (s *Server) Ping(_ context.Context, _ *anovelv1.PingRequest) (*anovelv1.PingResponse, error) {
	return &anovelv1.PingResponse{DaemonVersion: s.version}, nil
}

// Status reports everything `a-novel core status` needs in one round-trip.
func (s *Server) Status(_ context.Context, _ *anovelv1.StatusRequest) (*anovelv1.StatusResponse, error) {
	return &anovelv1.StatusResponse{
		DaemonVersion:              s.version,
		SocketPath:                 s.socketPath,
		StartedAt:                  timestamppb.New(s.startedAt),
		Uptime:                     durationpb.New(time.Since(s.startedAt)),
		Stacks:                     stacksToProto(s.stacks),
		ReinstallCheckpointPending: reinstall.Exists(),
	}, nil
}

// PrepareReinstall writes a checkpoint listing every running go-exec target
// with the env to relaunch it, fsyncs it, stops those targets, then signals the
// daemon to shut down. Containers stay out, surviving the daemon's death on
// their own.
//
// A second PrepareReinstall is rejected while one is pending, guarded by the
// checkpoint file's existence.
func (s *Server) PrepareReinstall(_ context.Context, _ *anovelv1.PrepareReinstallRequest) (*anovelv1.PrepareReinstallResponse, error) {
	if err := reinstall.EnsureSinglePending(); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err.Error()).WithCause(err)
	}
	// An instance never stores the env it started with, so the checkpoint
	// re-derives it from the env builder. The target still holds its port
	// claims, so the env names the ports it runs on, and the new daemon
	// relaunches it on those same ports.
	ids := s.liveGoExecIDs()
	cp := reinstall.Checkpoint{}
	for _, id := range ids {
		t, _ := s.stacks.Target(id)
		if t == nil {
			continue
		}
		envEntries, _, err := s.envBuilder.ForTarget(t)
		if err != nil {
			continue
		}
		cp.GoExec = append(cp.GoExec, reinstall.GoExecCheckpoint{
			TargetID: id,
			Env:      env.Environ(envEntries),
		})
	}
	if err := reinstall.Write(cp); err != nil {
		return nil, connect.Errorf(connect.CodeInternal, "write checkpoint: %v", err).WithCause(err)
	}
	// Stop the targets before the daemon exits, since a go-exec process
	// outlives its daemon and keeps the ports its relaunch needs. The 5s grace
	// keeps the RPC inside the 10s `a-novel install` gives it.
	for _, failure := range s.stopAll(ids, nil, 5*time.Second).failures {
		fmt.Fprintf(os.Stderr, "reinstall: %s\n", failure)
	}
	time.AfterFunc(shutdownDelay, s.stop)
	return &anovelv1.PrepareReinstallResponse{
		CheckpointPath:    reinstall.Path(),
		GoExecTargetCount: int32(len(cp.GoExec)),
	}, nil
}

// Shutdown is the no-checkpoint daemon stop. With force=false it SIGTERMs every
// running go-exec target with a 10s grace each and leaves containers alone.
// With force=true it also calls KillInfra(force=true) on every service holding
// an active infra session, cascade-killing the remaining targets and tearing
// down their infra containers.
//
// The response names every target that could not be stopped. Whoever ran this
// asked for a clean environment, and a partial one that reports success is one
// they will not go back and check. The shutdown signal fires after the
// response is sent, so the client gets a clean reply.
func (s *Server) Shutdown(_ context.Context, req *anovelv1.ShutdownRequest) (*anovelv1.ShutdownResponse, error) {
	var sessions []runner.InfraSessionRef
	if req.GetForce() {
		sessions = s.runner.ActiveInfraSessions()
	}
	outcome := s.stopAll(s.liveGoExecIDs(), sessions, 10*time.Second)
	time.AfterFunc(shutdownDelay, s.stop)
	return &anovelv1.ShutdownResponse{
		GoExecKilled:          int32(outcome.goExecKilled),
		InfraServicesTornDown: int32(outcome.infraTornDown),
		Failures:              outcome.failures,
	}, nil
}

// liveGoExecIDs returns the IDs of every starting or running go-exec target.
func (s *Server) liveGoExecIDs() []string {
	var ids []string
	for _, inst := range s.runner.AllInstances() {
		if inst.Mode == anovelv1.Mode_MODE_GO_EXEC && inst.Live() {
			ids = append(ids, inst.ID)
		}
	}
	return ids
}

// stopAll kills the go-exec targets in ids with grace and tears down sessions.
// It runs on its own 30s context, so a cancelled RPC never leaves half-killed
// targets: a shutdown is a commitment.
func (s *Server) stopAll(ids []string, sessions []runner.InfraSessionRef, grace time.Duration) teardownOutcome {
	killCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return tearDown(ids, sessions,
		func(id string) error { return s.runner.Kill(killCtx, id, grace) },
		func(ref runner.InfraSessionRef) error {
			return s.runner.KillInfra(killCtx, ref.Stack, ref.Service, true /* force */)
		})
}

// teardownOutcome is what a shutdown achieved. The counts are successes, and
// anything that failed is named in failures.
type teardownOutcome struct {
	goExecKilled  int
	infraTornDown int
	failures      []string
}

// tearDown stops every go-exec target and, when sessions is non-empty, every
// infra session, and reports what each one did. The kill funcs are parameters
// so this can be driven without a live runner.
//
// go-exec targets go in parallel: killGoExec blocks until its grace expires, so
// one goroutine per target bounds the total by the longest grace rather than
// their sum. Infra teardown is serial and runs after them, since a session's
// containers outlive the targets that were talking to it.
func tearDown(
	goExecIDs []string,
	sessions []runner.InfraSessionRef,
	killGoExec func(id string) error,
	killInfra func(ref runner.InfraSessionRef) error,
) teardownOutcome {
	var (
		mu  sync.Mutex
		out teardownOutcome
		wg  sync.WaitGroup
	)
	for _, id := range goExecIDs {
		wg.Go(func() {
			err := killGoExec(id)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.failures = append(out.failures, fmt.Sprintf("go-exec target %s: %v", id, err))
				return
			}
			out.goExecKilled++
		})
	}
	wg.Wait()
	for _, ref := range sessions {
		if err := killInfra(ref); err != nil {
			out.failures = append(out.failures, fmt.Sprintf("infra %s/%s: %v", ref.Stack, ref.Service, err))
			continue
		}
		out.infraTornDown++
	}
	// Goroutine completion order is not stable, so the same failures would
	// otherwise come back in a different order run to run.
	sort.Strings(out.failures)
	return out
}

// =============================================================================
// Discovery
// =============================================================================

// ListStacks returns every stack the daemon manages: the registered ones
// discovery accepted, in registration order.
func (s *Server) ListStacks(_ context.Context, _ *anovelv1.ListStacksRequest) (*anovelv1.ListStacksResponse, error) {
	return &anovelv1.ListStacksResponse{Stacks: stacksToProto(s.stacks)}, nil
}

// ListServices returns every service in the requested stack. An empty
// Request.Stack means the default stack, and "*" the union across every
// registered stack, where each returned Service keeps its stack name.
func (s *Server) ListServices(_ context.Context, req *anovelv1.ListServicesRequest) (*anovelv1.ListServicesResponse, error) {
	scope, err := s.scope(req.GetStack())
	if err != nil {
		return nil, err
	}
	out := &anovelv1.ListServicesResponse{}
	for _, st := range scope {
		// One batched podman scan serves every service of the stack.
		states := s.liveInfraStates(st.Name)
		for _, svc := range st.Services {
			out.Services = append(out.Services, s.serviceToProto(svc, states))
		}
	}
	// Discovery lists each stack's services by name, so a stable sort on the
	// stack orders the union by stack, then service.
	slices.SortStableFunc(out.Services, func(a, b *anovelv1.Service) int { return cmp.Compare(a.GetStack(), b.GetStack()) })
	return out, nil
}

// DescribeService returns one service by (stack, service) lookup.
func (s *Server) DescribeService(_ context.Context, req *anovelv1.DescribeServiceRequest) (*anovelv1.DescribeServiceResponse, error) {
	svc, err := s.findService(req.GetStack(), req.GetService())
	if err != nil {
		return nil, err
	}
	return &anovelv1.DescribeServiceResponse{
		Service: s.serviceToProto(svc, s.liveInfraStates(svc.Stack)),
	}, nil
}

// GetTopology renders the dependency graph as ASCII text. An empty Service
// emits every service in the stack, one below the next.
func (s *Server) GetTopology(_ context.Context, req *anovelv1.GetTopologyRequest) (*anovelv1.GetTopologyResponse, error) {
	if req.GetService() != "" {
		svc, err := s.findService(req.GetStack(), req.GetService())
		if err != nil {
			return nil, err
		}
		return &anovelv1.GetTopologyResponse{
			Rendered: discovery.RenderTopology(svc),
		}, nil
	}
	st, err := s.findStack(req.GetStack())
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
	return &anovelv1.GetTopologyResponse{Rendered: b.String()}, nil
}

// =============================================================================
// Targets
// =============================================================================

// StartTarget brings up the named target in the requested mode, defaulting to
// go-exec, once its dependencies are ready.
func (s *Server) StartTarget(ctx context.Context, req *anovelv1.StartTargetRequest) (*anovelv1.StartTargetResponse, error) {
	mode := cmp.Or(req.GetMode(), anovelv1.Mode_MODE_GO_EXEC)
	if mode != anovelv1.Mode_MODE_GO_EXEC && mode != anovelv1.Mode_MODE_CONTAINER {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "unknown mode %v", mode)
	}
	t, svc, err := s.findTarget(req.GetTargetId())
	if err != nil {
		return nil, err
	}
	inst, err := s.runner.StartTarget(ctx, t, svc, mode)
	switch {
	case errors.Is(err, runner.ErrEnv):
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	case err != nil:
		return nil, connect.NewError(connect.CodeFailedPrecondition, err.Error()).WithCause(err)
	}
	return &anovelv1.StartTargetResponse{Target: targetToProto(t, inst)}, nil
}

// KillTarget stops the named instance with the requested SIGTERM grace,
// defaulting to 10s, where 0 means an immediate SIGKILL. It is idempotent, and
// the response carries no target when the runner never held an instance.
func (s *Server) KillTarget(ctx context.Context, req *anovelv1.KillTargetRequest) (*anovelv1.KillTargetResponse, error) {
	grace := 10 * time.Second
	if req.GetTimeout() != nil {
		grace = req.GetTimeout().AsDuration()
	}
	if err := s.runner.Kill(ctx, req.GetTargetId(), grace); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}
	resp := &anovelv1.KillTargetResponse{}
	inst, ok := s.runner.Instance(req.GetTargetId())
	if t, _ := s.stacks.Target(req.GetTargetId()); ok && t != nil {
		resp.Target = targetToProto(t, &inst)
	}
	return resp, nil
}

// RestartTarget is Kill and Start in one RPC. The kill completes before the
// start claims the slot, so mutual exclusion holds.
func (s *Server) RestartTarget(ctx context.Context, req *anovelv1.RestartTargetRequest) (*anovelv1.RestartTargetResponse, error) {
	id := req.GetTargetId()
	// A 10s grace, matching KillTarget.
	if err := s.runner.Kill(ctx, id, 10*time.Second); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}
	mode := req.GetMode()
	if prev, ok := s.runner.Instance(id); ok && mode == anovelv1.Mode_MODE_UNSPECIFIED {
		// Keep the previous mode when an instance record survives;
		// StartTarget defaults to go-exec otherwise.
		mode = prev.Mode
	}
	startResp, err := s.StartTarget(ctx, &anovelv1.StartTargetRequest{TargetId: id, Mode: mode})
	if err != nil {
		return nil, err
	}
	return &anovelv1.RestartTargetResponse{Target: startResp.GetTarget()}, nil
}

// =============================================================================
// Service infrastructure
// =============================================================================

// StartInfra brings up a service's infrastructure containers and auto-runs
// every one-shot target the long-runners depend on. It is idempotent.
func (s *Server) StartInfra(ctx context.Context, req *anovelv1.StartInfraRequest) (*anovelv1.StartInfraResponse, error) {
	svc, err := s.findService(req.GetStack(), req.GetService())
	if err != nil {
		return nil, err
	}
	if err := s.runner.StartInfra(ctx, svc.Stack, svc.Name, req.GetOneShotsMode()); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err.Error()).WithCause(err)
	}
	return &anovelv1.StartInfraResponse{
		Service: s.serviceToProto(svc, s.liveInfraStates(svc.Stack)),
	}, nil
}

// KillInfra refuses while any long-runner of the service is still running.
// --force cascade-kills those targets first.
func (s *Server) KillInfra(ctx context.Context, req *anovelv1.KillInfraRequest) (*anovelv1.KillInfraResponse, error) {
	svc, err := s.findService(req.GetStack(), req.GetService())
	if err != nil {
		return nil, err
	}
	if err := s.runner.KillInfra(ctx, svc.Stack, svc.Name, req.GetForce()); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err.Error()).WithCause(err)
	}
	return &anovelv1.KillInfraResponse{
		Service: s.serviceToProto(svc, s.liveInfraStates(svc.Stack)),
	}, nil
}

// KillInfraContainer stops one infra container by (stack, service, name),
// leaving the rest of the service's infra and its running targets untouched.
// The TUI uses it to manage infra entries like any other tab.
func (s *Server) KillInfraContainer(ctx context.Context, req *anovelv1.KillInfraContainerRequest) (*anovelv1.KillInfraContainerResponse, error) {
	in, err := s.infraContainer(ctx, req.GetStack(), req.GetService(), req.GetName(), "stop")
	if err != nil {
		return nil, err
	}
	return &anovelv1.KillInfraContainerResponse{Infra: in}, nil
}

// RestartInfraContainer restarts one infra container in place with `podman
// restart`, preserving its volume bindings.
func (s *Server) RestartInfraContainer(ctx context.Context, req *anovelv1.RestartInfraContainerRequest) (*anovelv1.RestartInfraContainerResponse, error) {
	in, err := s.infraContainer(ctx, req.GetStack(), req.GetService(), req.GetName(), "restart")
	if err != nil {
		return nil, err
	}
	return &anovelv1.RestartInfraContainerResponse{Infra: in}, nil
}

// infraContainer runs the podman verb on one declared infra container and
// returns its refreshed state.
func (s *Server) infraContainer(ctx context.Context, stack, service, name, verb string) (*anovelv1.Infra, error) {
	svc, err := s.findService(stack, service)
	if err != nil {
		return nil, err
	}
	in := svc.FindInfra(name)
	if in == nil {
		return nil, connect.Errorf(connect.CodeNotFound,
			"infra %q not declared in %s/%s", name, svc.Stack, svc.Name)
	}
	if err := s.runner.InfraContainer(ctx, svc.Stack, svc.Name, in.Name, verb); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err.Error()).WithCause(err)
	}
	return infraToProto(in, s.liveInfraStates(svc.Stack)), nil
}

// =============================================================================
// Logs
// =============================================================================

// StreamLogs server-streams log lines for a target:
//   - by default, one snapshot of current.log from start to end
//   - with --follow, that snapshot plus every new line until the process
//     terminates or the client disconnects
//   - with --run-id, an archived run, never followed
//
// Snapshot lines come first in file order, and followed lines after. Each
// LogLine carries its original timestamp and stream tag.
func (s *Server) StreamLogs(ctx context.Context, req *anovelv1.StreamLogsRequest, stream anovelv1connect.CoreServiceStreamLogsServerStream) error {
	tid := req.GetTargetId()
	// An infra log ID, "<stack>/<service>/infra/<name>", streams an infra
	// container's stdout through this same RPC. Those containers belong to
	// podman, so the branch reads `podman logs -f <cid>`.
	if stack, service, name, ok := parseInfraLogID(tid); ok {
		st, found := s.runner.InfraStatesOf(ctx, stack)[service+"/"+name]
		if !found || st.ContainerID == "" {
			return connect.Errorf(connect.CodeFailedPrecondition,
				"no container for %s/%s/%s — has infra-start been run?", stack, service, name)
		}
		return streamPodmanLogs(ctx, st.ContainerID, req.GetFollow(), stream)
	}
	tgt, _, err := s.findTarget(tid)
	if err != nil {
		return err
	}
	// Pick the file to stream.
	path := s.logs.CurrentPath(tgt.Stack, tgt.Service, tgt.Name)
	if req.GetRunId() != "" {
		path = s.logs.RunPath(tgt.Stack, tgt.Service, tgt.Name, req.GetRunId())
	}
	follow := req.GetFollow() && req.GetRunId() == ""

	// Under --follow, subscribe before reading the file so no line written
	// between snapshot and subscribe is lost. The unsub fires on handler exit,
	// keeping dead channels out of the target's subscriber slice.
	var sub <-chan logs.Line
	if follow {
		var unsub func()
		var ok bool
		if sub, unsub, ok = s.logs.Subscribe(tid); ok {
			defer unsub()
		}
	}

	if err := streamFileToClient(ctx, path, stream, req.GetStream()); err != nil {
		// A missing file means no run yet, so fall through to the follow
		// path when one was requested.
		if !follow {
			return connect.Errorf(connect.CodeNotFound,
				"read log %s: %v", path, err).WithCause(err)
		}
	}
	if sub == nil {
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
			if err := sendLogLine(stream.Send, req.GetStream(), ln); err != nil {
				return err
			}
		}
	}
}

// ListRuns returns the timestamps of archived runs for the target,
// newest first.
func (s *Server) ListRuns(_ context.Context, req *anovelv1.ListRunsRequest) (*anovelv1.ListRunsResponse, error) {
	tgt, _, err := s.findTarget(req.GetTargetId())
	if err != nil {
		return nil, err
	}
	return &anovelv1.ListRunsResponse{
		RunIds: s.logs.ListRuns(tgt.Stack, tgt.Service, tgt.Name),
	}, nil
}

// streamFileToClient reads a JSON-lines log file and sends every line matching
// the optional stream filter to the client, on both the snapshot and the
// archived-run path.
func streamFileToClient(ctx context.Context, path string, stream anovelv1connect.CoreServiceStreamLogsServerStream, filter anovelv1.LogStream) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return streamLines(ctx, f, stream.Send, filter, path)
}

// streamLines forwards one JSON-lines log stream to send.
//
// It reads a line at a time rather than through a json.Decoder: a decoder that
// fails part-way cannot be resumed, so the only recovery left is to stop, and
// stopping hands the viewer a short log that looks whole. The writer flushes
// per line, but a process killed mid-write leaves a partial final one, and that
// is the common case.
func streamLines(ctx context.Context, r io.Reader, send func(*anovelv1.LogLine) error, filter anovelv1.LogStream, path string) error {
	reader := bufio.NewReader(r)
	lineNo := 0
	for {
		// A client that went away is not a failure, so the snapshot just stops.
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		lineNo++
		raw, readErr := reader.ReadString('\n')
		if err := emitLogLine(send, filter, raw, lineNo); err != nil {
			return err
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("read log %s: %w", path, readErr)
		}
	}
}

// emitLogLine forwards one raw record, or a marker naming it when it does not
// parse. The marker is sent whatever the stream filter is: a record that cannot
// be decoded has no stream to match on, and a viewer that filters it away is
// back to a log that looks complete. Its timestamp is the read time, since the
// record carries none — clients render these in receive order.
func emitLogLine(send func(*anovelv1.LogLine) error, filter anovelv1.LogStream, raw string, lineNo int) error {
	trimmed := strings.TrimRight(raw, "\r\n")
	if trimmed == "" {
		return nil
	}
	var ln logs.Line
	if err := json.Unmarshal([]byte(trimmed), &ln); err != nil {
		return send(&anovelv1.LogLine{
			Ts:     timestamppb.New(time.Now()),
			Stream: anovelv1.LogStream_LOG_STREAM_STDERR,
			Line:   fmt.Sprintf("a-novel: log line %d is unreadable and was skipped (%v)", lineNo, err),
		})
	}
	return sendLogLine(send, filter, ln)
}

// sendLogLine forwards ln unless filter selects the other stream.
func sendLogLine(send func(*anovelv1.LogLine) error, filter anovelv1.LogStream, ln logs.Line) error {
	ps := lineStreamToProto(ln.Stream)
	if filter != anovelv1.LogStream_LOG_STREAM_UNSPECIFIED && ps != filter {
		return nil
	}
	return send(&anovelv1.LogLine{
		Ts:     timestamppb.New(ln.Ts),
		Stream: ps,
		Line:   ln.Line,
	})
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
func streamPodmanLogs(ctx context.Context, cid string, follow bool, stream anovelv1connect.CoreServiceStreamLogsServerStream) error {
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
		return connect.Errorf(connect.CodeInternal,
			"start podman logs %s: %v", cid, err).WithCause(err)
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
	scanner := lineScanner(pipeR)
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

// lineScanner scans r line by line. Command output carries long lines, such as
// panics and JSON dumps, so the per-token cap goes up to 1 MiB to keep the
// scanner from bisecting one.
func lineScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	return sc
}

// =============================================================================
// Environment
// =============================================================================

// GetEnv assembles the env block for one service, or for every service in
// scope, "*" covering every stack. It is read-only and never allocates a port,
// so a variable whose slot is unallocated appears with an empty value and the
// user still sees the shape.
func (s *Server) GetEnv(_ context.Context, req *anovelv1.GetEnvRequest) (*anovelv1.GetEnvResponse, error) {
	var services []*discovery.Service
	if req.GetService() != "" && req.GetStack() != allStacks {
		svc, err := s.findService(req.GetStack(), req.GetService())
		if err != nil {
			return nil, err
		}
		services = append(services, svc)
	} else {
		scope, err := s.scope(req.GetStack())
		if err != nil {
			return nil, err
		}
		for _, st := range scope {
			services = append(services, st.Services...)
		}
	}
	out := &anovelv1.GetEnvResponse{}
	for _, svc := range services {
		entries, err := s.envBuilder.ForService(svc, "")
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
		}
		for _, e := range entries {
			out.Entries = append(out.Entries, &anovelv1.EnvEntry{
				Stack:   svc.Stack,
				Service: svc.Name,
				Key:     e.Key,
				Value:   e.Value,
			})
		}
	}
	return out, nil
}

// =============================================================================
// Volumes
// =============================================================================

// ListVolumes returns one read-only Volume row per compose-declared volume on
// the service, with its size and backup count.
func (s *Server) ListVolumes(_ context.Context, req *anovelv1.ListVolumesRequest) (*anovelv1.ListVolumesResponse, error) {
	svc, err := s.findService(req.GetStack(), req.GetService())
	if err != nil {
		return nil, err
	}
	out := &anovelv1.ListVolumesResponse{}
	for _, v := range volumes.List(svc) {
		out.Volumes = append(out.Volumes, &anovelv1.Volume{
			Name:        v.Name,
			Service:     v.Service,
			Stack:       v.Stack,
			SizeBytes:   v.SizeBytes,
			BackupCount: v.BackupCount,
		})
	}
	return out, nil
}

// BackupVolume writes a tar.zst snapshot per volume. It refuses while the
// service is up, unless --force cascade-kills it first.
func (s *Server) BackupVolume(ctx context.Context, req *anovelv1.BackupVolumeRequest) (*anovelv1.BackupVolumeResponse, error) {
	svc, err := s.findService(req.GetStack(), req.GetService())
	if err != nil {
		return nil, err
	}
	if err := s.ensureServiceDown(ctx, svc, req.GetForce(), "backup"); err != nil {
		return nil, err
	}
	paths, err := volumes.Backup(svc, req.GetTag())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}
	return &anovelv1.BackupVolumeResponse{ArchivePaths: paths}, nil
}

// RestoreVolume replaces each volume from its matching backup. It refuses while
// the service is up.
func (s *Server) RestoreVolume(ctx context.Context, req *anovelv1.RestoreVolumeRequest) (*anovelv1.RestoreVolumeResponse, error) {
	svc, err := s.findService(req.GetStack(), req.GetService())
	if err != nil {
		return nil, err
	}
	if err := s.ensureServiceDown(ctx, svc, req.GetForce(), "restore"); err != nil {
		return nil, err
	}
	restored, err := volumes.Restore(svc, req.GetFrom())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}
	return &anovelv1.RestoreVolumeResponse{RestoredVolumes: restored}, nil
}

// ClearVolume destroys the service's volumes, backing them up first unless
// --no-backup. It refuses while the service is up.
func (s *Server) ClearVolume(ctx context.Context, req *anovelv1.ClearVolumeRequest) (*anovelv1.ClearVolumeResponse, error) {
	svc, err := s.findService(req.GetStack(), req.GetService())
	if err != nil {
		return nil, err
	}
	if err := s.ensureServiceDown(ctx, svc, req.GetForce(), "clear"); err != nil {
		return nil, err
	}
	cleared, err := volumes.Clear(svc, req.GetNoBackup())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}
	return &anovelv1.ClearVolumeResponse{ClearedVolumes: cleared}, nil
}

// ensureServiceDown is the pre-check every destructive volume operation shares.
// It refuses with a hint while any target or infra is up; with force it
// cascade-kills both first.
func (s *Server) ensureServiceDown(ctx context.Context, svc *discovery.Service, force bool, op string) error {
	var running []string
	for _, inst := range s.runner.AllInstances() {
		if inst.Stack == svc.Stack && inst.Service == svc.Name && inst.Phase != anovelv1.Phase_PHASE_TERMINATED {
			running = append(running, inst.Target)
		}
	}
	sess, _ := s.runner.InfraSession(svc.Stack, svc.Name)
	if len(running) == 0 && !sess.Up {
		return nil // all clear
	}
	if !force {
		return connect.Errorf(connect.CodeFailedPrecondition,
			"refusing to %s while %s/%s is up — kill targets + infra first (or pass --force):\n  running targets: %v\n  infra session up: %v",
			op, svc.Stack, svc.Name, running, sess.Up)
	}
	// A forced KillInfra cascades through the targets and the infra alike.
	if err := s.runner.KillInfra(ctx, svc.Stack, svc.Name, true /* force */); err != nil {
		return connect.Errorf(connect.CodeInternal,
			"force-stop before %s: %v", op, err).WithCause(err)
	}
	return nil
}

// =============================================================================
// Exec / debug
// =============================================================================

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
func (s *Server) Exec(ctx context.Context, req *anovelv1.ExecRequest, stream anovelv1connect.CoreServiceExecServerStream) error {
	id := req.GetTargetId()
	cmdv := req.GetCmd()
	if len(cmdv) == 0 {
		return connect.NewError(connect.CodeInvalidArgument, "exec: empty cmd")
	}
	if id == "" {
		return connect.NewError(connect.CodeInvalidArgument, "exec: target_id required")
	}
	tgt, _, err := s.findTarget(id)
	if err != nil {
		return err
	}
	// A running instance decides the mode. Without one, go-exec is the only
	// mode that works, there being no live container to attach to.
	var execCmd *exec.Cmd
	if inst, ok := s.runner.Instance(id); ok && inst.Phase == anovelv1.Phase_PHASE_RUNNING &&
		inst.Mode == anovelv1.Mode_MODE_CONTAINER {
		if inst.ContainerID == "" {
			return connect.NewError(connect.CodeFailedPrecondition,
				"exec: target running in container mode but containerId is empty")
		}
		execCmd = exec.CommandContext(ctx, "podman", append([]string{"exec", inst.ContainerID}, cmdv...)...)
	} else {
		// A go-exec sibling gets the same directory and env as the target.
		// The builder allocates when needed under the target's own consumer
		// ID, which the usual refcounting releases.
		envEntries, _, err := s.envBuilder.ForTarget(tgt)
		if err != nil {
			return connect.Errorf(connect.CodeInternal, "exec: env: %v", err).WithCause(err)
		}
		execCmd = exec.CommandContext(ctx, cmdv[0], cmdv[1:]...)
		// Run in the service directory so relative paths match what the
		// target sees.
		execCmd.Dir = tgt.ServiceDir()
		execCmd.Env = env.Environ(envEntries)
	}
	stdoutR, err := execCmd.StdoutPipe()
	if err != nil {
		return connect.Errorf(connect.CodeInternal, "exec: stdout pipe: %v", err).WithCause(err)
	}
	stderrR, err := execCmd.StderrPipe()
	if err != nil {
		return connect.Errorf(connect.CodeInternal, "exec: stderr pipe: %v", err).WithCause(err)
	}
	if err := execCmd.Start(); err != nil {
		return connect.Errorf(connect.CodeInternal, "exec: start: %v", err).WithCause(err)
	}
	// Two readers, one stream: a pair of goroutines line-scan stdout and
	// stderr and forward to the connect stream under a mutex, since
	// Stream.Send is unsafe for concurrent calls.
	var sendMu sync.Mutex
	send := func(msg *anovelv1.ExecOutput) {
		sendMu.Lock()
		defer sendMu.Unlock()
		_ = stream.Send(msg)
	}
	pump := func(r io.Reader, tag anovelv1.LogStream) {
		for sc := lineScanner(r); sc.Scan(); {
			send(&anovelv1.ExecOutput{Stream: tag, Line: sc.Text()})
		}
	}
	var wg sync.WaitGroup
	wg.Go(func() { pump(stdoutR, anovelv1.LogStream_LOG_STREAM_STDOUT) })
	wg.Go(func() { pump(stderrR, anovelv1.LogStream_LOG_STREAM_STDERR) })
	wg.Wait()

	waitErr := execCmd.Wait()

	code, err := execExitCode(waitErr)
	if err != nil {
		return connect.Errorf(connect.CodeInternal, "exec: wait: %v", err).WithCause(err)
	}

	if waitErr != nil {
		// The human-readable line survives being piped to a file, where the
		// process exit status does not. It rides the same stream as the
		// output, after everything the child wrote.
		send(&anovelv1.ExecOutput{Stream: anovelv1.LogStream_LOG_STREAM_STDERR, Line: "[exec exited: " + waitErr.Error() + "]"})
	}
	// The terminal message carries the exit code and no line, so the client can
	// propagate the child's status as its own.
	send(&anovelv1.ExecOutput{ExitCode: &code})

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
// The hint's listen port is a suggestion the daemon never binds. It is
// `PID + 20000`, high but unprivileged, so debugging several targets gives a
// unique port per PID with no allocation bookkeeping.
func (s *Server) Debug(_ context.Context, req *anovelv1.DebugRequest) (*anovelv1.DebugResponse, error) {
	id := req.GetTargetId()
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "debug: target_id required")
	}
	inst, ok := s.runner.Instance(id)
	if !ok {
		return nil, connect.Errorf(connect.CodeNotFound, "debug: target %q not tracked (start it first)", id)
	}
	if inst.Phase != anovelv1.Phase_PHASE_RUNNING {
		return nil, connect.Errorf(connect.CodeFailedPrecondition,
			"debug: target %q is not running (phase=%s)", id, inst.Phase)
	}
	if inst.Mode != anovelv1.Mode_MODE_GO_EXEC {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			"debug: only go-exec targets can be attached to (container-mode dlv requires in-image dlv + port-forward)")
	}
	port := inst.PID + 20000
	return &anovelv1.DebugResponse{Hint: fmt.Sprintf(
		"Attach with:\n  dlv attach %d --listen=:%d --headless --api-version=2\nThen connect from your editor (vscode: 'Connect to server' on port %d).",
		inst.PID, port, port,
	)}, nil
}

// =============================================================================
// Watch
// =============================================================================

// Watch streams every phase transition the runner emits, filtered by the
// caller's (stack, service, target_id) tuple. An empty filter field matches any
// value, so (stack, service, "") watches one service and three empty fields
// watch everything.
//
// The stream stays open until the client cancels its side, or the daemon shuts
// down and the runner's fanout stops. Nothing is emitted up front, so a caller
// needing the initial state pairs Watch with a ListServices snapshot.
func (s *Server) Watch(ctx context.Context, req *anovelv1.WatchRequest, stream anovelv1connect.CoreServiceWatchServerStream) error {
	wantStack := req.GetStack()
	wantService := req.GetService()
	wantTargetID := req.GetTargetId()
	filter := func(ev runner.PhaseEvent) bool {
		if wantStack != "" && wantStack != allStacks && ev.Stack != wantStack {
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
// StateEvent.description, such as "<id> running" or "<id> terminated
// (killed)". The field is opaque to the daemon, and a client may re-render
// from (old_phase, new_phase, exit_reason) instead, but the pre-baked summary
// is the cheap default for `ps --watch`. A phase without a summary, such as
// PENDING, falls back to its enum name.
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
