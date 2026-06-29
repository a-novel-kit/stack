package repocfg

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeTree writes rel→content under root, creating parent dirs. Shared by the
// discovery tests.
func writeTree(root, rel, content string) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		panic(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		panic(err)
	}
}

// TestDiscover covers the strong-semantic path (class service): the fixed
// checks.yaml rules, with the subdir Go module + proto under cli/ detected by the
// recursive walk and the Go test context the renamed test-go.
func TestDiscover(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTree(root, "package.json", "{}")
	writeTree(root, "cli/go.mod", "module x\n")
	writeTree(root, "cli/buf.yaml", "version: v2\n")

	cc, err := LoadChecks()
	if err != nil {
		t.Fatalf("LoadChecks: %v", err)
	}
	d, err := Discover(root, ClassService, cc)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	got := contextsOf(d.Checks)
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
	// actions CodeQL is always-on (no folder detection); go/js from signals.
	for _, want := range []string{"go", "javascript-typescript", "actions"} {
		if !slices.Contains(d.CodeQLLangs, want) {
			t.Errorf("missing CodeQL lang %q; got %v", want, d.CodeQLLangs)
		}
	}
}

// TestDiscoverLibrary covers the freeform path (class library): file/script-based
// detection with lane-suffixed, path-encoded names. The layout exercises a root
// Go module (no //go:generate → no generated-go), a nested cli/ Go module (with
// //go:generate → generated-go-cli) + proto, and a pnpm workspace root whose
// scripts yield js-lane checks.
func TestDiscoverLibrary(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTree(root, "go.mod", "module x\n")
	writeTree(root, "main.go", "package main\n") // no //go:generate
	writeTree(root, "pnpm-workspace.yaml", "packages: []\n")
	writeTree(root, "package.json", `{"scripts":{"lint":"x","test":"y"}}`)
	writeTree(root, "cli/go.mod", "module x/cli\n")
	writeTree(root, "cli/gen.go", "package cli\n\n//go:generate echo hi\n")
	writeTree(root, "cli/buf.yaml", "version: v2\n")

	cc, err := LoadChecks()
	if err != nil {
		t.Fatalf("LoadChecks: %v", err)
	}
	d, err := Discover(root, ClassLibrary, cc)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	got := contextsOf(d.Checks)
	want := []string{
		"GitGuardian Security Checks",
		"generated-go-cli", // cli/ module has //go:generate
		"lint-go",          // root module
		"lint-go-cli",      // nested module → path segment
		"lint-js",          // workspace-root lint script
		"lint-proto-cli",   // buf.yaml in cli/
		"test-go",
		"test-go-cli",
		"test-js", // workspace-root test script
	}
	if !slices.Equal(got, want) {
		t.Errorf("library checks =\n  %v\nwant\n  %v", got, want)
	}
	// generated-go (root) must NOT appear — the root module has no //go:generate.
	if slices.Contains(got, "generated-go") {
		t.Errorf("root module has no //go:generate; generated-go must be absent: %v", got)
	}
	for _, lang := range []string{"go", "javascript-typescript", "actions"} {
		if !slices.Contains(d.CodeQLLangs, lang) {
			t.Errorf("missing CodeQL lang %q; got %v", lang, d.CodeQLLangs)
		}
	}
}
