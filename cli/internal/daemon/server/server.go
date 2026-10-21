package server

import (
	"time"

	"connectrpc.com/connect/v2"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// Server must satisfy the generated handler interface, so an RPC added to the
// proto without a handler fails the build.
var _ anovelv1connect.CoreServiceHandler = (*Server)(nil)

// allStacks is the stack name that addresses every discovered stack at once.
const allStacks = "*"

// killGrace is the SIGTERM grace a kill gets unless the request sets one.
const killGrace = 10 * time.Second

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
