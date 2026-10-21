package server

import (
	"cmp"
	"context"
	"errors"

	"connectrpc.com/connect/v2"

	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

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
	grace := killGrace
	if req.GetTimeout() != nil {
		grace = req.GetTimeout().AsDuration()
	}
	if err := s.runner.Kill(ctx, req.GetTargetId(), grace); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}
	resp := &anovelv1.KillTargetResponse{}
	inst, tracked := s.runner.Instance(req.GetTargetId())
	t, _ := s.stacks.Target(req.GetTargetId())
	if tracked && t != nil {
		resp.Target = targetToProto(t, &inst)
	}
	return resp, nil
}

// RestartTarget is Kill and Start in one RPC. The kill completes before the
// start claims the slot, so mutual exclusion holds.
func (s *Server) RestartTarget(ctx context.Context, req *anovelv1.RestartTargetRequest) (*anovelv1.RestartTargetResponse, error) {
	id := req.GetTargetId()
	if err := s.runner.Kill(ctx, id, killGrace); err != nil {
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
