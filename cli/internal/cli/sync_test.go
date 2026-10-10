package cli

import (
	"io"
	"testing"
)

// TestUpdateRepo pins how sync moves an existing checkout: it fast-forwards the
// default branch, never switches branches, and never overwrites unstaged work.
func TestUpdateRepo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// setup prepares the local clone; seed pushes to its origin.
		setup      func(t *testing.T, local, seed string)
		wantStatus string
		// check inspects the clone, given its HEAD before the update.
		check func(t *testing.T, local, headBefore string)
	}{
		{
			name:       "OnDefaultBranch/CleanFastForwards",
			setup:      func(t *testing.T, _, seed string) { advanceOrigin(t, seed) },
			wantStatus: syncUpdated,
			check: func(t *testing.T, local, _ string) {
				if got, want := gitOut(t, local, "rev-parse", "refs/heads/master"), gitOut(t, local, "rev-parse", "refs/remotes/origin/master"); got != want {
					t.Errorf("local master %s not fast-forwarded to origin %s", got, want)
				}
				if got := readFixture(t, local, "a.txt"); got != "a1\n" {
					t.Errorf("a.txt = %q, want the pulled content", got)
				}
			},
		},
		{
			// An unrelated unstaged change survives the fast-forward.
			name: "OnDefaultBranch/UnrelatedDirtyPreserved",
			setup: func(t *testing.T, local, seed string) {
				advanceOrigin(t, seed)
				writeFixture(t, local, "b.txt", "local-wip\n")
			},
			wantStatus: syncUpdated,
			check: func(t *testing.T, local, _ string) {
				if got := readFixture(t, local, "b.txt"); got != "local-wip\n" {
					t.Errorf("unstaged b.txt = %q, want it preserved", got)
				}
				if got := readFixture(t, local, "a.txt"); got != "a1\n" {
					t.Errorf("a.txt = %q, want the pulled content", got)
				}
			},
		},
		{
			// A conflicting unstaged change is never clobbered: sync skips and
			// leaves both HEAD and the working tree as they were.
			name: "OnDefaultBranch/ConflictingDirtySkipped",
			setup: func(t *testing.T, local, seed string) {
				advanceOrigin(t, seed)
				writeFixture(t, local, "a.txt", "my-wip\n")
			},
			wantStatus: syncSkipped,
			check: func(t *testing.T, local, headBefore string) {
				if got := gitOut(t, local, "rev-parse", "HEAD"); got != headBefore {
					t.Errorf("HEAD moved to %s despite the conflict; want %s", got, headBefore)
				}
				if got := readFixture(t, local, "a.txt"); got != "my-wip\n" {
					t.Errorf("unstaged a.txt = %q, want it left untouched", got)
				}
			},
		},
		{
			// Off the default branch, the master ref advances without leaving
			// the feature branch or touching the working tree.
			name: "OffDefaultBranch/UpdatesRefOnly",
			setup: func(t *testing.T, local, seed string) {
				mustGit(t, local, "checkout", "--quiet", "-b", "feature")
				advanceOrigin(t, seed)
				writeFixture(t, local, "b.txt", "feature-wip\n")
			},
			wantStatus: syncUpdated,
			check: func(t *testing.T, local, _ string) {
				if got := gitOut(t, local, "symbolic-ref", "--short", "HEAD"); got != "feature" {
					t.Errorf("HEAD = %q, want to stay on 'feature'", got)
				}
				if master, origin := gitOut(t, local, "rev-parse", "refs/heads/master"), gitOut(t, local, "rev-parse", "refs/remotes/origin/master"); master != origin {
					t.Errorf("master %s not fast-forwarded to origin %s", master, origin)
				}
				if got := readFixture(t, local, "b.txt"); got != "feature-wip\n" {
					t.Errorf("unstaged b.txt = %q, want it preserved", got)
				}
				if got := readFixture(t, local, "a.txt"); got != "a0\n" {
					t.Errorf("a.txt = %q, want the feature-branch content untouched", got)
				}
			},
		},
		{
			name:       "UpToDate",
			setup:      func(*testing.T, string, string) {},
			wantStatus: syncUpToDate,
			check:      func(*testing.T, string, string) {},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			local, seed := initSyncRepo(t)
			c.setup(t, local, seed)
			headBefore := gitOut(t, local, "rev-parse", "HEAD")

			status, err := updateRepo(local, io.Discard)
			if err != nil {
				t.Fatalf("updateRepo: %v", err)
			}
			if status != c.wantStatus {
				t.Fatalf("status = %q, want %q", status, c.wantStatus)
			}
			c.check(t, local, headBefore)
		})
	}
}
