package server

import (
	"context"
	"errors"
	"strings"
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

// Config wires a Server to the daemon's subsystems. daemon.Run sets every
// field; a test leaves zero the fields of the RPCs it does not call.
type Config struct {
	Version     string           // daemon binary version, reported by Status
	SocketPath  string           // reported by Status
	BackupsRoot string           // where the volume RPCs keep backups
	Checkpoint  string           // where PrepareReinstall writes its checkpoint
	Stacks      discovery.Stacks // the managed stacks and their service trees
	Runner      *runner.Runner   // process and container supervisor
	Env         *env.Builder     // env block synthesis
	Logs        *logs.Store      // per-target log files and streaming hub
	// Stop ends the daemon's main loop; Shutdown and PrepareReinstall call it.
	// It is idempotent.
	Stop func()
	// Stopping closes once the daemon begins to stop, whatever stopped it.
	Stopping <-chan struct{}
}

// Server is the daemon's connect-rpc handler, owning the state that survives
// across RPC calls.
type Server struct {
	cfg       Config
	startedAt time.Time // for uptime calculation
}

// New constructs a Server wired as cfg says.
func New(cfg Config) *Server {
	return &Server{cfg: cfg, startedAt: time.Now()}
}

// errStopping is the cause a stream's context carries when the daemon stopped
// it.
var errStopping = errors.New("daemon stopping")

// streamContext returns a child of ctx that also ends once the daemon begins to
// stop, with errStopping as its cause. A log follow or a watch runs on it, so
// an open stream never holds the graceful shutdown.
func (s *Server) streamContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(ctx)
	go func() {
		select {
		case <-s.cfg.Stopping:
			cancel(errStopping)
		case <-ctx.Done():
		}
	}()
	return ctx, func() { cancel(nil) }
}

// streamEnd is the result of a stream whose context ended. A daemon stop ends
// it with CodeUnavailable, so the client reports the stop. A client that went
// away ends it with nil.
func streamEnd(ctx context.Context) error {
	if errors.Is(context.Cause(ctx), errStopping) {
		return connect.NewError(connect.CodeUnavailable, errStopping.Error())
	}
	return nil
}

// findStack returns the discovered Stack named `name`, or the default stack
// when the name is empty. An unknown name yields a connect.Error.
func (s *Server) findStack(name string) (*discovery.Stack, error) {
	if len(s.cfg.Stacks) == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			"no stacks registered (set A_NOVEL_STACKS)")
	}
	if st := s.cfg.Stacks.Stack(name); st != nil {
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
	if svc := s.cfg.Stacks.Service(st.Name, serviceName); svc != nil {
		return svc, nil
	}
	return nil, connect.Errorf(connect.CodeNotFound,
		"service %q not found in stack %q", serviceName, st.Name)
}

// scope returns the stacks a request names: every stack for "*", otherwise
// the one findStack resolves.
func (s *Server) scope(name string) ([]*discovery.Stack, error) {
	if name == allStacks {
		return s.cfg.Stacks, nil
	}
	st, err := s.findStack(name)
	if err != nil {
		return nil, err
	}
	return []*discovery.Stack{st}, nil
}

// canonicalID resolves a target or infra log ID a client shortened, against
// the default stack: "<service>/<target>" and "<service>/infra/<name>" gain
// the default stack's name in front. Any other ID comes back as it is. A
// handler resolves the ID first, so a shorthand reaches the same target as
// every other request that names no stack.
func (s *Server) canonicalID(id string) string {
	parts := strings.Split(id, "/")
	isShorthand := len(parts) == 2 || len(parts) == 3 && parts[1] == "infra"
	if st := s.cfg.Stacks.Stack(""); isShorthand && st != nil {
		return st.Name + "/" + id
	}
	return id
}

// findTarget returns the discovered target with the given ID.
func (s *Server) findTarget(id string) (*discovery.Target, *discovery.Service, error) {
	if t, svc := s.cfg.Stacks.Target(id); t != nil {
		return t, svc, nil
	}
	return nil, nil, connect.Errorf(connect.CodeNotFound, "unknown target %q", id)
}
