package server

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/a-novel-kit/stack/cli/internal/daemon/reinstall"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

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
// with the ports to relaunch it on, fsyncs it, stops those targets, then signals the
// daemon to shut down. Containers stay out, surviving the daemon's death on
// their own.
//
// A second PrepareReinstall is rejected while one is pending, guarded by the
// checkpoint file's existence.
func (s *Server) PrepareReinstall(_ context.Context, _ *anovelv1.PrepareReinstallRequest) (*anovelv1.PrepareReinstallResponse, error) {
	if err := reinstall.EnsureSinglePending(); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err.Error()).WithCause(err)
	}
	// A running target still holds its port claims, so the checkpoint records
	// those ports and the new daemon relaunches it on them. It records nothing
	// else: the relaunch rebuilds the env, so no secret reaches the file.
	ids := s.liveGoExecIDs()
	cp := reinstall.Checkpoint{}
	for _, id := range ids {
		if t, _ := s.stacks.Target(id); t != nil {
			cp.GoExec = append(cp.GoExec, reinstall.GoExecCheckpoint{TargetID: id, Env: s.envBuilder.HeldPorts(t)})
		}
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
// The response names every target that could not be stopped, so a partial
// shutdown never reads as a success. The daemon stops after the response is
// sent.
func (s *Server) Shutdown(_ context.Context, req *anovelv1.ShutdownRequest) (*anovelv1.ShutdownResponse, error) {
	var sessions []runner.InfraSessionRef
	if req.GetForce() {
		sessions = s.runner.ActiveInfraSessions()
	}
	outcome := s.stopAll(s.liveGoExecIDs(), sessions, killGrace)
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
// go-exec targets stop in parallel, since a kill blocks for up to its grace.
// The total is then bounded by the longest grace. Infra sessions stop in
// parallel too, after every target that talked to them is gone.
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
		wg.Go(func() {
			err := killInfra(ref)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.failures = append(out.failures, fmt.Sprintf("infra %s/%s: %v", ref.Stack, ref.Service, err))
				return
			}
			out.infraTornDown++
		})
	}
	wg.Wait()
	// Goroutine completion order is not stable, so the same failures would
	// otherwise come back in a different order run to run.
	slices.Sort(out.failures)
	return out
}
