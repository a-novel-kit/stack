package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkspaceCheckouts builds a fake workspace — a git repo at the root
// with an origin remote, a whitelist file, and a few clone markers under app/
// and kit/ — and checks the sweep surfaces exactly the pulled repos: the stack
// itself (from root), every whitelisted checkout present on disk, no
// not-yet-cloned entry, and the stack only once even when the whitelist lists it.
func TestWorkspaceCheckouts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustGit(t, root, "init", "--quiet")
	mustGit(t, root, "remote", "add", "origin", "git@github.com:a-novel-kit/stack.git")
	writeFixture(t, root, repoWhitelistFile, strings.Join([]string{
		"repos:",
		"  - a-novel-kit/golib",
		"  - a-novel-kit/nodelib", // listed but not cloned → absent
		"  - a-novel/service-json-keys",
		"  - a-novel-kit/stack", // duplicate of the root → deduped
		"",
	}, "\n"))
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
	byName := map[string]checkout{}
	for _, c := range got {
		if _, dup := byName[c.FullName()]; dup {
			t.Fatalf("%s appears twice in checkouts", c.FullName())
		}
		byName[c.FullName()] = c
	}
	for _, want := range []string{"a-novel-kit/stack", "a-novel-kit/golib", "a-novel/service-json-keys"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("missing candidate %q (got %v)", want, keys(byName))
		}
	}
	if _, ok := byName["a-novel-kit/nodelib"]; ok {
		t.Errorf("nodelib is not cloned; it must not be a candidate (got %v)", keys(byName))
	}
	if len(got) != 3 {
		t.Errorf("want 3 candidates, got %d (%v)", len(got), keys(byName))
	}
	if dir := byName["a-novel-kit/stack"].dir; dir != root {
		t.Errorf("stack candidate dir = %q, want the workspace root %q", dir, root)
	}
}

func TestLoadRepoWhitelist(t *testing.T) {
	t.Parallel()

	t.Run("valid list parses org and name", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFixture(t, root, repoWhitelistFile,
			"repos:\n  - a-novel-kit/jwt\n  - a-novel/service-json-keys\n")
		got, err := loadRepoWhitelist(root)
		if err != nil {
			t.Fatalf("loadRepoWhitelist: %v", err)
		}
		want := []repoEntry{
			{Org: orgAnovelKit, Name: "jwt"},
			{Org: orgAnovel, Name: "service-json-keys"},
		}
		if len(got) != len(want) {
			t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("absent file is nothing to sync, not an error", func(t *testing.T) {
		t.Parallel()
		got, err := loadRepoWhitelist(t.TempDir())
		if err != nil {
			t.Fatalf("loadRepoWhitelist on missing file: %v", err)
		}
		if got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("entry without a slash is rejected", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFixture(t, root, repoWhitelistFile, "repos:\n  - golib\n")
		if _, err := loadRepoWhitelist(root); err == nil {
			t.Error("expected an error for a slash-less entry, got nil")
		}
	})

	t.Run("unknown org is rejected", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFixture(t, root, repoWhitelistFile, "repos:\n  - github/whatever\n")
		if _, err := loadRepoWhitelist(root); err == nil {
			t.Error("expected an error for an unknown org, got nil")
		}
	})
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

// keys returns the map keys, for readable failure messages.
func keys(m map[string]checkout) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
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
