package cli

import (
	"iter"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestPrintLogs pins the client-side --since filter of `run logs`.
func TestPrintLogs(t *testing.T) {
	t.Parallel()

	now := time.Now()
	lines := []*anovelv1.StreamLogsResponse{
		{Ts: timestamppb.New(now.Add(-time.Hour)), Line: "old"},
		{Ts: timestamppb.New(now.Add(-time.Minute)), Line: "first"},
		{Ts: timestamppb.New(now.Add(-time.Second)), Line: "second"},
		{Ts: timestamppb.New(now), Line: "third"},
	}
	var stream iter.Seq2[*anovelv1.StreamLogsResponse, error] = func(yield func(*anovelv1.StreamLogsResponse, error) bool) {
		for _, ln := range lines {
			if !yield(ln, nil) {
				return
			}
		}
	}

	testCases := []struct {
		name  string
		since time.Duration
		want  []string
	}{
		{name: "Success/Everything", want: []string{"old", "first", "second", "third"}},
		{name: "Success/Since", since: 10 * time.Minute, want: []string{"first", "second", "third"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			if err := printLogs(&out, stream, testCase.since); err != nil {
				t.Fatalf("printLogs: %v", err)
			}
			printed := strings.Split(strings.TrimSpace(out.String()), "\n")
			got := make([]string, len(printed))
			for i, line := range printed {
				got[i] = line[strings.LastIndexByte(line, ' ')+1:]
			}
			if strings.Join(got, ",") != strings.Join(testCase.want, ",") {
				t.Errorf("printed %q, want %q", got, testCase.want)
			}
		})
	}
}
