package cli

import (
	"cmp"
	"io"
	"slices"
	"testing"
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

			local, seed := initSyncRepo(t)
			if testCase.branch != "" {
				mustGit(t, local, "checkout", "--quiet", "-b", testCase.branch)
			}
			if testCase.advance {
				advanceOrigin(t, seed)
			}
			for name, content := range testCase.dirty {
				writeFixture(t, local, name, content)
			}
			masterBefore := gitOut(t, local, "rev-parse", "refs/heads/master")

			status, err := updateRepo(local, io.Discard)
			if err != nil || status != testCase.wantStatus {
				t.Fatalf("updateRepo = (%q, %v), want %q", status, err, testCase.wantStatus)
			}
			wantMaster := masterBefore
			if testCase.wantFF {
				wantMaster = gitOut(t, local, "rev-parse", "refs/remotes/origin/master")
			}
			if got := gitOut(t, local, "rev-parse", "refs/heads/master"); got != wantMaster {
				t.Errorf("local master = %s, want %s (fast-forward %v)", got, wantMaster, testCase.wantFF)
			}
			if got, want := gitOut(t, local, "symbolic-ref", "--short", "HEAD"), cmp.Or(testCase.branch, branchMaster); got != want {
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

	repos := []repoEntry{
		{Org: orgAnovelKit, Name: "golib"},
		{Org: orgAnovel, Name: "service-json-keys"},
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
