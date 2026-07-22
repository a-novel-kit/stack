package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

// TestReadDaemonLogFrom pins that a failed start quotes its own output and not
// the output of the start before it. A daemon that fails, is fixed, and fails
// again for a new reason would otherwise be diagnosed from the stale message.
func TestReadDaemonLogFrom(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.log")
	first := "Error: previous failure\n"
	if err := os.WriteFile(path, []byte(first), 0o600); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	got := readDaemonLogFrom(path, int64(len(first)))
	if got != "" {
		t.Errorf("read from end of file = %q, want empty", got)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if _, err := f.WriteString("Error: this attempt\n"); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	_ = f.Close()

	got = readDaemonLogFrom(path, int64(len(first)))
	if got != "Error: this attempt" {
		t.Errorf("read from offset = %q, want only this attempt's line", got)
	}
}

// TestReadDaemonLogFromCaps stops a crash-looping daemon from burying its own
// error under its retries.
func TestReadDaemonLogFromCaps(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", daemonLogTailLimit*3)), 0o600); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	if got := len(readDaemonLogFrom(path, 0)); got > daemonLogTailLimit {
		t.Errorf("read %d bytes, want at most %d", got, daemonLogTailLimit)
	}
}

// TestReadDaemonLogFromMissing covers a log that was never created: no output,
// no panic — the readiness error still has to be reportable.
func TestReadDaemonLogFromMissing(t *testing.T) {
	t.Parallel()

	if got := readDaemonLogFrom(filepath.Join(t.TempDir(), "absent.log"), 0); got != "" {
		t.Errorf("missing log read = %q, want empty", got)
	}
}

func TestDaemonFailureDetail(t *testing.T) {
	// Not parallel: t.Setenv redirects the process-wide XDG_STATE_HOME that
	// paths.DaemonLog() resolves through.
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	t.Run("unopenable log falls back to the manual hint", func(t *testing.T) {
		got := daemonFailureDetail(errors.New("permission denied"), 0)
		if !strings.Contains(got, "--foreground") {
			t.Errorf("detail = %q, want the --foreground fallback", got)
		}
	})

	t.Run("silent daemon says so", func(t *testing.T) {
		got := daemonFailureDetail(nil, 0)
		if !strings.Contains(got, "wrote nothing") {
			t.Errorf("detail = %q, want it to report an empty log", got)
		}
		if !strings.Contains(got, paths.DaemonLog()) {
			t.Errorf("detail = %q, want the log path so it can be inspected", got)
		}
	})

	t.Run("a daemon that explained itself is quoted", func(t *testing.T) {
		if err := os.MkdirAll(filepath.Dir(paths.DaemonLog()), 0o700); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		msg := "Error: discover stacks: stack swept at /tmp/gone: no such file or directory"
		if err := os.WriteFile(paths.DaemonLog(), []byte(msg+"\n"), 0o600); err != nil {
			t.Fatalf("fixture: %v", err)
		}

		got := daemonFailureDetail(nil, 0)
		if !strings.Contains(got, msg) {
			t.Errorf("detail = %q, want it to quote the daemon's error", got)
		}
	})
}

// TestOpenDaemonLogAppends pins the append: a start that fails is often one of
// several, and truncating would discard the attempts that explain the last one.
func TestOpenDaemonLogAppends(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	first, err := openDaemonLog()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := first.WriteString("one\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = first.Close()

	second, err := openDaemonLog()
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := second.WriteString("two\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = second.Close()

	body, err := os.ReadFile(paths.DaemonLog())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "one\ntwo\n" {
		t.Errorf("log = %q, want both attempts", body)
	}
}
