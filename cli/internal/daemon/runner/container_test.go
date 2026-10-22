package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestStartContainer covers a kill that arrives during `compose up`. The kill
// finds no container yet and leaves the target STOPPING. When compose returns,
// the start stops the container it brought up and fails, so the killed target
// never serves.
func TestStartContainer(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// A stand-in podman: compose up waits for the test's release file, ps
	// names one container, and stop records its arguments.
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
compose) : > %[1]s/composing; while [ ! -e %[1]s/release ]; do sleep 0.05; done ;;
ps) echo cid123 ;;
stop) echo "$@" >> %[1]s/stops ;;
*) exit 1 ;;
esac
`, dir)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "podman"), []byte(script), 0o700); err != nil { //nolint:gosec // an executable fixture
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	r, _, tgt := newRunnerForLaunch()

	started := make(chan error, 1)
	go func() {
		_, err := r.launch(t.Context(), tgt, anovelv1.Mode_MODE_CONTAINER)
		started <- err
	}()
	waitForFile(t, filepath.Join(dir, "composing"))

	if err := r.Kill(t.Context(), tgt.ID(), 0); err != nil {
		t.Fatalf("Kill during compose up: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-started:
		if !errors.Is(err, errKilledWhileStarting) {
			t.Fatalf("launch: got %v, want %v", err, errKilledWhileStarting)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("launch still running 10s after compose up returned")
	}
	if inst, _ := r.Instance(tgt.ID()); inst.Phase != anovelv1.Phase_PHASE_TERMINATED || inst.ExitReason != anovelv1.ExitReason_EXIT_REASON_KILLED {
		t.Errorf("record = (%v, %v), want TERMINATED with KILLED", inst.Phase, inst.ExitReason)
	}
	if stops, _ := os.ReadFile(filepath.Join(dir, "stops")); !strings.Contains(string(stops), "cid123") {
		t.Errorf("podman stop calls = %q, want a stop of cid123", stops)
	}
}

// waitForFile polls until path exists, failing the test after 10s.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(path); err == nil {
			return
		}
	}
	t.Fatalf("%s never appeared", path)
}
