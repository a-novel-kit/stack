package runner

import (
	"context"
	"testing"
	"time"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Tests for the kill path's two reporting duties: a stop that fails reaches the caller, and a
// terminated instance stops the goroutines watching it.
//
// A container-mode instance runs a watch loop and a log stream on one context. The watcher polls
// `podman inspect` every 2s and returns once it sees the container exited, so a container that
// outlives its instance keeps both goroutines going for the life of the daemon.

func TestMarkTerminatedClosesTheInstanceContext(t *testing.T) {
	r := &Runner{instances: map[string]*Instance{}}

	ctx, cancel := context.WithCancel(context.Background())
	const id = "default/svc/rest"

	r.instances[id] = &Instance{
		ID:      id,
		Service: "svc",
		Stack:   "default",
		Phase:   anovelv1.Phase_PHASE_RUNNING,
		cancel:  cancel,
	}

	r.markTerminated(id, anovelv1.ExitReason_EXIT_REASON_KILLED, "")

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("markTerminated left the instance context open; the watch and log goroutines run on it")
	}
}

func TestMarkTerminatedHandlesAnInstanceWithNoContext(t *testing.T) {
	r := &Runner{instances: map[string]*Instance{}}

	const id = "default/svc/migrations"

	// A go-exec instance that never started carries no cancel.
	r.instances[id] = &Instance{ID: id, Phase: anovelv1.Phase_PHASE_PENDING}

	r.markTerminated(id, anovelv1.ExitReason_EXIT_REASON_KILLED, "")

	if got := r.instances[id].Phase; got != anovelv1.Phase_PHASE_TERMINATED {
		t.Errorf("phase: got %v, want TERMINATED", got)
	}
}

func TestKillContainerReportsAFailedStop(t *testing.T) {
	r := &Runner{instances: map[string]*Instance{}}

	const id = "default/svc/rest"

	r.instances[id] = &Instance{
		ID:          id,
		Service:     "svc",
		Stack:       "default",
		Phase:       anovelv1.Phase_PHASE_STOPPING,
		Mode:        ModeContainer,
		ContainerID: "a-novel-test-container-that-does-not-exist",
	}

	// The stop fails whether or not podman is installed: the binary is missing, or it rejects the
	// unknown container.
	err := r.killContainer(t.Context(), id, time.Second)
	if err == nil {
		t.Fatal("killContainer: got nil for a container that cannot be stopped")
	}

	// The instance keeps its STOPPING phase. Marking it terminated would report a container gone
	// while it still holds its name and host ports, and the next start collides with it.
	if got := r.instances[id].Phase; got != anovelv1.Phase_PHASE_STOPPING {
		t.Errorf("phase after a failed stop: got %v, want STOPPING", got)
	}
}
