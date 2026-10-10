package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOngoingWork covers the git-state guard: a clean default-branch checkout is
// safe (empty reason), while a feature branch, a detached HEAD, or a dirty tree
// each yield a skip reason so the sweep never reconciles from in-progress state.
func TestOngoingWork(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, local string)
		want  string
	}{
		{name: "Success/CleanDefaultBranch", setup: func(*testing.T, string) {}},
		{
			name:  "Success/FeatureBranch",
			setup: func(t *testing.T, local string) { mustGit(t, local, "checkout", "--quiet", "-b", "feature") },
			want:  "on feature",
		},
		{
			name:  "Success/DetachedHEAD",
			setup: func(t *testing.T, local string) { mustGit(t, local, "checkout", "--quiet", "--detach") },
			want:  "detached HEAD",
		},
		{
			name:  "Success/DirtyTree",
			setup: func(t *testing.T, local string) { writeFixture(t, local, "a.txt", "work in progress\n") },
			want:  "uncommitted changes",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			local, _ := initSyncRepo(t)
			tc.setup(t, local)
			if got := ongoingWork(local); got != tc.want {
				t.Errorf("ongoingWork = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUnpushedCommits(t *testing.T) {
	t.Parallel()

	local, _ := initSyncRepo(t)
	expect := func(when string, want int) {
		t.Helper()
		if n := unpushedCommits(local); n != want {
			t.Fatalf("%s: %d unpushed commits, want %d", when, n, want)
		}
	}

	expect("fresh clone", 0)
	commitFixture(t, local, "c.txt", "c0\n")
	expect("after one local commit", 1)
	mustGit(t, local, "push", "--quiet")
	expect("after push", 0)
	// With no upstream there is no remote to have lost the commit to.
	mustGit(t, local, "checkout", "--quiet", "-b", "no-upstream")
	commitFixture(t, local, "d.txt", "d0\n")
	expect("no upstream", 0)
}

func TestGitToplevel(t *testing.T) {
	t.Parallel()

	root := initPublishRepo(t)
	sub := filepath.Join(root, "deep", "inside")
	nested := filepath.Join(root, "app", "service-x")
	for _, dir := range []string{sub, nested} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	mustGit(t, nested, "init", "--quiet", "--initial-branch=master")

	for _, tc := range []struct{ name, dir, want string }{
		{"Success/Subdirectory", sub, root},
		// An independent repo nested inside (a pulled checkout under app/) is
		// its own toplevel: this scopes `publish` to the service you cd into.
		{"Success/NestedRepo", nested, nested},
		{"Error/OutsideARepo", t.TempDir(), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := gitToplevel(tc.dir)
			// Resolve symlinks on both sides — macOS TempDirs live under /private.
			gotResolved, _ := filepath.EvalSymlinks(got)
			wantResolved, _ := filepath.EvalSymlinks(tc.want)
			if (err != nil) != (tc.want == "") || gotResolved != wantResolved {
				t.Errorf("gitToplevel = (%q, %v), want %q", got, err, tc.want)
			}
		})
	}
}
