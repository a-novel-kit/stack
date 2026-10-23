package server

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

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
