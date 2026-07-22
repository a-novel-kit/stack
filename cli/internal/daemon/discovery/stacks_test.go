package discovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

// TestDiscoverStacksSkipsVanishedScratch pins the asymmetry that keeps the
// daemon startable: a scratch stack's default home is a directory the OS
// reclaims, so its files can disappear while its $A_NOVEL_STACKS entry lives on
// in a shell config. Failing discovery over that would take down the whole
// daemon — including the workspace the operator is actually using — every time
// a temp sweep ran.
func TestDiscoverStacksSkipsVanishedScratch(t *testing.T) {
	t.Parallel()

	present := t.TempDir()
	gone := filepath.Join(t.TempDir(), "swept")

	got, err := DiscoverStacks([]stacks.Stack{
		{Name: "default", Path: present, IsDefault: true},
		{Name: "swept", Path: gone},
	})
	if err != nil {
		t.Fatalf("a vanished scratch stack should not fail discovery: %v", err)
	}
	if len(got) != 1 || got[0].Name != "default" {
		t.Fatalf("discovered %v, want only the default stack", got)
	}
}

// TestDiscoverStacksFailsOnVanishedDefault is the other half: the default stack
// IS the workspace, so its absence is a real misconfiguration and must be loud.
func TestDiscoverStacksFailsOnVanishedDefault(t *testing.T) {
	t.Parallel()

	gone := filepath.Join(t.TempDir(), "swept")

	if _, err := DiscoverStacks([]stacks.Stack{
		{Name: "default", Path: gone, IsDefault: true},
	}); err == nil {
		t.Fatal("a missing default stack should fail discovery")
	}
}

// TestDiscoverStacksSkipsNonDirectory covers the same skip for a path that
// exists but is a file — equally unusable, equally not worth a dead daemon.
func TestDiscoverStacksSkipsNonDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	got, err := DiscoverStacks([]stacks.Stack{
		{Name: "default", Path: dir, IsDefault: true},
		{Name: "bogus", Path: file},
	})
	if err != nil {
		t.Fatalf("a non-directory scratch stack should not fail discovery: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("discovered %d stacks, want 1", len(got))
	}
}
