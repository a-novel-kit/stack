package daemon

import (
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

// TestRegisteredAndDiscovered pins that the server hears only about the stacks
// discovery kept. The raw registration list would let ListStacks advertise a
// stack every other RPC then rejects as unregistered, and the two views must
// agree.
func TestRegisteredAndDiscovered(t *testing.T) {
	t.Parallel()

	configured := []stacks.Stack{
		{Name: "default", Path: "/w", IsDefault: true},
		{Name: "swept", Path: "/tmp/gone"},
		{Name: "alive", Path: "/tmp/here"},
	}
	disc := []*discovery.Stack{{Name: "default"}, {Name: "alive"}}

	got := registeredAndDiscovered(configured, disc)

	if len(got) != 2 {
		t.Fatalf("kept %d stacks, want 2", len(got))
	}
	// Order is preserved, so the first entry is still the default one.
	if got[0].Name != "default" || !got[0].IsDefault {
		t.Errorf("first kept stack = %+v, want the default", got[0])
	}
	if got[1].Name != "alive" {
		t.Errorf("second kept stack = %q, want \"alive\"", got[1].Name)
	}
}

// TestRegisteredAndDiscoveredEmpty covers discovery keeping nothing: the result
// comes back empty, never falling back to the full registration list.
func TestRegisteredAndDiscoveredEmpty(t *testing.T) {
	t.Parallel()

	got := registeredAndDiscovered([]stacks.Stack{{Name: "swept"}}, nil)

	if len(got) != 0 {
		t.Fatalf("kept %v, want none", got)
	}
}
