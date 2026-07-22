package main

import "testing"

// TestParseBuildArgs_Keep guards the --keep flag wiring. The reuse behavior it
// drives needs podman, so it is covered end-to-end instead.
func TestParseBuildArgs_Keep(t *testing.T) {
	t.Parallel()

	on, err := parseBuildArgs([]string{"--keep"})
	if err != nil {
		t.Fatalf("parseBuildArgs(--keep): %v", err)
	}
	if !on.keep {
		t.Error("--keep must set opts.keep")
	}

	off, err := parseBuildArgs(nil)
	if err != nil {
		t.Fatalf("parseBuildArgs(nil): %v", err)
	}
	if off.keep {
		t.Error("opts.keep must default to false")
	}
}
