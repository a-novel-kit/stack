package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
	"github.com/a-novel-kit/stack/cli/internal/workspace/workspacetest"
)

// TestOngoingWork covers the git-state guard: a clean default-branch checkout is
// safe (empty reason), while a feature branch, a detached HEAD, or a dirty tree
// each yield a skip reason so the sweep never reconciles from in-progress state.
func TestOngoingWork(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		setup func(t *testing.T, local string)
		want  string
	}{
		{name: "Success/CleanDefaultBranch", setup: func(*testing.T, string) {}},
		{
			name:  "Success/FeatureBranch",
			setup: func(t *testing.T, local string) { workspacetest.Git(t, local, "checkout", "--quiet", "-b", "feature") },
			want:  "on feature",
		},
		{
			name:  "Success/DetachedHEAD",
			setup: func(t *testing.T, local string) { workspacetest.Git(t, local, "checkout", "--quiet", "--detach") },
			want:  "detached HEAD",
		},
		{
			name: "Success/DirtyTree",
			setup: func(t *testing.T, local string) {
				fsutiltest.Write(t, local, map[string]string{"a.txt": "work in progress\n"})
			},
			want: "uncommitted changes",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			local, _ := workspacetest.InitSync(t)
			testCase.setup(t, local)
			if got := OngoingWork(local); got != testCase.want {
				t.Errorf("OngoingWork = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestUnpushedCommits(t *testing.T) {
	t.Parallel()

	local, _ := workspacetest.InitSync(t)
	expect := func(when string, want int) {
		t.Helper()
		if n := UnpushedCommits(local); n != want {
			t.Fatalf("%s: %d unpushed commits, want %d", when, n, want)
		}
	}

	expect("fresh clone", 0)
	workspacetest.Commit(t, local, "c.txt", "c0\n")
	expect("after one local commit", 1)
	workspacetest.Git(t, local, "push", "--quiet")
	expect("after push", 0)
	// With no upstream there is no remote to have lost the commit to.
	workspacetest.Git(t, local, "checkout", "--quiet", "-b", "no-upstream")
	workspacetest.Commit(t, local, "d.txt", "d0\n")
	expect("no upstream", 0)
}

func TestGitToplevel(t *testing.T) {
	t.Parallel()

	root := workspacetest.InitPushed(t)
	sub := filepath.Join(root, "deep", "inside")
	nested := filepath.Join(root, "app", "service-x")
	for _, dir := range []string{sub, nested} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	workspacetest.Git(t, nested, "init", "--quiet", "--initial-branch=master")

	testCases := []struct{ name, dir, want string }{
		{"Success/Subdirectory", sub, root},
		// An independent repo nested inside (a pulled checkout under app/) is
		// its own toplevel: this scopes `publish` to the service you cd into.
		{"Success/NestedRepo", nested, nested},
		{"Error/OutsideARepo", t.TempDir(), ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got, err := Toplevel(testCase.dir)
			// Resolve symlinks on both sides — macOS TempDirs live under /private.
			gotResolved, _ := filepath.EvalSymlinks(got)
			wantResolved, _ := filepath.EvalSymlinks(testCase.want)
			if (err != nil) != (testCase.want == "") || gotResolved != wantResolved {
				t.Errorf("Toplevel = (%q, %v), want %q", got, err, testCase.want)
			}
		})
	}
}
