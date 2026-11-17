package server

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// record renders one JSON-lines log record.
func record(stream, line string) string {
	return `{"ts":"2026-07-22T10:00:00Z","stream":"` + stream + `","line":"` + line + `"}` + "\n"
}

// TestStreamFileToClient pins the snapshot a tail sends: the last lines that
// pass the stream filter, in file order.
func TestStreamFileToClient(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fsutiltest.Write(t, dir, map[string]string{
		"current.log": record("stdout", "one") + record("stderr", "two") + record("stdout", "three") + record("stdout", "four"),
	})
	path := filepath.Join(dir, "current.log")

	testCases := []struct {
		name   string
		filter anovelv1.LogStream
		tail   int
		want   []string
	}{
		{name: "Success/NoTail", want: []string{"one", "two", "three", "four"}},
		{name: "Success/Tail", tail: 2, want: []string{"three", "four"}},
		{name: "Success/TailAfterFilter", filter: anovelv1.LogStream_LOG_STREAM_STDERR, tail: 2, want: []string{"two"}},
		{name: "Success/TailAboveLength", tail: 10, want: []string{"one", "two", "three", "four"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var got []string
			send := func(ln *anovelv1.StreamLogsResponse) error {
				got = append(got, ln.GetLine())
				return nil
			}
			require.NoError(t, streamFileToClient(t.Context(), path, send, testCase.filter, testCase.tail))
			require.Equal(t, testCase.want, got)
		})
	}
}

// errAfterFirst yields one whole record and then fails, standing in for a log
// file on failing storage.
type errAfterFirst struct{ done bool }

var errRead = errors.New("input/output error")

func (r *errAfterFirst) Read(p []byte) (int, error) {
	if r.done {
		return 0, errRead
	}
	r.done = true
	return copy(p, record("stdout", "first")), nil
}

// TestStreamLines pins whether a log snapshot reached the end of the file, for
// an operator who has no other way to check it.
func TestStreamLines(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		in        io.Reader
		filter    anovelv1.LogStream
		cancelled bool
		want      []string // a substring of each line sent, in order
		wantErr   error
	}{
		{
			// A process killed mid-write leaves exactly this: whole records, then a
			// partial one. Everything after the tear has to still arrive, and the
			// tear itself has to be visible.
			name: "Success/ReportsACorruptRecord",
			in:   strings.NewReader(record("stdout", "first") + `{"ts":"2026-07-22T10:00:01Z","stream":"stdo` + "\n" + record("stdout", "third")),
			want: []string{"first", "line 2 is unreadable", "third"},
		},
		{
			// A record that does not decode has no stream to match on. Filtering it
			// away would put the viewer back in front of a log that looks complete.
			name:   "Success/MarkerSurvivesAStreamFilter",
			in:     strings.NewReader(record("stderr", "err") + "{ broken\n"),
			filter: anovelv1.LogStream_LOG_STREAM_STDOUT,
			want:   []string{"unreadable"},
		},
		{
			// EOF is the end of the log; anything else is the reader giving up
			// part-way through one, and the two need different answers.
			name: "Error/ReadFailure", in: &errAfterFirst{}, want: []string{"first"}, wantErr: errRead,
		},
		{name: "Success/EndsCleanlyAtEOF", in: strings.NewReader(record("stdout", "only")), want: []string{"only"}},
		{
			// ReadString returns the trailing bytes together with io.EOF, so an
			// unterminated last record is data, not the end of the file.
			name: "Success/ReadsAFinalRecordWithNoNewline",
			in:   strings.NewReader(strings.TrimSuffix(record("stdout", "last"), "\n")),
			want: []string{"last"},
		},
		// A client that went away is not a failure.
		{name: "Success/StopsOnACancelledContext", in: strings.NewReader("{ broken\n"), cancelled: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if testCase.cancelled {
				cancel()
			}
			var got []string
			err := streamLines(ctx, testCase.in, func(ln *anovelv1.StreamLogsResponse) error {
				got = append(got, ln.GetLine())
				return nil
			}, testCase.filter, "current.log")
			require.ErrorIs(t, err, testCase.wantErr)
			require.Len(t, got, len(testCase.want))
			for i, want := range testCase.want {
				require.Contains(t, got[i], want)
			}
		})
	}
}

func TestParseInfraLogID(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                  string
		in                    string
		stack, service, infra string
		ok                    bool
	}{
		{name: "Success", in: "default/svc-x/infra/postgres-x", stack: "default", service: "svc-x", infra: "postgres-x", ok: true},
		{name: "Error/TooFewSegments", in: "default/svc/infra"},
		{name: "Error/TooManySegments", in: "default/svc/infra/x/y"},
		// Missing the "infra" sentinel: it addresses a regular target.
		{name: "Error/NoInfraSentinel", in: "default/svc/rest/x"},
		{name: "Error/Empty", in: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			stack, service, infra, ok := parseInfraLogID(testCase.in)
			require.Equal(t, testCase.stack, stack)
			require.Equal(t, testCase.service, service)
			require.Equal(t, testCase.infra, infra)
			require.Equal(t, testCase.ok, ok)
		})
	}
}

// TestPodmanLogsArgs pins that an infra log honors --tail and --follow.
func TestPodmanLogsArgs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		follow bool
		tail   int32
		want   string
	}{
		{name: "Success/Snapshot", want: "logs cid"},
		{name: "Success/Follow", follow: true, want: "logs -f cid"},
		{name: "Success/TailAndFollow", follow: true, tail: 100, want: "logs -f --tail 100 cid"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, strings.Join(podmanLogsArgs("cid", testCase.follow, testCase.tail), " "))
		})
	}
}

// TestLineScanner pins that a line over 1 MiB never stops a scan. It comes out
// in 1 MiB pieces and the lines after it still come through, so an Exec child
// writing it never blocks on a pipe nobody reads.
func TestLineScanner(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 2*maxLineBytes+maxLineBytes/2)
	piece, rest := strings.Repeat("x", maxLineBytes), strings.Repeat("x", maxLineBytes/2)

	testCases := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "Success/ShortLines", input: "one\ntwo\n", want: []string{"one", "two"}},
		{name: "Success/LongLine", input: "before\n" + long + "\nafter\n", want: []string{"before", piece, piece, rest, "after"}},
		{name: "Success/LongLineAtEOF", input: long, want: []string{piece, piece, rest}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var got []string
			sc := lineScanner(strings.NewReader(testCase.input))
			for sc.Scan() {
				got = append(got, sc.Text())
			}
			require.NoError(t, sc.Err())
			// The lines run to MiBs, so a mismatch reports their lengths.
			require.True(t, slices.Equal(testCase.want, got), "line lengths = %v, want %v", lengths(got), lengths(testCase.want))
		})
	}
}

// lengths returns the length of each line.
func lengths(lines []string) []int {
	out := make([]int, len(lines))
	for i, line := range lines {
		out[i] = len(line)
	}
	return out
}
