package cli

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestWorkspaceCheckouts builds a fake workspace (a git repo at the root with
// an origin remote, a whitelist, and clone markers under app/ and kit/) and
// checks the sweep surfaces exactly the pulled repos: the stack itself, from
// the root and only once, plus every whitelisted checkout present on disk.
func TestWorkspaceCheckouts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustGit(t, root, "init", "--quiet")
	mustGit(t, root, "remote", "add", "origin", "git@github.com:a-novel-kit/stack.git")
	writeFixture(t, root, repoWhitelistFile, `repos:
  - a-novel-kit/golib
  - a-novel-kit/nodelib # listed but not cloned
  - a-novel/service-json-keys
  - a-novel-kit/stack # the root itself
`)
	// workspaceCheckouts only stats <dir>/.git, so a marker dir is enough.
	for _, d := range []string{"kit/golib", "app/service-json-keys", "kit/stack"} {
		if err := os.MkdirAll(filepath.Join(root, d, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	got, err := workspaceCheckouts(root)
	if err != nil {
		t.Fatalf("workspaceCheckouts: %v", err)
	}
	dirs := map[string]string{}
	for _, c := range got {
		dirs[c.FullName()] = c.dir
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

func TestLoadRepoWhitelist(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		content string // "" leaves the file absent
		want    []repoEntry
		wantErr bool
	}{
		{
			name:    "Success",
			content: "repos:\n  - a-novel-kit/jwt\n  - a-novel/service-json-keys\n",
			want:    []repoEntry{{Org: orgAnovelKit, Name: "jwt"}, {Org: orgAnovel, Name: "service-json-keys"}},
		},
		// An absent file is nothing to sync, not an error.
		{name: "Success/AbsentFile"},
		{name: "Error/NoSlash", content: "repos:\n  - golib\n", wantErr: true},
		{name: "Error/UnknownOrg", content: "repos:\n  - github/whatever\n", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if tc.content != "" {
				writeFixture(t, root, repoWhitelistFile, tc.content)
			}
			got, err := loadRepoWhitelist(root)
			if (err != nil) != tc.wantErr || !slices.Equal(got, tc.want) {
				t.Fatalf("loadRepoWhitelist = (%+v, %v), want (%+v, error %v)", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

// TestRepoEntryListed checks a filter set names a repo by its full
// <org>/<name> or its bare <name>, and that comma-joined values split.
func TestRepoEntryListed(t *testing.T) {
	t.Parallel()
	c := repoEntry{Org: orgAnovel, Name: "service-json-keys"}
	cases := []struct {
		name string
		set  []string
		want bool
	}{
		{"empty set", nil, false},
		{"full name", []string{"a-novel/service-json-keys"}, true},
		{"bare name", []string{"service-json-keys"}, true},
		{"unrelated repo", []string{"a-novel-kit/golib"}, false},
		{"comma-joined list", []string{"golib,service-json-keys"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := c.listed(normaliseFilter(tc.set)); got != tc.want {
				t.Errorf("listed(%v) = %v, want %v", tc.set, got, tc.want)
			}
		})
	}
}

// TestRepoEntryDir pins where each repository checks out under the workspace
// root, the routing sync, repo update --all and --sandbox share.
func TestRepoEntryDir(t *testing.T) {
	t.Parallel()

	cases := []struct {
		repo repoEntry
		want string
	}{
		{repo: stackRepo, want: "/ws"},
		{repo: repoEntry{Org: orgAnovelKit, Name: "golib"}, want: "/ws/kit/golib"},
		{repo: repoEntry{Org: orgAnovel, Name: "service-json-keys"}, want: "/ws/app/service-json-keys"},
	}
	for _, c := range cases {
		t.Run(c.repo.FullName(), func(t *testing.T) {
			t.Parallel()
			if got := c.repo.Dir("/ws"); got != c.want {
				t.Errorf("Dir = %q, want %q", got, c.want)
			}
		})
	}
}
