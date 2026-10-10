package repocfg

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestDiscover covers the main.yaml-based discovery: required checks are the
// always set plus every main.yaml job, minus the report-* and master-only
// exclusions. A repo without a main.yaml (e.g. docs/meta) yields just the
// always set.
func TestDiscover(t *testing.T) {
	t.Parallel()

	cc, err := LoadChecks()
	if err != nil {
		t.Fatalf("LoadChecks: %v", err)
	}
	// The [Agent] app id is per-org, injected before discovery. A sentinel id
	// proves merge-gate resolves to the injected value, not a global constant.
	cc.ResolveBotIntegrations(&OrgProfile{Bots: map[string]int64{"agent": 4242}})

	testCases := []struct {
		name     string
		mainYAML string
		want     []string
	}{
		{name: "Success/NoMainYaml", want: []string{"epic-freeze", "merge-gate"}},
		{
			name: "Success/MainYaml",
			mainYAML: `
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
`,
			want: []string{"epic-freeze", "lint-go", "merge-gate", "test-go"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if testCase.mainYAML != "" {
				workflows := filepath.Join(root, ".github", "workflows")
				if err := os.MkdirAll(workflows, 0o750); err != nil {
					panic(err)
				}
				if err := os.WriteFile(filepath.Join(workflows, "main.yaml"), []byte(testCase.mainYAML), 0o600); err != nil {
					panic(err)
				}
			}
			d, err := Discover(root, cc)
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			got := make([]string, len(d.Checks))
			for i, c := range d.Checks {
				got[i] = c.Context
				// merge-gate + epic-freeze are required against the injected per-org [Agent] app id.
				if (c.Context == "merge-gate" || c.Context == "epic-freeze") && c.IntegrationID != 4242 {
					t.Errorf("%s integration id = %d, want the injected 4242", c.Context, c.IntegrationID)
				}
			}
			if !slices.Equal(got, testCase.want) {
				t.Errorf("required checks = %v, want %v", got, testCase.want)
			}
		})
	}
}
