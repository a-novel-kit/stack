package repocfg

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestDiscover exercises the subdir-detection fix end to end on a stack-shaped
// layout: node + actions signals at the root, the Go module and proto config
// in cli/. The root-only detection used to find only the root signals.
func TestDiscover(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			panic(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			panic(err)
		}
	}
	write("package.json", "{}")
	write(".github/workflows/main.yaml", "name: main\n")
	write("cli/go.mod", "module x\n")
	write("cli/buf.yaml", "version: v2\n")

	cc, err := LoadChecks()
	if err != nil {
		t.Fatalf("LoadChecks: %v", err)
	}
	d, err := Discover(root, cc)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	got := contextsOf(d.Checks)

	// Go + proto (under cli/) and node + always (at root) are all detected, and
	// the Go test context is the renamed test-go, never the retired bare test.
	for _, want := range []string{
		"lint-go", "generated-go", "test-go", "lint-proto", "lint-node",
		"GitGuardian Security Checks",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("missing discovered check %q; got %v", want, got)
		}
	}
	if slices.Contains(got, "test") {
		t.Errorf("discovered the retired bare `test` check; got %v", got)
	}

	// One CodeQL language per detected signal.
	for _, want := range []string{"go", "javascript-typescript", "actions"} {
		if !slices.Contains(d.CodeQLLangs, want) {
			t.Errorf("missing CodeQL lang %q; got %v", want, d.CodeQLLangs)
		}
	}
}
