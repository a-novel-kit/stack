package cli

import (
	"errors"
	"iter"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestExecResult pins how `a-novel run exec` propagates a child's exit status,
// so a failing exec stops a `&&` chain.
func TestExecResult(t *testing.T) {
	code := func(v int32) *int32 { return &v }

	testCases := []struct {
		name     string
		exitCode *int32
		// wantExit is the code carried by an *ExitError; 0 means "expect no error".
		wantExit int
		// wantErr covers the other failure shape: the daemon never reported at all.
		wantErr bool
	}{
		{"success", code(0), 0, false},
		{"failure", code(3), 3, false},
		// A signal-killed child reports -1, and the code stays non-zero so it
		// never reads as success.
		{"signalled", code(-1), -1, false},
		// The case the terminal message exists for: no report is not success.
		{"no terminal message", nil, 0, true},
	}

	for _, testCase := range testCases {
		err := execResult(testCase.exitCode)
		var exitErr *ExitError
		gotExit := 0
		if errors.As(err, &exitErr) {
			gotExit = exitErr.Code
		}
		if gotExit != testCase.wantExit || (err != nil && gotExit == 0) != testCase.wantErr {
			t.Errorf("execResult(%s) = %v, want exit code %d (non-exit error %v)", testCase.name, err, testCase.wantExit, testCase.wantErr)
		}
	}
}

// TestParseEntityID pins the ID shapes kill, restart, logs and start accept. A
// shorthand resolves against the first $A_NOVEL_STACKS entry, not a stack
// literally named "default".
func TestParseEntityID(t *testing.T) {
	// Not parallel: t.Setenv redirects the stack registry.
	t.Setenv("A_NOVEL_STACKS", "main:/srv/main,fork:/srv/fork")

	testCases := []struct {
		arg, stack string
		want       entityRef
	}{
		{arg: "svc/rest", want: entityRef{Stack: "main", Service: "svc", Target: "rest", ID: "main/svc/rest"}},
		{arg: "svc/rest", stack: "fork", want: entityRef{Stack: "fork", Service: "svc", Target: "rest", ID: "fork/svc/rest"}},
		{arg: "fork/svc/rest", want: entityRef{Stack: "fork", Service: "svc", Target: "rest", ID: "fork/svc/rest"}},
		{arg: "svc/infra/pg", want: entityRef{IsInfra: true, Stack: "main", Service: "svc", Infra: "pg", ID: "main/svc/infra/pg"}},
		{arg: "fork/svc/infra/pg", want: entityRef{IsInfra: true, Stack: "fork", Service: "svc", Infra: "pg", ID: "fork/svc/infra/pg"}},
		// "infra" anywhere but second-to-last is an ordinary target name.
		{arg: "svc/infra", want: entityRef{Stack: "main", Service: "svc", Target: "infra", ID: "main/svc/infra"}},
	}
	for _, testCase := range testCases {
		if got := parseEntityID(testCase.arg, testCase.stack); got != testCase.want {
			t.Errorf("parseEntityID(%q, %q) = %+v, want %+v", testCase.arg, testCase.stack, got, testCase.want)
		}
	}
}

// TestPrintLogs pins the client-side --since and --tail filters of `run logs`.
func TestPrintLogs(t *testing.T) {
	t.Parallel()

	now := time.Now()
	lines := []*anovelv1.LogLine{
		{Ts: timestamppb.New(now.Add(-time.Hour)), Line: "old"},
		{Ts: timestamppb.New(now.Add(-time.Minute)), Line: "first"},
		{Ts: timestamppb.New(now.Add(-time.Second)), Line: "second"},
		{Ts: timestamppb.New(now), Line: "third"},
	}
	var stream iter.Seq2[*anovelv1.LogLine, error] = func(yield func(*anovelv1.LogLine, error) bool) {
		for _, ln := range lines {
			if !yield(ln, nil) {
				return
			}
		}
	}

	testCases := []struct {
		name  string
		since time.Duration
		tail  int
		want  []string
	}{
		{name: "Success/Everything", want: []string{"old", "first", "second", "third"}},
		{name: "Success/Since", since: 10 * time.Minute, want: []string{"first", "second", "third"}},
		{name: "Success/Tail", tail: 2, want: []string{"second", "third"}},
		{name: "Success/SinceAndTail", since: 10 * time.Minute, tail: 5, want: []string{"first", "second", "third"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			if err := printLogs(&out, stream, testCase.since, testCase.tail); err != nil {
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
