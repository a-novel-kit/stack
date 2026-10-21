package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// StreamLogs server-streams log lines for a target:
//   - by default, one snapshot of current.log from start to end
//   - with --follow, that snapshot plus every new line until the process
//     terminates or the client disconnects
//   - with --run-id, an archived run, never followed
//
// Snapshot lines come first in file order, and followed lines after. Each
// StreamLogsResponse carries its original timestamp and stream tag.
func (s *Server) StreamLogs(ctx context.Context, req *anovelv1.StreamLogsRequest, stream anovelv1connect.CoreServiceStreamLogsServerStream) error {
	tid := req.GetTargetId()
	// An infra log ID, "<stack>/<service>/infra/<name>", streams an infra
	// container's stdout through this same RPC. Those containers belong to
	// podman, so the branch reads `podman logs -f <cid>`.
	if stack, service, name, ok := parseInfraLogID(tid); ok {
		st, found := s.runner.InfraStatesOf(ctx, stack)[service+"/"+name]
		if !found || st.ContainerID == "" {
			return connect.Errorf(connect.CodeFailedPrecondition,
				"no container for %s/%s/%s — has infra-start been run?", stack, service, name)
		}
		return streamPodmanLogs(ctx, st.ContainerID, req.GetFollow(), stream)
	}
	tgt, _, err := s.findTarget(tid)
	if err != nil {
		return err
	}
	// Pick the file to stream.
	path := s.logs.CurrentPath(tgt.Stack, tgt.Service, tgt.Name)
	if req.GetRunId() != "" {
		path = s.logs.RunPath(tgt.Stack, tgt.Service, tgt.Name, req.GetRunId())
	}
	follow := req.GetFollow() && req.GetRunId() == ""

	// Under --follow, subscribe before reading the file, so no line written in
	// between is lost.
	var sub <-chan logs.Line
	if follow {
		if ch, unsub, ok := s.logs.Subscribe(tid); ok {
			defer unsub()
			sub = ch
		}
	}

	if err := streamFileToClient(ctx, path, stream, req.GetStream()); err != nil {
		// A missing file means no run yet, so fall through to the follow
		// path when one was requested.
		if !follow {
			return connect.Errorf(connect.CodeNotFound,
				"read log %s: %v", path, err).WithCause(err)
		}
	}
	return forwardLines(ctx, sub, stream.Send, req.GetStream())
}

// forwardLines sends every line of sub until the subscription closes or the
// client disconnects. A nil sub sends nothing.
func forwardLines(ctx context.Context, sub <-chan logs.Line, send func(*anovelv1.StreamLogsResponse) error, filter anovelv1.LogStream) error {
	if sub == nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case ln, ok := <-sub:
			if !ok {
				return nil
			}
			if err := sendLogLine(send, filter, ln); err != nil {
				return err
			}
		}
	}
}

// ListRuns returns the timestamps of archived runs for the target,
// newest first.
func (s *Server) ListRuns(_ context.Context, req *anovelv1.ListRunsRequest) (*anovelv1.ListRunsResponse, error) {
	tgt, _, err := s.findTarget(req.GetTargetId())
	if err != nil {
		return nil, err
	}
	return &anovelv1.ListRunsResponse{
		RunIds: s.logs.ListRuns(tgt.Stack, tgt.Service, tgt.Name),
	}, nil
}

// streamFileToClient reads a JSON-lines log file and sends every line matching
// the optional stream filter to the client, on both the snapshot and the
// archived-run path.
func streamFileToClient(ctx context.Context, path string, stream anovelv1connect.CoreServiceStreamLogsServerStream, filter anovelv1.LogStream) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return streamLines(ctx, f, stream.Send, filter, path)
}

// streamLines forwards one JSON-lines log stream to send.
//
// It reads a line at a time rather than through a json.Decoder: a decoder that
// fails part-way cannot be resumed, so the only recovery left is to stop, and
// stopping hands the viewer a short log that looks whole. The writer flushes
// per line, but a process killed mid-write leaves a partial final one, and that
// is the common case.
func streamLines(ctx context.Context, r io.Reader, send func(*anovelv1.StreamLogsResponse) error, filter anovelv1.LogStream, path string) error {
	reader := bufio.NewReader(r)
	lineNo := 0
	for {
		// A client that went away is not a failure, so the snapshot just stops.
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		lineNo++
		raw, readErr := reader.ReadString('\n')
		if err := emitLogLine(send, filter, raw, lineNo); err != nil {
			return err
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("read log %s: %w", path, readErr)
		}
	}
}

