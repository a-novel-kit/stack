package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

// TestReadDaemonLogFrom pins that a failed start quotes only its own output: a
// daemon that fails, is fixed, and fails again is diagnosed from the new
// message, and a crash-looping one cannot bury its error under its retries.
func TestReadDaemonLogFrom(t *testing.T) {
	t.Parallel()

	const first = "Error: previous failure\n"
	for _, tc := range []struct {
		name    string
		content string // "" leaves the log absent
		offset  int64
		want    string
	}{
		{name: "Success/FromOffset", content: first + "Error: this attempt\n", offset: int64(len(first)), want: "Error: this attempt"},
		{name: "Success/AtEnd", content: first, offset: int64(len(first))},
		// A log that was never created: the readiness error is still reportable.
		{name: "Success/Missing"},
		{name: "Success/Capped", content: strings.Repeat("x", daemonLogTailLimit*3), want: strings.Repeat("x", daemonLogTailLimit)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tc.content != "" {
				writeFixture(t, dir, "daemon.log", tc.content)
			}
			if got := readDaemonLogFrom(filepath.Join(dir, "daemon.log"), tc.offset); got != tc.want {
				t.Errorf("readDaemonLogFrom = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDaemonFailureDetail(t *testing.T) {
	// Not parallel: t.Setenv redirects the process-wide XDG_STATE_HOME that
	// paths.DaemonLog() resolves through.
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	t.Run("Success/UnopenableLogFallsBackToHint", func(t *testing.T) {
		got := daemonFailureDetail(errors.New("permission denied"), 0)
		if !strings.Contains(got, "--foreground") {
			t.Errorf("detail = %q, want the --foreground fallback", got)
		}
	})

	t.Run("Success/SilentDaemon", func(t *testing.T) {
		got := daemonFailureDetail(nil, 0)
		if !strings.Contains(got, "wrote nothing") {
			t.Errorf("detail = %q, want it to report an empty log", got)
		}
		if !strings.Contains(got, paths.DaemonLog()) {
			t.Errorf("detail = %q, want the log path so it can be inspected", got)
		}
	})

	t.Run("Success/QuotesTheDaemon", func(t *testing.T) {
		msg := "Error: discover stacks: stack swept at /tmp/gone: no such file or directory"
		writeFixture(t, filepath.Dir(paths.DaemonLog()), filepath.Base(paths.DaemonLog()), msg+"\n")

		got := daemonFailureDetail(nil, 0)
		if !strings.Contains(got, msg) {
			t.Errorf("detail = %q, want it to quote the daemon's error", got)
		}
	})
}

// TestOpenDaemonLogAppends pins the append: a start that fails is often one of
// several, and every attempt stays in the log to explain the last one.
func TestOpenDaemonLogAppends(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	for _, attempt := range []string{"one\n", "two\n"} {
		f, err := openDaemonLog()
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if _, err := f.WriteString(attempt); err != nil {
			t.Fatalf("write: %v", err)
		}
		_ = f.Close()
	}

	body, err := os.ReadFile(paths.DaemonLog())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "one\ntwo\n" {
		t.Errorf("log = %q, want both attempts", body)
	}
}
