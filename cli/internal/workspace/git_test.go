package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
	"github.com/a-novel-kit/stack/cli/internal/workspace"
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
			name: "Success/FeatureBranch",
			setup: func(t *testing.T, local string) {
				t.Helper()

				workspacetest.Git(t, local, "checkout", "--quiet", "-b", "feature")
			},
			want: "on feature",
		},
		{
			name: "Success/DetachedHEAD",
			setup: func(t *testing.T, local string) {
				t.Helper()

				workspacetest.Git(t, local, "checkout", "--quiet", "--detach")
			},
			want: "detached HEAD",
		},
		{
			name: "Success/DirtyTree",
			setup: func(t *testing.T, local string) {
				t.Helper()

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
			require.Equal(t, testCase.want, workspace.OngoingWork(t.Context(), local))
		})
	}
}

func TestUnpushedCommits(t *testing.T) {
	t.Parallel()

	local, _ := workspacetest.InitSync(t)
	require.Equal(t, 0, workspace.UnpushedCommits(t.Context(), local), "fresh clone")

	workspacetest.Commit(t, local, "c.txt", "c0\n")
	require.Equal(t, 1, workspace.UnpushedCommits(t.Context(), local), "after one local commit")

	workspacetest.Git(t, local, "push", "--quiet")
	require.Equal(t, 0, workspace.UnpushedCommits(t.Context(), local), "after push")

	// With no upstream there is no remote to have lost the commit to.
	workspacetest.Git(t, local, "checkout", "--quiet", "-b", "no-upstream")
	workspacetest.Commit(t, local, "d.txt", "d0\n")
	require.Equal(t, 0, workspace.UnpushedCommits(t.Context(), local), "no upstream")
}

func TestToplevel(t *testing.T) {
	t.Parallel()

	root := workspacetest.InitPushed(t)
	sub := filepath.Join(root, "deep", "inside")
	nested := filepath.Join(root, "app", "service-x")
	for _, dir := range []string{sub, nested} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	workspacetest.Git(t, nested, "init", "--quiet", "--initial-branch=master")

	testCases := []struct {
		name    string
		dir     string
		want    string
		wantErr bool
	}{
		{name: "Success/Subdirectory", dir: sub, want: root},
		// An independent repo nested inside (a pulled checkout under app/) is
		// its own toplevel: this scopes `publish` to the service you cd into.
		{name: "Success/NestedRepo", dir: nested, want: nested},
		{name: "Error/OutsideARepo", dir: t.TempDir(), wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := workspace.Toplevel(t.Context(), testCase.dir)
			if testCase.wantErr {
				require.Error(t, err)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			// Resolve symlinks on both sides: macOS TempDirs live under /private.
			gotResolved, err := filepath.EvalSymlinks(got)
			require.NoError(t, err)
			wantResolved, err := filepath.EvalSymlinks(testCase.want)
			require.NoError(t, err)
			require.Equal(t, wantResolved, gotResolved)
		})
	}
}
