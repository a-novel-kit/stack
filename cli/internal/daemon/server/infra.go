package server

import (
	"context"

	"connectrpc.com/connect/v2"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

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

// KillInfra tears down a service's infra. It refuses while a target of the
// service is live, unless force stops those targets first.
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
