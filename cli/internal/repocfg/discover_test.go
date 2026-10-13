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
		name        string
		mainYAML    string
		want        []string
		wantSecrets []string
	}{
		{name: "Success/NoMainYaml", want: []string{"check-dependencies", "epic-freeze", "merge-gate", "review-gate"}},
		{
			name: "Success/MainYaml",
			mainYAML: `
name: main
jobs:
  test-go:
    runs-on: ubuntu-latest
    steps:
      - uses: codecov/codecov-action@v7
        with:
          token: ${{ secrets.CODECOV_TOKEN }}
      - run: echo "${{ secrets.CODECOV_TOKEN }} ${{secrets.GITHUB_TOKEN}}"
  lint-go:
    runs-on: ubuntu-latest
  report-codecov:
    runs-on: ubuntu-latest
  publish-docs:
    if: "github.ref == 'refs/heads/master' && success()"
    runs-on: ubuntu-latest
`,
			want:        []string{"check-dependencies", "epic-freeze", "lint-go", "merge-gate", "review-gate", "test-go"},
			wantSecrets: []string{"CODECOV_TOKEN", "GITHUB_TOKEN"},
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
				// merge-gate, epic-freeze and review-gate are required against the injected per-org [Agent] app id.
				if (c.Context == "merge-gate" || c.Context == "epic-freeze" || c.Context == "review-gate") && c.IntegrationID != 4242 {
					t.Errorf("%s integration id = %d, want the injected 4242", c.Context, c.IntegrationID)
				}
				// check-dependencies is a workflow job, so GitHub Actions posts it.
				if c.Context == "check-dependencies" && c.IntegrationID != cc.Integrations[integrationActions] {
					t.Errorf("check-dependencies integration id = %d, want Actions", c.IntegrationID)
				}
			}
			if !slices.Equal(got, testCase.want) {
				t.Errorf("required checks = %v, want %v", got, testCase.want)
			}
			if !slices.Equal(d.Secrets, testCase.wantSecrets) {
				t.Errorf("secrets = %v, want %v", d.Secrets, testCase.wantSecrets)
			}
		})
	}
}
