package repocfg

import (
	"slices"
	"strings"
	"testing"
)

func TestRenderCodeQL(t *testing.T) {
	t.Parallel()

	t.Run("named suite emits queries + filter in config", func(t *testing.T) {
		t.Parallel()
		out, err := RenderCodeQL([]string{"go", "actions"}, "security-and-quality", "master")
		if err != nil {
			t.Fatalf("RenderCodeQL: %v", err)
		}
		for _, want := range []string{
			`language: ["go", "actions"]`,
			`branches: ["master"]`,
			"config: |",
			"queries:",
			"- uses: security-and-quality",
			"query-filters:",
			"- exclude:",
			"id: actions/unpinned-tag",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in:\n%s", want, out)
			}
		}
		// The bare placeholder must be fully substituted.
		if strings.Contains(out, "__") {
			t.Errorf("unsubstituted placeholder remains:\n%s", out)
		}
	})

	t.Run("default suite omits queries but keeps the filter", func(t *testing.T) {
		t.Parallel()
		out, err := RenderCodeQL([]string{"go"}, "", "main")
		if err != nil {
			t.Fatalf("RenderCodeQL: %v", err)
		}
		if strings.Contains(out, "queries:") {
			t.Errorf("default suite must omit queries:\n%s", out)
		}
		for _, want := range []string{"config: |", "query-filters:", "id: actions/unpinned-tag"} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in:\n%s", want, out)
			}
		}
	})
}

// TestIsManaged pins the managed-namespace boundary the union reconcile relies
// on: catalog + retired are managed; docker-derived and manual names are not.
func TestIsManaged(t *testing.T) {
	t.Parallel()
	cc := mustLoadChecks(t)

	cases := []struct {
		name string
		ctx  string
		want bool
	}{
		{"go test (catalog)", "test-go", true},
		{"lint-go (catalog)", "lint-go", true},
		{"lint-proto (catalog)", "lint-proto", true},
		{"build-js (feature catalog)", "build-js", true},
		{"gitguardian (always catalog)", "GitGuardian Security Checks", true},
		{"codecov (catalog)", "codecov/patch", true},
		{"bare test (overloaded, not retired)", "test", false},
		{"hand-named docker job (quirk)", "build-job-init", false},
		{"derived docker check", "build-rest", false},
		{"manual addition", "some-manual-check", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cc.IsManaged(tc.ctx); got != tc.want {
				t.Errorf("IsManaged(%q) = %v, want %v", tc.ctx, got, tc.want)
			}
		})
	}
}

// TestIsManagedRetired covers the retired mechanism in isolation (the live map
// retires nothing right now): a retired name is managed, so a normal update
// drops a stale live check by that name.
func TestIsManagedRetired(t *testing.T) {
	t.Parallel()
	cc := &ChecksConfig{Retired: []string{"old-lint"}}
	if !cc.IsManaged("old-lint") {
		t.Error("a retired context must be managed")
	}
	if cc.IsManaged("still-manual") {
		t.Error("a non-retired, non-catalog context must be unmanaged")
	}
}

// TestReconcileMasterChecks covers the add / preserve / drop / reset behaviour
// of `update`'s required-check reconciliation against the real checks.yaml.
func TestReconcileMasterChecks(t *testing.T) {
	t.Parallel()
	cc := mustLoadChecks(t)

	cases := []struct {
		name       string
		discovered []CheckRef
		live       []CheckRef
		force      bool
		want       []string
	}{
		{
			name:       "Create/NoLive",
			discovered: refs("lint-go", "test-go"),
			want:       []string{"lint-go", "test-go"},
		},
		{
			name:       "AddNewlyDeclaredManaged",
			discovered: refs("lint-go", "test-go"),
			live:       refs("lint-go"), // test-go just declared, not yet live
			want:       []string{"lint-go", "test-go"},
		},
		{
			name:       "PreserveUnmanaged",
			discovered: refs("lint-go", "test-go"),
			live:       refs("lint-go", "build-job-init"), // hand-named gate kept
			want:       []string{"build-job-init", "lint-go", "test-go"},
		},
		{
			name:       "DropRemovedManaged",
			discovered: refs("lint-go", "test-go"), // repo dropped its proto
			live:       refs("lint-go", "test-go", "lint-proto"),
			want:       []string{"lint-go", "test-go"},
		},
		{
			name:       "Force/DropsUnmanagedToo",
			discovered: refs("test-go"),
			live:       refs("test", "build-job-init", "manual-x"),
			force:      true,
			want:       []string{"test-go"},
		},
		{
			name:       "Dedup",
			discovered: refs("lint-go", "test-go"),
			live:       refs("lint-go"),
			want:       []string{"lint-go", "test-go"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := contextsOf(reconcileMasterChecks(tc.discovered, tc.live, cc, tc.force))
			if !slices.Equal(got, tc.want) {
				t.Errorf("reconcileMasterChecks = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestReconcileMasterChecksRetired drives the retired path with a synthetic map
// (the live map retires nothing): a renamed managed check's OLD name, present
// live but no longer discovered, is dropped.
func TestReconcileMasterChecksRetired(t *testing.T) {
	t.Parallel()
	cc := &ChecksConfig{
		Languages: map[string]LangRule{"go": {Checks: []CheckDef{{Context: "test-go"}}}},
		Retired:   []string{"test"},
	}
	got := contextsOf(reconcileMasterChecks(refs("test-go"), refs("test"), cc, false))
	if want := []string{"test-go"}; !slices.Equal(got, want) {
		t.Errorf("retired drop: got %v, want %v", got, want)
	}
}

// TestResolveMasterChecks ties the reconcile to the RepoTarget method the plan
// and the summary both call, including the --full (ForceChecks) reset.
func TestResolveMasterChecks(t *testing.T) {
	t.Parallel()
	cc := mustLoadChecks(t)

	target := &RepoTarget{
		Checks:           cc,
		Discovered:       &Discovered{Checks: refs("test-go", "lint-go")},
		LiveMasterChecks: refs("build-job-init"),
	}

	// Reconcile preserves the unmanaged hand-named `build-job-init` on top of
	// the discovered set.
	if got, want := contextsOf(target.ResolveMasterChecks()),
		[]string{"build-job-init", "lint-go", "test-go"}; !slices.Equal(got, want) {
		t.Errorf("ResolveMasterChecks = %v, want %v", got, want)
	}

	// --full resets to the discovered set alone.
	target.ForceChecks = true
	if got, want := contextsOf(target.ResolveMasterChecks()),
		[]string{"lint-go", "test-go"}; !slices.Equal(got, want) {
		t.Errorf("ResolveMasterChecks(force) = %v, want %v", got, want)
	}
}

func mustLoadChecks(t *testing.T) *ChecksConfig {
	t.Helper()
	cc, err := LoadChecks()
	if err != nil {
		panic(err)
	}
	return cc
}

// refs builds a CheckRef slice from bare context names.
func refs(contexts ...string) []CheckRef {
	out := make([]CheckRef, len(contexts))
	for i, c := range contexts {
		out[i] = CheckRef{Context: c}
	}
	return out
}

// contextsOf extracts the context names from a CheckRef slice, preserving order.
func contextsOf(checks []CheckRef) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.Context
	}
	return out
}
