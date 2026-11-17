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

// TestParseURL pins the remote forms that name a GitHub repository. The parser
// backs the org guard of detect, setup's stack check and the sandbox, so a
// foreign host or a local path must name none.
func TestParseURL(t *testing.T) {
	t.Parallel()

	stack := workspace.Repo{Org: "a-novel-kit", Name: "stack"}

	testCases := []struct {
		name   string
		url    string
		expect workspace.Repo
		ok     bool
	}{
		{name: "Success/SSH", url: "git@github.com:a-novel-kit/stack.git", expect: stack, ok: true},
		{name: "Success/SSHScheme", url: "ssh://git@github.com/a-novel-kit/stack.git", expect: stack, ok: true},
		{name: "Success/HTTPS", url: "https://github.com/a-novel-kit/stack", expect: stack, ok: true},
		{name: "Success/HTTPSGitSuffix", url: "https://github.com/a-novel-kit/stack.git\n", expect: stack, ok: true},
		{
			name: "Success/Fork", url: "git@github.com:someone/stack.git",
			expect: workspace.Repo{Org: "someone", Name: "stack"}, ok: true,
		},
		{name: "Error/ForeignHost", url: "https://gitlab.com/a-novel/foo.git"},
		{name: "Error/LookalikeHost", url: "https://github.com.example.org/a-novel/foo.git"},
		{name: "Error/LocalPath", url: "/srv/mirrors/a-novel-kit/stack.git"},
		{name: "Error/ExtraSegment", url: "https://github.com/a-novel-kit/stack/tree"},
		{name: "Error/NoName", url: "git@github.com:a-novel-kit"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			repo, ok := workspace.ParseURL(testCase.url)
			require.Equal(t, testCase.ok, ok)
			require.Equal(t, testCase.expect, repo)
		})
	}
}

// TestRootRepo pins that the workspace root counts as the stack whatever its
// origin's host, so a stack cloned from a local mirror keeps its unsaved work
// guarded, while a GitHub origin names the repository, a fork included.
func TestRootRepo(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		origin string // "" for a directory that is no checkout
		expect workspace.Repo
		ok     bool
	}{
		{name: "Success/GitHub", origin: "git@github.com:someone/stack.git", expect: workspace.Repo{Org: "someone", Name: "stack"}, ok: true},
		{name: "Success/LocalMirror", origin: "/srv/mirrors/stack.git", expect: workspace.StackRepo, ok: true},
		{name: "Error/NotACheckout"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if testCase.origin != "" {
				root = workspacetest.Init(t)
				workspacetest.Git(t, root, "remote", "add", "origin", testCase.origin)
			}

			repo, ok := workspace.RootRepo(t.Context(), root)
			require.Equal(t, testCase.ok, ok)
			require.Equal(t, testCase.expect, repo)
		})
	}
}
