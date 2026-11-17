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

// TestCheckouts builds a fake workspace (a git repo at the root with an origin
// remote, a whitelist, and clone markers under app/ and kit/) and checks the
// sweep surfaces exactly the pulled repos: the stack itself, from the root and
// only once, plus every whitelisted checkout present on disk.
func TestCheckouts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workspacetest.Git(t, root, "init", "--quiet")
	workspacetest.Git(t, root, "remote", "add", "origin", "git@github.com:a-novel-kit/stack.git")
	fsutiltest.Write(t, root, map[string]string{workspace.ReposFile: `repos:
  - a-novel-kit/golib
  - a-novel-kit/nodelib # listed but not cloned
  - a-novel/service-json-keys
  - a-novel-kit/stack # the root itself
`})
	// Checkouts only stats <dir>/.git, so a marker dir is enough.
	for _, dir := range []string{"kit/golib", "app/service-json-keys", "kit/stack"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir, ".git"), 0o755))
	}

	got, err := workspace.Checkouts(root)
	require.NoError(t, err)
	dirs := map[string]string{}
	for _, c := range got {
		dirs[c.FullName()] = c.Path
	}
	want := map[string]string{
		"a-novel-kit/stack":         root,
		"a-novel-kit/golib":         filepath.Join(root, "kit", "golib"),
		"a-novel/service-json-keys": filepath.Join(root, "app", "service-json-keys"),
	}
	require.Len(t, got, len(want))
	require.Equal(t, want, dirs)
}

// TestRoot pins the --root resolution. zsh leaves ~ unexpanded in --root=~/x,
// so the CLI expands it, and a relative root resolves against the working
// directory.
//
//nolint:paralleltest // t.Setenv("HOME") pins the ~ expansion, and forbids t.Parallel.
func TestRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wd, err := os.Getwd()
	require.NoError(t, err)

	testCases := []struct {
		name string
		root string
		want string
	}{
		{name: "Success/Tilde", root: "~/stacks/review", want: filepath.Join(home, "stacks", "review")},
		{name: "Success/Relative", root: "stacks/review", want: filepath.Join(wd, "stacks", "review")},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := workspace.Root(testCase.root)
			require.NoError(t, err)
			require.Equal(t, testCase.want, got)
		})
	}
}

func TestLoadRepos(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		content string // "" leaves the file absent
		want    []workspace.Repo
		wantErr bool
	}{
		{
			name:    "Success",
			content: "repos:\n  - a-novel-kit/jwt\n  - a-novel/service-json-keys\n",
			want: []workspace.Repo{
				{Org: workspace.OrgAnovelKit, Name: "jwt"},
				{Org: workspace.OrgAnovel, Name: "service-json-keys"},
			},
		},
		// An absent file is nothing to sync, not an error.
		{name: "Success/AbsentFile"},
		{name: "Error/NoSlash", content: "repos:\n  - golib\n", wantErr: true},
		{name: "Error/UnknownOrg", content: "repos:\n  - github/whatever\n", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if testCase.content != "" {
				fsutiltest.Write(t, root, map[string]string{workspace.ReposFile: testCase.content})
			}
			got, err := workspace.LoadRepos(root)
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.want, got)
		})
	}
}

// TestRepo pins how a filter set names a repository, by its full <org>/<name>
// or its bare <name>, and where each repository checks out under the workspace
// root, which the routing sync and --sandbox share.
func TestRepo(t *testing.T) {
	t.Parallel()

	t.Run("Listed", func(t *testing.T) {
		t.Parallel()

		repo := workspace.Repo{Org: workspace.OrgAnovel, Name: "service-json-keys"}
		testCases := []struct {
			name string
			set  map[string]bool
			want bool
		}{
			{name: "Success/EmptySet", set: nil, want: false},
			{name: "Success/FullName", set: map[string]bool{"a-novel/service-json-keys": true}, want: true},
			{name: "Success/BareName", set: map[string]bool{"service-json-keys": true}, want: true},
			{name: "Success/UnrelatedRepo", set: map[string]bool{"a-novel-kit/golib": true}, want: false},
		}

		for _, testCase := range testCases {
			t.Run(testCase.name, func(t *testing.T) {
				t.Parallel()

				require.Equal(t, testCase.want, repo.Listed(testCase.set))
			})
		}
	})

	t.Run("Dir", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name string
			repo workspace.Repo
			want string
		}{
			{name: "Success/Stack", repo: workspace.StackRepo, want: "/ws"},
			{name: "Success/Kit", repo: workspace.Repo{Org: workspace.OrgAnovelKit, Name: "golib"}, want: "/ws/kit/golib"},
			{
				name: "Success/App",
				repo: workspace.Repo{Org: workspace.OrgAnovel, Name: "service-json-keys"},
				want: "/ws/app/service-json-keys",
			},
		}

		for _, testCase := range testCases {
			t.Run(testCase.name, func(t *testing.T) {
				t.Parallel()

				require.Equal(t, testCase.want, testCase.repo.Dir("/ws"))
			})
		}
	})
}
