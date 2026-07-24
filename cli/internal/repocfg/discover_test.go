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
	// The [Agent] app id is per-org, injected before discovery. Use a sentinel id
	// so the assertion below proves merge-gate resolves to the injected value, not
	// a global constant.
	cc.ResolveBotIntegrations(&OrgProfile{Bots: map[string]int64{"agent": 4242}})
	d, err := Discover(root, cc)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	// always (epic-freeze + merge-gate) + the non-excluded main.yaml jobs.
	got := contextsOf(d.Checks)
	want := []string{"epic-freeze", "lint-go", "merge-gate", "test-go"}
	if !slices.Equal(got, want) {
		t.Errorf("required checks = %v, want %v", got, want)
	}
	// merge-gate + epic-freeze must be required against the injected per-org [Agent] app id.
	for _, c := range d.Checks {
		if (c.Context == "merge-gate" || c.Context == "epic-freeze") && c.IntegrationID != 4242 {
			t.Errorf("%s integration id = %d, want the injected 4242", c.Context, c.IntegrationID)
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
	if got, want := contextsOf(d.Checks), []string{"epic-freeze", "merge-gate"}; !slices.Equal(got, want) {
		t.Errorf("checks = %v, want %v", got, want)
	}
}
