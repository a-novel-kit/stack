package server

import (
	"context"

	"connectrpc.com/connect/v2"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// GetEnv assembles the env block for one service, or for every service in
// scope, "*" covering every stack. It is read-only and never allocates a port,
// so a variable whose slot is unallocated appears with an empty value and the
// user still sees the shape.
func (s *Server) GetEnv(_ context.Context, req *anovelv1.GetEnvRequest) (*anovelv1.GetEnvResponse, error) {
	services, err := s.envServices(req.GetStack(), req.GetService())
	if err != nil {
		return nil, err
	}
	out := &anovelv1.GetEnvResponse{}
	for _, svc := range services {
		entries, err := s.envBuilder.ForService(svc, "")
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
		}
		for _, e := range entries {
			out.Entries = append(out.Entries, envEntryToProto(svc, e))
		}
	}
	return out, nil
}

// envServices returns the services a GetEnv request covers: the named one, or
// every service of the stacks in scope.
func (s *Server) envServices(stack, service string) ([]*discovery.Service, error) {
	if service != "" && stack != allStacks {
		svc, err := s.findService(stack, service)
		if err != nil {
			return nil, err
		}
		return []*discovery.Service{svc}, nil
	}
	scope, err := s.scope(stack)
	if err != nil {
		return nil, err
	}
	var services []*discovery.Service
	for _, st := range scope {
		services = append(services, st.Services...)
	}
	return services, nil
}
