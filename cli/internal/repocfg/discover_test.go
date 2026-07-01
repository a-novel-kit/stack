package repocfg

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeTree writes rel→content under root, creating parent dirs.
func writeTree(root, rel, content string) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		panic(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		panic(err)
	}
}

// TestDiscover covers the main.yaml-based discovery: required checks are the
// always set plus every main.yaml job, minus the report-* and master-only
// exclusions; CodeQL languages come from the file signals + the always list.
func TestDiscover(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTree(root, ".github/workflows/main.yaml", `
name: main
jobs:
  test-go:
    runs-on: ubuntu-latest
  lint-go:
    runs-on: ubuntu-latest
  report-codecov:
    runs-on: ubuntu-latest
  publish-docs:
    if: "github.ref == 'refs/heads/master' && success()"
    runs-on: ubuntu-latest
`)
	writeTree(root, "go.mod", "module x\n")
	writeTree(root, "package.json", "{}")

	cc, err := LoadChecks()
	if err != nil {
		t.Fatalf("LoadChecks: %v", err)
	}
	d, err := Discover(root, cc)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// always (GitGuardian + merge-gate) + the non-excluded main.yaml jobs.
	got := contextsOf(d.Checks)
	want := []string{"GitGuardian Security Checks", "lint-go", "merge-gate", "test-go"}
	if !slices.Equal(got, want) {
		t.Errorf("required checks = %v, want %v", got, want)
	}
	// merge-gate is posted by the [Agent] App, not Actions — the ruleset must
	// require it against that integration id.
	for _, c := range d.Checks {
		if c.Context == "merge-gate" && c.IntegrationID != 3549379 {
			t.Errorf("merge-gate integration id = %d, want 3549379 ([Agent] App)", c.IntegrationID)
		}
	}
	// report-* and master-only jobs must NOT be required.
	for _, ex := range []string{"report-codecov", "publish-docs"} {
		if slices.Contains(got, ex) {
			t.Errorf("excluded job %q leaked into required checks: %v", ex, got)
		}
	}
	// CodeQL languages from file signals (go.mod, package.json) + always-on actions.
	for _, lang := range []string{"go", "javascript-typescript", "actions"} {
		if !slices.Contains(d.CodeQLLangs, lang) {
			t.Errorf("missing CodeQL lang %q; got %v", lang, d.CodeQLLangs)
		}
	}
}

// TestDiscover_NoMainYaml: a repo without a main.yaml (e.g. docs/meta) yields
// just the always-required checks.
func TestDiscover_NoMainYaml(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cc, err := LoadChecks()
	if err != nil {
		t.Fatalf("LoadChecks: %v", err)
	}
	d, err := Discover(root, cc)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got, want := contextsOf(d.Checks), []string{"GitGuardian Security Checks", "merge-gate"}; !slices.Equal(got, want) {
		t.Errorf("checks = %v, want %v", got, want)
	}
}