// emitLogLine forwards one raw record, or a marker naming it when it does not
// parse. The marker is sent whatever the stream filter is: a record that cannot
// be decoded has no stream to match on, and a viewer that filters it away is
// back to a log that looks complete. Its timestamp is the read time, since the
// record carries none, and clients render it in receive order.
func emitLogLine(send func(*anovelv1.StreamLogsResponse) error, filter anovelv1.LogStream, raw string, lineNo int) error {
	trimmed := strings.TrimRight(raw, "\r\n")
	if trimmed == "" {
		return nil
	}
	var ln logs.Line
	if err := json.Unmarshal([]byte(trimmed), &ln); err != nil {
		marker := &anovelv1.StreamLogsResponse{
			Ts:     timestamppb.Now(),
			Stream: anovelv1.LogStream_LOG_STREAM_STDERR,
			Line:   fmt.Sprintf("a-novel: log line %d is unreadable and was skipped (%v)", lineNo, err),
		}
		return send(marker)
	}
	return sendLogLine(send, filter, ln)
}

// sendLogLine forwards ln unless filter selects the other stream.
func sendLogLine(send func(*anovelv1.StreamLogsResponse) error, filter anovelv1.LogStream, ln logs.Line) error {
	ps := lineStreamToProto(ln.Stream)
	if filter != anovelv1.LogStream_LOG_STREAM_UNSPECIFIED && ps != filter {
		return nil
	}
	return send(&anovelv1.StreamLogsResponse{Ts: timestamppb.New(ln.Ts), Stream: ps, Line: ln.Line})
}

func lineStreamToProto(s logs.Stream) anovelv1.LogStream {
	switch s {
	case logs.StreamStdout:
		return anovelv1.LogStream_LOG_STREAM_STDOUT
	case logs.StreamStderr:
		return anovelv1.LogStream_LOG_STREAM_STDERR
	default:
		return anovelv1.LogStream_LOG_STREAM_UNSPECIFIED
	}
}

// parseInfraLogID recognizes the "<stack>/<service>/infra/<name>" form
// StreamLogs uses to address an infra container's log stream. It returns
// (stack, service, name, true) on a match and ("", "", "", false) otherwise.
//
// A target ID has three segments, so the four-segment infra form never matches
// one.
func parseInfraLogID(id string) (string, string, string, bool) {
	parts := strings.Split(id, "/")
	if len(parts) != 4 || parts[2] != "infra" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[3], true
}

// streamPodmanLogs forwards `podman logs [-f] <cid>` output to a client log
// stream. The container's stdout and stderr merge into one pipe, reported as
// stdout, since service images write to stdout anyway.
//
// Each line is stamped at read time, which is accurate enough for the log
// viewer and skips parsing podman's optional --timestamps prefix.
func streamPodmanLogs(ctx context.Context, cid string, follow bool, stream anovelv1connect.CoreServiceStreamLogsServerStream) error {
	args := []string{"logs", cid}
	if follow {
		args = []string{"logs", "-f", cid}
	}
	cmd := exec.CommandContext(ctx, "podman", args...)
	pipeR, pipeW := io.Pipe()
	// An early return leaves podman's output unread. Closing the reader fails
	// the blocked copy, so Wait returns and the goroutine below ends.
	defer func() { _ = pipeR.Close() }()
	cmd.Stdout, cmd.Stderr = pipeW, pipeW
	if err := cmd.Start(); err != nil {
		return connect.Errorf(connect.CodeInternal,
			"start podman logs %s: %v", cid, err).WithCause(err)
	}
	// Close the writer when the command exits so the scanner sees EOF.
	go func() {
		_ = cmd.Wait()
		_ = pipeW.Close()
	}()
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()
	for scanner := lineScanner(pipeR); scanner.Scan() && ctx.Err() == nil; {
		ln := &anovelv1.StreamLogsResponse{Ts: timestamppb.Now(), Stream: anovelv1.LogStream_LOG_STREAM_STDOUT, Line: scanner.Text()}
		if err := stream.Send(ln); err != nil {
			return err
		}
	}
	return nil
}

// lineScanner scans r line by line. Command output carries long lines, such as
// panics and JSON dumps, so the per-token cap goes up to 1 MiB to keep the
// scanner from bisecting one.
func lineScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	return sc
}
