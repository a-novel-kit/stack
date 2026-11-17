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

	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// New returns a client of the daemon listening on paths.Socket(). Call and
// Stream issue its RPCs, so a daemon that does not answer reads as a
// NotRunningError.
func New() anovelv1connect.CoreServiceClient {
	socketPath := paths.Socket()
	httpClient := &http.Client{
		Transport: &http.Transport{
			// Unix-socket dialer: the network and address arguments are
			// discarded, and so are the scheme and host of the URL handed
			// to connect-rpc, since every call dials the same socket.
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				// A socket another user created could answer as the daemon.
				if err := paths.CheckOwner(socketPath); err != nil {
					return nil, err
				}
				return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", socketPath)
			},
		},
	}
	return anovelv1connect.NewCoreServiceClient(connect.NewClient(connecthttp.NewTransport(
		httpClient,
		"http://localhost", // host is ignored; transport dials the socket
	)))
}

// Call issues one RPC of a client New returned, as in
// rpc.Call(ctx, c.StartTarget, &anovelv1.StartTargetRequest{...}). A daemon
// that does not answer fails it with a NotRunningError.
func Call[Req, Resp any](ctx context.Context, rpc func(context.Context, *Req) (*Resp, error), req *Req) (*Resp, error) {
	resp, err := rpc(ctx, req)
	if err != nil {
		return nil, mapErr(err)
	}
	return resp, nil
}

// Stream opens a server stream of a client New returned, as in
// rpc.Stream(ctx, c.StreamLogs, req), and yields its messages until it ends,
// then closes it. A failure is yielded once, as the last value: a daemon that
// does not answer the opening call yields a NotRunningError.
func Stream[Req, Resp any, S interface {
	Receive() (*Resp, error)
	Close() error
}](ctx context.Context, open func(context.Context, *Req) (S, error), req *Req) iter.Seq2[*Resp, error] {
	return func(yield func(*Resp, error) bool) {
		stream, err := open(ctx, req)
		if err != nil {
			yield(nil, mapErr(err))
			return
		}
		defer func() { _ = stream.Close() }()

		for {
			msg, err := stream.Receive()
			if errors.Is(err, io.EOF) {
				return
			}
			if !yield(msg, err) || err != nil {
				return
			}
		}
	}
}

// IsNotRunning reports whether err signals an unreachable daemon: a missing
// socket or a refused connection.
func IsNotRunning(err error) bool {
	_, ok := errors.AsType[*NotRunningError](err)
	return ok
}

// NotRunningError is a transport failure to reach the daemon. Its message
// names the socket the client dialed.
type NotRunningError struct {
	Cause error
}

func (e *NotRunningError) Error() string {
	return fmt.Sprintf("a-novel daemon not reachable at %s: %v (run `a-novel core start` first)", paths.Socket(), e.Cause)
}
func (e *NotRunningError) Unwrap() error { return e.Cause }

// mapErr turns a transport failure into a typed error the CLI can report in
// its own words.
func mapErr(err error) error {
	// Connect errors carry a code, and a missing or refusing socket comes
	// through as Unavailable.
	if ce, ok := errors.AsType[*connect.Error](err); ok && ce.Code() == connect.CodeUnavailable {
		return &NotRunningError{Cause: err}
	}
	// Raw net.OpError (e.g., dial unix: connect: no such file or directory)
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return &NotRunningError{Cause: err}
	}
	return err
}
