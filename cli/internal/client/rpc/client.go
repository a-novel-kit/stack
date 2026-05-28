// Package rpc wraps the generated connect-rpc client with the daemon's
// unix-socket transport. CLI and TUI both consume Client; neither speaks
// connect-rpc primitives directly.
package rpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
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
			// Unix socket dialer: replace net+addr with our path. The
			// scheme/host in the URL we pass to connect-rpc are just
			// placeholders since we always dial the same socket.
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", socketPath)
			},
		},
	}
	core := anovelv1connect.NewCoreServiceClient(
		httpClient,
		"http://localhost", // host is ignored; transport dials the socket
	)
	return &Client{core: core, path: socketPath}
}

// SocketPath returns the socket path this client dials. Useful for error
// messages.
func (c *Client) SocketPath() string { return c.path }

// =============================================================================
// Daemon control
// =============================================================================

// Ping is the cheapest possible RPC — used to detect an already-running
// daemon. Returns the connection error wrapped with a NotRunning helper so
// callers can distinguish "daemon down" from "daemon errored".
func (c *Client) Ping(ctx context.Context) (*anovelv1.PingResponse, error) {
	resp, err := c.core.Ping(ctx, connect.NewRequest(&anovelv1.PingRequest{}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// Status returns the full daemon-side state in one round trip.
func (c *Client) Status(ctx context.Context) (*anovelv1.StatusResponse, error) {
	resp, err := c.core.Status(ctx, connect.NewRequest(&anovelv1.StatusRequest{}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// ListStacks returns every registered stack.
func (c *Client) ListStacks(ctx context.Context) (*anovelv1.ListStacksResponse, error) {
	resp, err := c.core.ListStacks(ctx, connect.NewRequest(&anovelv1.ListStacksRequest{}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// ListServices returns every service in `stack` (use "" for default,
// "*" for all stacks).
func (c *Client) ListServices(ctx context.Context, stack string) (*anovelv1.ListServicesResponse, error) {
	resp, err := c.core.ListServices(ctx, connect.NewRequest(&anovelv1.ListServicesRequest{Stack: stack}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// DescribeService returns one service in detail.
func (c *Client) DescribeService(ctx context.Context, stack, service string) (*anovelv1.DescribeServiceResponse, error) {
	resp, err := c.core.DescribeService(ctx, connect.NewRequest(&anovelv1.DescribeServiceRequest{
		Stack:   stack,
		Service: service,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// GetTopology returns the dependency-graph text for one service (or every
// service in the stack if `service` is empty).
func (c *Client) GetTopology(ctx context.Context, stack, service string) (*anovelv1.GetTopologyResponse, error) {
	resp, err := c.core.GetTopology(ctx, connect.NewRequest(&anovelv1.GetTopologyRequest{
		Stack:   stack,
		Service: service,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// StartTarget brings up the named target in the requested mode (use
// MODE_UNSPECIFIED to default to go-exec).
func (c *Client) StartTarget(ctx context.Context, id string, mode anovelv1.Mode) (*anovelv1.StartTargetResponse, error) {
	resp, err := c.core.StartTarget(ctx, connect.NewRequest(&anovelv1.StartTargetRequest{
		TargetId: id,
		Mode:     mode,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// KillTarget stops the named instance with the requested SIGTERM grace.
// `timeout` of 0 → daemon default (10s); for immediate SIGKILL, pass a
// non-nil zero-duration; for any specific value, pass it.
func (c *Client) KillTarget(ctx context.Context, id string, timeout time.Duration) (*anovelv1.KillTargetResponse, error) {
	req := &anovelv1.KillTargetRequest{TargetId: id}
	if timeout > 0 {
		req.Timeout = durationpb.New(timeout)
	}
	resp, err := c.core.KillTarget(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// RestartTarget kills then starts the target in one RPC.
func (c *Client) RestartTarget(ctx context.Context, id string, mode anovelv1.Mode) (*anovelv1.RestartTargetResponse, error) {
	resp, err := c.core.RestartTarget(ctx, connect.NewRequest(&anovelv1.RestartTargetRequest{
		TargetId: id,
		Mode:     mode,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// GetEnv returns the env entries for a service (or every service across
// every stack with allStacks=true; or every service in the requested
// stack with service="").
func (c *Client) GetEnv(ctx context.Context, stack, service, only string, allStacks bool) (*anovelv1.GetEnvResponse, error) {
	resp, err := c.core.GetEnv(ctx, connect.NewRequest(&anovelv1.GetEnvRequest{
		Stack:     stack,
		Service:   service,
		Only:      only,
		AllStacks: allStacks,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// StartInfra brings up the service's infrastructure containers and
// auto-runs its one-shots.
func (c *Client) StartInfra(ctx context.Context, stack, service string, oneShotsMode anovelv1.Mode) (*anovelv1.StartInfraResponse, error) {
	resp, err := c.core.StartInfra(ctx, connect.NewRequest(&anovelv1.StartInfraRequest{
		Stack:        stack,
		Service:      service,
		OneShotsMode: oneShotsMode,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// KillInfra tears down the service's infrastructure (refuses if any
// long-runner is still up unless force=true).
func (c *Client) KillInfra(ctx context.Context, stack, service string, force bool) (*anovelv1.KillInfraResponse, error) {
	resp, err := c.core.KillInfra(ctx, connect.NewRequest(&anovelv1.KillInfraRequest{
		Stack:   stack,
		Service: service,
		Force:   force,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// KillInfraContainer stops one infra container (by name) without
// affecting the rest of the service. Used by the TUI for tab-level
// infra lifecycle.
func (c *Client) KillInfraContainer(ctx context.Context, stack, service, name string) (*anovelv1.KillInfraContainerResponse, error) {
	resp, err := c.core.KillInfraContainer(ctx, connect.NewRequest(&anovelv1.KillInfraContainerRequest{
		Stack:   stack,
		Service: service,
		Name:    name,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// RestartInfraContainer issues `podman restart` for one infra
// container — faster than kill+infra-start because it preserves
// volume bindings.
func (c *Client) RestartInfraContainer(ctx context.Context, stack, service, name string) (*anovelv1.RestartInfraContainerResponse, error) {
	resp, err := c.core.RestartInfraContainer(ctx, connect.NewRequest(&anovelv1.RestartInfraContainerRequest{
		Stack:   stack,
		Service: service,
		Name:    name,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// StreamLogs returns a server-stream of log lines. Caller drains via
// Receive()/Msg() and Close()s on done. `runID` of "" means
// current.log; non-empty selects an archived run. `follow` keeps the
// stream open for new lines past EOF (snapshot + subscribe).
func (c *Client) StreamLogs(ctx context.Context, targetID, runID string, follow bool, streamFilter anovelv1.LogStream) (*connect.ServerStreamForClient[anovelv1.LogLine], error) {
	stream, err := c.core.StreamLogs(ctx, connect.NewRequest(&anovelv1.StreamLogsRequest{
		TargetId: targetID,
		Follow:   follow,
		RunId:    runID,
		Stream:   streamFilter,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return stream, nil
}

// ListRuns returns the timestamps of archived runs for a target,
// newest first.
func (c *Client) ListRuns(ctx context.Context, targetID string) (*anovelv1.ListRunsResponse, error) {
	resp, err := c.core.ListRuns(ctx, connect.NewRequest(&anovelv1.ListRunsRequest{
		TargetId: targetID,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// ListVolumes returns one Volume per compose-declared volume on the
// service, with current size + backup count.
func (c *Client) ListVolumes(ctx context.Context, stack, service string) (*anovelv1.ListVolumesResponse, error) {
	resp, err := c.core.ListVolumes(ctx, connect.NewRequest(&anovelv1.ListVolumesRequest{
		Stack: stack, Service: service,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// BackupVolume writes a tar.zst snapshot of every volume on the service.
// `tag` is optional, embedded in the filename when non-empty. `force`
// cascade-kills the service before backing up.
func (c *Client) BackupVolume(ctx context.Context, stack, service, tag string, force bool) (*anovelv1.BackupVolumeResponse, error) {
	resp, err := c.core.BackupVolume(ctx, connect.NewRequest(&anovelv1.BackupVolumeRequest{
		Stack: stack, Service: service, Tag: tag, Force: force,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// RestoreVolume replaces volumes from a backup. `from` is a timestamp
// prefix; empty means "latest".
func (c *Client) RestoreVolume(ctx context.Context, stack, service, from string, force bool) (*anovelv1.RestoreVolumeResponse, error) {
	resp, err := c.core.RestoreVolume(ctx, connect.NewRequest(&anovelv1.RestoreVolumeRequest{
		Stack: stack, Service: service, From: from, Force: force,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// ClearVolume destroys volumes (auto-backups first unless noBackup).
func (c *Client) ClearVolume(ctx context.Context, stack, service string, noBackup, force bool) (*anovelv1.ClearVolumeResponse, error) {
	resp, err := c.core.ClearVolume(ctx, connect.NewRequest(&anovelv1.ClearVolumeRequest{
		Stack: stack, Service: service, NoBackup: noBackup, Force: force,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// PrepareReinstall asks the daemon to checkpoint + shut down.
func (c *Client) PrepareReinstall(ctx context.Context) (*anovelv1.PrepareReinstallResponse, error) {
	resp, err := c.core.PrepareReinstall(ctx, connect.NewRequest(&anovelv1.PrepareReinstallRequest{}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// Shutdown is the no-checkpoint daemon stop. `force=true` cascade-kills
// every service's infra + targets. Both variants signal the daemon to
// exit after the response is sent; callers poll Ping to know when the
// socket is gone.
func (c *Client) Shutdown(ctx context.Context, force bool) (*anovelv1.ShutdownResponse, error) {
	resp, err := c.core.Shutdown(ctx, connect.NewRequest(&anovelv1.ShutdownRequest{Force: force}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// Watch opens a server-streaming subscription to phase events. Filters:
// empty stack means "any stack"; empty service means "any service in the
// stack"; empty targetID means "every target". The returned stream
// closes when the caller's context is cancelled or the daemon exits.
func (c *Client) Watch(ctx context.Context, stack, service, targetID string) (*connect.ServerStreamForClient[anovelv1.StateEvent], error) {
	stream, err := c.core.Watch(ctx, connect.NewRequest(&anovelv1.WatchRequest{
		Stack:    stack,
		Service:  service,
		TargetId: targetID,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return stream, nil
}

// Exec runs cmd inside the target's runtime (container exec, or a sibling
// process in the target's env for go-exec). The returned server-stream
// emits one ExecOutput per stdout/stderr line; closes on natural EOF.
func (c *Client) Exec(ctx context.Context, targetID string, cmdv []string) (*connect.ServerStreamForClient[anovelv1.ExecOutput], error) {
	stream, err := c.core.Exec(ctx, connect.NewRequest(&anovelv1.ExecRequest{
		TargetId: targetID,
		Cmd:      cmdv,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return stream, nil
}

// Debug returns the dlv-attach hint for a running go-exec target.
func (c *Client) Debug(ctx context.Context, targetID string) (*anovelv1.DebugResponse, error) {
	resp, err := c.core.Debug(ctx, connect.NewRequest(&anovelv1.DebugRequest{
		TargetId: targetID,
	}))
	if err != nil {
		return nil, c.mapErr(err)
	}
	return resp.Msg, nil
}

// IsNotRunning reports whether err signals the daemon is unreachable (socket
// missing, connection refused). Distinct from a daemon-side error.
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

// mapErr classifies errors so the CLI can show a helpful message rather than
// a raw transport error.
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
