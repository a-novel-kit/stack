package cli

import (
	"cmp"
	"io"
	"maps"
	"slices"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/workspace"
	"github.com/a-novel-kit/stack/cli/internal/workspace/workspacetest"
)

// TestUpdateRepo pins how sync moves an existing checkout: it fast-forwards the
// default branch, never switches branches, and never overwrites unstaged work.
func TestUpdateRepo(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		// The local clone checks out branch (when set), origin advances a.txt
		// (when advance), then dirty is written to the clone's working tree.
		branch  string
		advance bool
		dirty   map[string]string

		wantStatus string
		// wantFF: local master reaches origin/master; otherwise it stays put.
		wantFF    bool
		wantFiles map[string]string
	}{
		{
			name:       "OnDefaultBranch/CleanFastForwards",
			advance:    true,
			wantStatus: syncUpdated, wantFF: true,
			wantFiles: map[string]string{"a.txt": "a1\n"},
		},
		{
			// An unrelated unstaged change survives the fast-forward.
			name:    "OnDefaultBranch/UnrelatedDirtyPreserved",
			advance: true, dirty: map[string]string{"b.txt": "local-wip\n"},
			wantStatus: syncUpdated, wantFF: true,
			wantFiles: map[string]string{"a.txt": "a1\n", "b.txt": "local-wip\n"},
		},
		{
			// A conflicting unstaged change is never clobbered: sync skips and
			// leaves both HEAD and the working tree as they were.
			name:    "OnDefaultBranch/ConflictingDirtySkipped",
			advance: true, dirty: map[string]string{"a.txt": "my-wip\n"},
			wantStatus: syncSkipped,
			wantFiles:  map[string]string{"a.txt": "my-wip\n"},
		},
		{
			// Off the default branch, the master ref advances without leaving
			// the feature branch or touching the working tree.
			name:   "OffDefaultBranch/UpdatesRefOnly",
			branch: "feature", advance: true, dirty: map[string]string{"b.txt": "feature-wip\n"},
			wantStatus: syncUpdated, wantFF: true,
			wantFiles: map[string]string{"a.txt": "a0\n", "b.txt": "feature-wip\n"},
		},
		{name: "UpToDate", wantStatus: syncUpToDate, wantFF: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			local, seed := workspacetest.InitSync(t)
			if testCase.branch != "" {
				workspacetest.Git(t, local, "checkout", "--quiet", "-b", testCase.branch)
			}
			if testCase.advance {
				workspacetest.AdvanceOrigin(t, seed)
			}
			for name, content := range testCase.dirty {
				writeFixture(t, local, name, content)
			}
			masterBefore := workspacetest.Git(t, local, "rev-parse", "refs/heads/master")

			status, err := updateRepo(local, io.Discard)
			if err != nil || status != testCase.wantStatus {
				t.Fatalf("updateRepo = (%q, %v), want %q", status, err, testCase.wantStatus)
			}
			wantMaster := masterBefore
			if testCase.wantFF {
				wantMaster = workspacetest.Git(t, local, "rev-parse", "refs/remotes/origin/master")
			}
			if got := workspacetest.Git(t, local, "rev-parse", "refs/heads/master"); got != wantMaster {
				t.Errorf("local master = %s, want %s (fast-forward %v)", got, wantMaster, testCase.wantFF)
			}
			if got, want := workspacetest.Git(t, local, "symbolic-ref", "--short", "HEAD"), cmp.Or(testCase.branch, "master"); got != want {
				t.Errorf("HEAD = %q, want to stay on %q", got, want)
			}
			for name, want := range testCase.wantFiles {
				if got := readFixture(t, local, name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}

// TestSyncJobs pins that --allow and --ignore name a repo by its full or its
// bare name, and that --ignore wins.
func TestSyncJobs(t *testing.T) {
	t.Parallel()

	repos := []workspace.Repo{
		{Org: workspace.OrgAnovelKit, Name: "golib"},
		{Org: workspace.OrgAnovel, Name: "service-json-keys"},
	}
	testCases := []struct {
		name            string
		allow, ignore   []string
		want, wantSkips []string
	}{
		{name: "Success/NoFilter", want: []string{"a-novel-kit/golib", "a-novel/service-json-keys"}},
		{name: "Success/AllowFullName", allow: []string{"a-novel-kit/golib"}, want: []string{"a-novel-kit/golib"}},
		{name: "Success/AllowBareName", allow: []string{"golib"}, want: []string{"a-novel-kit/golib"}},
		{
			name: "Success/IgnoreBareName", ignore: []string{"service-json-keys"},
			want: []string{"a-novel-kit/golib"}, wantSkips: []string{"a-novel/service-json-keys"},
		},
		{
			name: "Success/IgnoreWinsOverAllow", allow: []string{"golib"}, ignore: []string{"a-novel-kit/golib"},
			wantSkips: []string{"a-novel-kit/golib"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			list, skipped := syncJobs(t.TempDir(), repos, normalizeFilter(testCase.allow), normalizeFilter(testCase.ignore))
			got := make([]string, len(list))
			for i, job := range list {
				got[i] = job.Name
			}
			if !slices.Equal(got, testCase.want) || !slices.Equal(skipped, testCase.wantSkips) {
				t.Errorf("syncJobs = (%v, skipped %v), want (%v, skipped %v)", got, skipped, testCase.want, testCase.wantSkips)
			}
		})
	}
}

// TestNormalizeFilter checks the --allow/--ignore values become one set of
// trimmed names, with comma-joined values split.
func TestNormalizeFilter(t *testing.T) {
	t.Parallel()

	got := normalizeFilter([]string{"golib, service-json-keys", "", "a-novel/service-profile"})
	want := map[string]bool{"golib": true, "service-json-keys": true, "a-novel/service-profile": true}
	if !maps.Equal(got, want) {
		t.Errorf("normalizeFilter = %v, want %v", got, want)
	}
}
