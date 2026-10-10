package cli

import (
	"cmp"
	"io"
	"testing"
)

// TestUpdateRepo pins how sync moves an existing checkout: it fast-forwards the
// default branch, never switches branches, and never overwrites unstaged work.
func TestUpdateRepo(t *testing.T) {
	t.Parallel()

	cases := []struct {
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

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			local, seed := initSyncRepo(t)
			if c.branch != "" {
				mustGit(t, local, "checkout", "--quiet", "-b", c.branch)
			}
			if c.advance {
				advanceOrigin(t, seed)
			}
			for name, content := range c.dirty {
				writeFixture(t, local, name, content)
			}
			masterBefore := gitOut(t, local, "rev-parse", "refs/heads/master")

			status, err := updateRepo(local, io.Discard)
			if err != nil || status != c.wantStatus {
				t.Fatalf("updateRepo = (%q, %v), want %q", status, err, c.wantStatus)
			}
			wantMaster := masterBefore
			if c.wantFF {
				wantMaster = gitOut(t, local, "rev-parse", "refs/remotes/origin/master")
			}
			if got := gitOut(t, local, "rev-parse", "refs/heads/master"); got != wantMaster {
				t.Errorf("local master = %s, want %s (fast-forward %v)", got, wantMaster, c.wantFF)
			}
			if got, want := gitOut(t, local, "symbolic-ref", "--short", "HEAD"), cmp.Or(c.branch, branchMaster); got != want {
				t.Errorf("HEAD = %q, want to stay on %q", got, want)
			}
			for name, want := range c.wantFiles {
				if got := readFixture(t, local, name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}
