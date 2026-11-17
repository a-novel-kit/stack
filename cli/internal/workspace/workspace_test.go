package workspace

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
	"github.com/a-novel-kit/stack/cli/internal/workspace/workspacetest"
)

// TestWorkspaceCheckouts builds a fake workspace (a git repo at the root with
// an origin remote, a whitelist, and clone markers under app/ and kit/) and
// checks the sweep surfaces exactly the pulled repos: the stack itself, from
// the root and only once, plus every whitelisted checkout present on disk.
func TestWorkspaceCheckouts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	workspacetest.Git(t, root, "init", "--quiet")
	workspacetest.Git(t, root, "remote", "add", "origin", "git@github.com:a-novel-kit/stack.git")
	fsutiltest.Write(t, root, map[string]string{ReposFile: `repos:
  - a-novel-kit/golib
  - a-novel-kit/nodelib # listed but not cloned
  - a-novel/service-json-keys
  - a-novel-kit/stack # the root itself
`})
	// Checkouts only stats <dir>/.git, so a marker dir is enough.
	checkouts := []string{"kit/golib", "app/service-json-keys", "kit/stack"}
	for _, d := range checkouts {
		if err := os.MkdirAll(filepath.Join(root, d, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	got, err := Checkouts(root)
	if err != nil {
		t.Fatalf("Checkouts: %v", err)
	}
	dirs := map[string]string{}
	for _, c := range got {
		dirs[c.FullName()] = c.Path
	}
	want := map[string]string{
		"a-novel-kit/stack":         root,
		"a-novel-kit/golib":         filepath.Join(root, "kit", "golib"),
		"a-novel/service-json-keys": filepath.Join(root, "app", "service-json-keys"),
	}
	if len(got) != len(want) || !maps.Equal(dirs, want) {
		t.Errorf("checkouts = %+v, want %v", got, want)
	}
}

// TestWorkspaceRoot pins the --root resolution. zsh leaves ~ unexpanded in
// --root=~/x, so the CLI expands it, and a relative root resolves against the
// working directory.
func TestWorkspaceRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

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
			got, err := Root(testCase.root)
			if err != nil {
				t.Fatalf("Root: %v", err)
			}
			if got != testCase.want {
				t.Errorf("Root(%q) = %q, want %q", testCase.root, got, testCase.want)
			}
		})
	}
}

func TestLoadRepoWhitelist(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		content string // "" leaves the file absent
		want    []Repo
		wantErr bool
	}{
		{
			name:    "Success",
			content: "repos:\n  - a-novel-kit/jwt\n  - a-novel/service-json-keys\n",
			want:    []Repo{{Org: OrgAnovelKit, Name: "jwt"}, {Org: OrgAnovel, Name: "service-json-keys"}},
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
				fsutiltest.Write(t, root, map[string]string{ReposFile: testCase.content})
			}
			got, err := LoadRepos(root)
			if (err != nil) != testCase.wantErr || !slices.Equal(got, testCase.want) {
				t.Fatalf("LoadRepos = (%+v, %v), want (%+v, error %v)", got, err, testCase.want, testCase.wantErr)
			}
		})
	}
}

// TestRepoEntryListed checks a filter set names a repo by its full
// <org>/<name> or its bare <name>.
func TestRepoEntryListed(t *testing.T) {
	t.Parallel()
	c := Repo{Org: OrgAnovel, Name: "service-json-keys"}
	testCases := []struct {
		name string
		set  map[string]bool
		want bool
	}{
		{"empty set", nil, false},
		{"full name", map[string]bool{"a-novel/service-json-keys": true}, true},
		{"bare name", map[string]bool{"service-json-keys": true}, true},
		{"unrelated repo", map[string]bool{"a-novel-kit/golib": true}, false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := c.Listed(testCase.set); got != testCase.want {
				t.Errorf("listed(%v) = %v, want %v", testCase.set, got, testCase.want)
			}
		})
	}
}

// TestRepoEntryDir pins where each repository checks out under the workspace
// root, which the routing sync and --sandbox share.
func TestRepoEntryDir(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		repo Repo
		want string
	}{
		{repo: StackRepo, want: "/ws"},
		{repo: Repo{Org: OrgAnovelKit, Name: "golib"}, want: "/ws/kit/golib"},
		{repo: Repo{Org: OrgAnovel, Name: "service-json-keys"}, want: "/ws/app/service-json-keys"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.repo.FullName(), func(t *testing.T) {
			t.Parallel()
			if got := testCase.repo.Dir("/ws"); got != testCase.want {
				t.Errorf("Dir = %q, want %q", got, testCase.want)
			}
		})
	}
}
