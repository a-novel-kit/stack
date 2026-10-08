// Package rpc wraps the generated connect-rpc client with the daemon's
// unix-socket transport. CLI and TUI both consume Client; neither speaks
// connect-rpc primitives directly.
package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// Client is a thin wrapper around the generated CoreServiceClient with
// unix-socket dialing and helpful error mapping.
type Client struct {
	core anovelv1connect.CoreServiceClient
	path string
}

// New constructs a Client targeting the daemon at socketPath. Pass "" to use
// the default location (paths.Socket()).
func New(socketPath string) *Client {
	if socketPath == "" {
		socketPath = paths.Socket()
	}
	httpClient := &http.Client{
		Transport: &http.Transport{
			// Unix-socket dialer: the network and address arguments are
			// discarded, and so are the scheme and host of the URL handed
			// to connect-rpc, since every call dials the same socket.
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", socketPath)
			},
		},
	}
	core := anovelv1connect.NewCoreServiceClient(connect.NewClient(connecthttp.NewTransport(
		httpClient,
		"http://localhost", // host is ignored; transport dials the socket
	)))
	return &Client{core: core, path: socketPath}
}

// SocketPath returns the socket path this client dials. Useful for error
// messages.
func (c *Client) SocketPath() string { return c.path }

// Ping is the cheapest RPC, used to detect an already-running daemon. A
// transport failure comes back as a [NotRunningError] (test it with
// [IsNotRunning]) so callers can tell a down daemon from one that answered
// with an error.
func (c *Client) Ping(ctx context.Context) (*anovelv1.PingResponse, error) {
	resp, err := c.core.Ping(ctx, &anovelv1.PingRequest{})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// Status returns the full daemon-side state in one round trip.
func (c *Client) Status(ctx context.Context) (*anovelv1.StatusResponse, error) {
	resp, err := c.core.Status(ctx, &anovelv1.StatusRequest{})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// ListStacks returns every registered stack.
func (c *Client) ListStacks(ctx context.Context) (*anovelv1.ListStacksResponse, error) {
	resp, err := c.core.ListStacks(ctx, &anovelv1.ListStacksRequest{})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// ListServices returns every service in `stack` (use "" for default,
// "*" for all stacks).
func (c *Client) ListServices(ctx context.Context, stack string) (*anovelv1.ListServicesResponse, error) {
	resp, err := c.core.ListServices(ctx, &anovelv1.ListServicesRequest{Stack: stack})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// DescribeService returns one service in detail.
func (c *Client) DescribeService(ctx context.Context, stack, service string) (*anovelv1.DescribeServiceResponse, error) {
	resp, err := c.core.DescribeService(ctx, &anovelv1.DescribeServiceRequest{
		Stack:   stack,
		Service: service,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// GetTopology returns the dependency-graph text for one service (or every
// service in the stack if `service` is empty).
func (c *Client) GetTopology(ctx context.Context, stack, service string) (*anovelv1.GetTopologyResponse, error) {
	resp, err := c.core.GetTopology(ctx, &anovelv1.GetTopologyRequest{
		Stack:   stack,
		Service: service,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// StartTarget brings up the named target in the requested mode (use
// MODE_UNSPECIFIED to default to go-exec).
func (c *Client) StartTarget(ctx context.Context, id string, mode anovelv1.Mode) (*anovelv1.StartTargetResponse, error) {
	resp, err := c.core.StartTarget(ctx, &anovelv1.StartTargetRequest{
		TargetId: id,
		Mode:     mode,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// KillTarget stops the named instance, using timeout as the SIGTERM grace
// period. A timeout of 0 leaves the field unset so the daemon applies its own
// default grace; any positive value overrides it.
func (c *Client) KillTarget(ctx context.Context, id string, timeout time.Duration) (*anovelv1.KillTargetResponse, error) {
	req := &anovelv1.KillTargetRequest{TargetId: id}
	if timeout > 0 {
		req.Timeout = durationpb.New(timeout)
	}
	resp, err := c.core.KillTarget(ctx, req)
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// RestartTarget kills then starts the target in one RPC.
func (c *Client) RestartTarget(ctx context.Context, id string, mode anovelv1.Mode) (*anovelv1.RestartTargetResponse, error) {
	resp, err := c.core.RestartTarget(ctx, &anovelv1.RestartTargetRequest{
		TargetId: id,
		Mode:     mode,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// GetEnv returns the env entries for a service (or every service across
// every stack with allStacks=true; or every service in the requested
// stack with service="").
func (c *Client) GetEnv(ctx context.Context, stack, service, only string, allStacks bool) (*anovelv1.GetEnvResponse, error) {
	resp, err := c.core.GetEnv(ctx, &anovelv1.GetEnvRequest{
		Stack:     stack,
		Service:   service,
		Only:      only,
		AllStacks: allStacks,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// StartInfra brings up the service's infrastructure containers and
// auto-runs its one-shots.
func (c *Client) StartInfra(ctx context.Context, stack, service string, oneShotsMode anovelv1.Mode) (*anovelv1.StartInfraResponse, error) {
	resp, err := c.core.StartInfra(ctx, &anovelv1.StartInfraRequest{
		Stack:        stack,
		Service:      service,
		OneShotsMode: oneShotsMode,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// KillInfra tears down the service's infrastructure (refuses if any
// long-runner is still up unless force=true).
func (c *Client) KillInfra(ctx context.Context, stack, service string, force bool) (*anovelv1.KillInfraResponse, error) {
	resp, err := c.core.KillInfra(ctx, &anovelv1.KillInfraRequest{
		Stack:   stack,
		Service: service,
		Force:   force,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// KillInfraContainer stops one infra container by name, leaving the rest
// of the service up. The TUI uses it for tab-level infra lifecycle.
func (c *Client) KillInfraContainer(ctx context.Context, stack, service, name string) (*anovelv1.KillInfraContainerResponse, error) {
	resp, err := c.core.KillInfraContainer(ctx, &anovelv1.KillInfraContainerRequest{
		Stack:   stack,
		Service: service,
		Name:    name,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// RestartInfraContainer issues `podman restart` for one infra container,
// which preserves the volume bindings and so beats kill+infra-start.
func (c *Client) RestartInfraContainer(ctx context.Context, stack, service, name string) (*anovelv1.RestartInfraContainerResponse, error) {
	resp, err := c.core.RestartInfraContainer(ctx, &anovelv1.RestartInfraContainerRequest{
		Stack:   stack,
		Service: service,
		Name:    name,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// StreamLogs returns the log lines of a target; ranging over them drains
// and then closes the stream. `runID` of "" means
// current.log; non-empty selects an archived run. `follow` keeps the
// stream open for new lines past EOF (snapshot + subscribe).
func (c *Client) StreamLogs(ctx context.Context, targetID, runID string, follow bool, streamFilter anovelv1.LogStream) (iter.Seq2[*anovelv1.LogLine, error], error) {
	stream, err := c.core.StreamLogs(ctx, &anovelv1.StreamLogsRequest{
		TargetId: targetID,
		Follow:   follow,
		RunId:    runID,
		Stream:   streamFilter,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return receive(stream.Receive, stream.Close), nil
}

// ListRuns returns the timestamps of archived runs for a target,
// newest first.
func (c *Client) ListRuns(ctx context.Context, targetID string) (*anovelv1.ListRunsResponse, error) {
	resp, err := c.core.ListRuns(ctx, &anovelv1.ListRunsRequest{
		TargetId: targetID,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// ListVolumes returns one Volume per compose-declared volume on the
// service, with current size + backup count.
func (c *Client) ListVolumes(ctx context.Context, stack, service string) (*anovelv1.ListVolumesResponse, error) {
	resp, err := c.core.ListVolumes(ctx, &anovelv1.ListVolumesRequest{
		Stack: stack, Service: service,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// BackupVolume writes a tar.zst snapshot of every volume on the service.
// `tag` is optional, embedded in the filename when non-empty. `force`
// cascade-kills the service before backing up.
func (c *Client) BackupVolume(ctx context.Context, stack, service, tag string, force bool) (*anovelv1.BackupVolumeResponse, error) {
	resp, err := c.core.BackupVolume(ctx, &anovelv1.BackupVolumeRequest{
		Stack: stack, Service: service, Tag: tag, Force: force,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// RestoreVolume replaces volumes from a backup. `from` is a timestamp
// prefix; empty means "latest".
func (c *Client) RestoreVolume(ctx context.Context, stack, service, from string, force bool) (*anovelv1.RestoreVolumeResponse, error) {
	resp, err := c.core.RestoreVolume(ctx, &anovelv1.RestoreVolumeRequest{
		Stack: stack, Service: service, From: from, Force: force,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// ClearVolume destroys volumes (auto-backups first unless noBackup).
func (c *Client) ClearVolume(ctx context.Context, stack, service string, noBackup, force bool) (*anovelv1.ClearVolumeResponse, error) {
	resp, err := c.core.ClearVolume(ctx, &anovelv1.ClearVolumeRequest{
		Stack: stack, Service: service, NoBackup: noBackup, Force: force,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// PrepareReinstall asks the daemon to checkpoint + shut down.
func (c *Client) PrepareReinstall(ctx context.Context) (*anovelv1.PrepareReinstallResponse, error) {
	resp, err := c.core.PrepareReinstall(ctx, &anovelv1.PrepareReinstallRequest{})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// Shutdown is the no-checkpoint daemon stop. `force=true` cascade-kills
// every service's infra + targets. Both variants signal the daemon to
// exit after the response is sent; callers poll Ping to know when the
// socket is gone.
func (c *Client) Shutdown(ctx context.Context, force bool) (*anovelv1.ShutdownResponse, error) {
	resp, err := c.core.Shutdown(ctx, &anovelv1.ShutdownRequest{Force: force})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// Watch opens a server-streaming subscription to phase events. Filters:
// empty stack means "any stack"; empty service means "any service in the
// stack"; empty targetID means "every target". The events end when the
// caller's context is canceled or the daemon exits.
func (c *Client) Watch(ctx context.Context, stack, service, targetID string) (iter.Seq2[*anovelv1.StateEvent, error], error) {
	stream, err := c.core.Watch(ctx, &anovelv1.WatchRequest{
		Stack:    stack,
		Service:  service,
		TargetId: targetID,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return receive(stream.Receive, stream.Close), nil
}

// Exec runs cmd inside the target's runtime (container exec, or a sibling
// process in the target's env for go-exec). It yields one ExecOutput per
// stdout/stderr line until the command ends.
func (c *Client) Exec(ctx context.Context, targetID string, cmdv []string) (iter.Seq2[*anovelv1.ExecOutput, error], error) {
	stream, err := c.core.Exec(ctx, &anovelv1.ExecRequest{
		TargetId: targetID,
		Cmd:      cmdv,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return receive(stream.Receive, stream.Close), nil
}

// Debug returns the dlv-attach hint for a running go-exec target.
func (c *Client) Debug(ctx context.Context, targetID string) (*anovelv1.DebugResponse, error) {
	resp, err := c.core.Debug(ctx, &anovelv1.DebugRequest{
		TargetId: targetID,
	})
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp, nil
}

// IsNotRunning reports whether err signals an unreachable daemon: a missing
// socket or a refused connection.
func IsNotRunning(err error) bool {
	var nrErr *NotRunningError
	return errors.As(err, &nrErr)
}

// NotRunningError wraps a transport-level failure. Carries the socket path
// for actionable error messages.
type NotRunningError struct {
	SocketPath string
	Cause      error
}

func (e *NotRunningError) Error() string {
	return fmt.Sprintf("a-novel daemon not reachable at %s: %v (run `a-novel core start` first)", e.SocketPath, e.Cause)
}
func (e *NotRunningError) Unwrap() error { return e.Cause }

// mapErr turns a transport failure into a typed error the CLI can report in
// its own words.
func (c *Client) mapErr(err error) error {
	// Connect errors carry a code; the transport-level failures we care
	// about (socket missing, refused) come through as Unavailable.
	var ce *connect.Error
	if errors.As(err, &ce) && ce.Code() == connect.CodeUnavailable {
		return &NotRunningError{SocketPath: c.path, Cause: err}
	}
	// Raw net.OpError (e.g., dial unix: connect: no such file or directory)
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return &NotRunningError{SocketPath: c.path, Cause: err}
	}
	return err
}

// receive yields a server stream's messages until it ends, then closes the
// stream. A failure other than io.EOF is yielded once, as the last value.
func receive[T any](next func() (*T, error), closeStream func() error) iter.Seq2[*T, error] {
	return func(yield func(*T, error) bool) {
		defer func() { _ = closeStream() }()

		for {
			msg, err := next()
			if errors.Is(err, io.EOF) || !yield(msg, err) || err != nil {
				return
			}
		}
	}
}
