package server

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestStreamFileToClient pins the snapshot a tail sends: the last lines that
// pass the stream filter, in file order.
func TestStreamFileToClient(t *testing.T) {
	t.Parallel()

	record := func(stream, line string) string {
		return `{"ts":"2026-07-22T10:00:00Z","stream":"` + stream + `","line":"` + line + `"}` + "\n"
	}
	path := filepath.Join(t.TempDir(), "current.log")
	content := record("stdout", "one") + record("stderr", "two") + record("stdout", "three") + record("stdout", "four")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("seed log: %v", err)
	}

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
			if err := streamFileToClient(t.Context(), path, send, testCase.filter, testCase.tail); err != nil {
				t.Fatalf("streamFileToClient: %v", err)
			}
			if !slices.Equal(got, testCase.want) {
				t.Errorf("sent %q, want %q", got, testCase.want)
			}
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
			if got := strings.Join(podmanLogsArgs("cid", testCase.follow, testCase.tail), " "); got != testCase.want {
				t.Errorf("podmanLogsArgs = %q, want %q", got, testCase.want)
			}
		})
	}
}
