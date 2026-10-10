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

func TestUnpushedCommits(t *testing.T) {
	t.Parallel()

	local, _ := initSyncRepo(t)

	if n := unpushedCommits(local); n != 0 {
		t.Fatalf("fresh clone has %d unpushed commits, want 0", n)
	}

	writeFixture(t, local, "c.txt", "c0\n")
	mustGit(t, local, "add", "-A")
	mustGit(t, local, "commit", "--quiet", "-m", "local only")

	if n := unpushedCommits(local); n != 1 {
		t.Fatalf("after one local commit: %d unpushed, want 1", n)
	}

	mustGit(t, local, "push", "--quiet")

	if n := unpushedCommits(local); n != 0 {
		t.Fatalf("after push: %d unpushed, want 0", n)
	}
}

// TestUnpushedCommitsNoUpstream covers a checkout with no upstream at all,
// which counts as zero unpushed commits.
func TestUnpushedCommitsNoUpstream(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustGit(t, dir, "init", "--quiet", "--initial-branch=master")
	mustGit(t, dir, "config", "user.email", "test@a-novel.dev")
	mustGit(t, dir, "config", "user.name", "test")
	writeFixture(t, dir, "a.txt", "a0\n")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "--quiet", "-m", "init")

	if n := unpushedCommits(dir); n != 0 {
		t.Fatalf("no-upstream checkout reported %d unpushed, want 0", n)
	}
}

func TestGitToplevel(t *testing.T) {
	t.Parallel()

	root := initPublishRepo(t)
	sub := filepath.Join(root, "deep", "inside")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := gitToplevel(sub)
	if err != nil {
		t.Fatalf("gitToplevel: %v", err)
	}
	// Resolve symlinks on both sides — macOS TempDirs live under /private.
	wantResolved, _ := filepath.EvalSymlinks(root)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Errorf("toplevel = %q, want %q", gotResolved, wantResolved)
	}

	if _, err = gitToplevel(t.TempDir()); err == nil {
		t.Error("expected error outside a git repo, got none")
	}

	// An independent repo nested inside (a pulled checkout under app/) resolves
	// to ITSELF, not the outer repo — this is what scopes `publish` to a single
	// service when you cd into it.
	nested := filepath.Join(root, "app", "service-x")
	if err = os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if out, gErr := runGit(nested, "init", "--quiet", "--initial-branch=master"); gErr != nil {
		t.Fatalf("init nested: %v\n%s", gErr, out)
	}
	got, err = gitToplevel(nested)
	if err != nil {
		t.Fatalf("gitToplevel(nested): %v", err)
	}
	wantNested, _ := filepath.EvalSymlinks(nested)
	gotNested, _ := filepath.EvalSymlinks(got)
	if gotNested != wantNested {
		t.Errorf("nested toplevel = %q, want %q (must scope to the nested repo, not the outer)", gotNested, wantNested)
	}
}
