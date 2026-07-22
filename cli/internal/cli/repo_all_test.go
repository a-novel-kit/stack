package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

// TestDiscoverUpdateCandidates builds a fake workspace — a git repo at the root
// with an origin remote, a whitelist file, and a few clone markers under app/
// and kit/ — and checks the sweep surfaces exactly the pulled repos: the stack
// itself (from root), every whitelisted checkout present on disk, no
// not-yet-cloned entry, and the stack only once even when the whitelist lists it.
func TestDiscoverUpdateCandidates(t *testing.T) {
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
	// discoverUpdateCandidates only stats <dir>/.git, so a marker dir is enough.
	for _, d := range []string{"kit/golib", "app/service-json-keys", "kit/stack"} {
		if err := os.MkdirAll(filepath.Join(root, d, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	got, err := discoverUpdateCandidates(root)
	if err != nil {
		t.Fatalf("discoverUpdateCandidates: %v", err)
	}
	byName := map[string]updateCandidate{}
	for _, c := range got {
		if _, dup := byName[c.fullName()]; dup {
			t.Fatalf("%s appears twice in candidates", c.fullName())
		}
		byName[c.fullName()] = c
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

// TestOngoingWork covers the git-state guard: a clean default-branch checkout is
// safe (empty reason), while a feature branch, a detached HEAD, or a dirty tree
// each yield a skip reason so the sweep never reconciles from in-progress state.
func TestOngoingWork(t *testing.T) {
	t.Parallel()

	t.Run("clean default branch is safe", func(t *testing.T) {
		t.Parallel()
		local, _ := initSyncRepo(t)
		if reason := ongoingWork(local); reason != "" {
			t.Errorf("clean master: reason = %q, want empty", reason)
		}
	})

	t.Run("off the default branch", func(t *testing.T) {
		t.Parallel()
		local, _ := initSyncRepo(t)
		mustGit(t, local, "checkout", "--quiet", "-b", "feature")
		if reason := ongoingWork(local); reason != "on feature" {
			t.Errorf("feature branch: reason = %q, want %q", reason, "on feature")
		}
	})

	t.Run("detached HEAD", func(t *testing.T) {
		t.Parallel()
		local, _ := initSyncRepo(t)
		mustGit(t, local, "checkout", "--quiet", gitOut(t, local, "rev-parse", "HEAD"))
		if reason := ongoingWork(local); reason != "detached HEAD" {
			t.Errorf("detached: reason = %q, want %q", reason, "detached HEAD")
		}
	})

	t.Run("dirty working tree on the default branch", func(t *testing.T) {
		t.Parallel()
		local, _ := initSyncRepo(t)
		writeFixture(t, local, "a.txt", "work in progress\n")
		if reason := ongoingWork(local); reason != "uncommitted changes" {
			t.Errorf("dirty tree: reason = %q, want %q", reason, "uncommitted changes")
		}
	})
}

// TestExcludedRepo checks the --exclude matcher accepts both the full
// <org>/<name> and the bare <name>, and that comma-joined values split.
func TestExcludedRepo(t *testing.T) {
	t.Parallel()
	c := updateCandidate{org: orgAnovel, repo: "service-json-keys"}
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
			if got := excludedRepo(normaliseFilter(tc.set), c); got != tc.want {
				t.Errorf("excludedRepo(%v) = %v, want %v", tc.set, got, tc.want)
			}
		})
	}
}

// TestRenderCompactSummary asserts the batch view shows the per-repo dimensions
// that vary — class, rulesets, codecov, and the discovered required checks.
func TestRenderCompactSummary(t *testing.T) {
	t.Parallel()
	target := &repocfg.RepoTarget{
		Org:  orgAnovel,
		Repo: "service-auth",
		Class: &repocfg.ClassPreset{
			Class:    repocfg.ClassService,
			Rulesets: repocfg.ClassRulesets{Master: true, RequireApproval: true},
		},
		Discovered: &repocfg.Discovered{Checks: []repocfg.CheckRef{{Context: "lint-go"}, {Context: "test-go"}}},
	}
	var buf bytes.Buffer
	renderCompactSummary(&buf, target)
	got := buf.String()
	for _, want := range []string{
		"a-novel/service-auth", "class service",
		"master", "require-approval",
		"lint-go, test-go (2)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("compact summary missing %q\n--- got ---\n%s", want, got)
		}
	}
	// A single repo's overview must stay compact — two lines, not the full grouped view.
	if lines := strings.Count(strings.TrimSpace(got), "\n"); lines > 1 {
		t.Errorf("compact summary should be at most 2 lines, got %d:\n%s", lines+1, got)
	}
}

// TestRenderAllJSON checks the --all --json document shape: an array of
// {repo, ops} objects, and a clean "[]" when nothing is eligible.
func TestRenderAllJSON(t *testing.T) {
	t.Parallel()

	t.Run("with plans", func(t *testing.T) {
		t.Parallel()
		items := []plannedUpdate{{
			cand: updateCandidate{org: orgAnovel, repo: "service-json-keys"},
			plan: &repocfg.Plan{Ops: []repocfg.Op{{Method: "PATCH", Path: "repos/a-novel/service-json-keys"}}},
		}}
		var buf bytes.Buffer
		if err := renderAllJSON(&buf, items); err != nil {
			t.Fatalf("renderAllJSON: %v", err)
		}
		for _, want := range []string{`"repo": "a-novel/service-json-keys"`, `"method": "PATCH"`, `"ops"`} {
			if !strings.Contains(buf.String(), want) {
				t.Errorf("json missing %q\n%s", want, buf.String())
			}
		}
	})

	t.Run("empty renders as an array", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		if err := renderAllJSON(&buf, nil); err != nil {
			t.Fatalf("renderAllJSON(nil): %v", err)
		}
		if got := strings.TrimSpace(buf.String()); got != "[]" {
			t.Errorf("empty json = %q, want %q", got, "[]")
		}
	})
}

// keys returns the map keys, for readable failure messages.
func keys(m map[string]updateCandidate) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
