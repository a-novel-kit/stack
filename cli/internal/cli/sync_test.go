package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustGit runs a git command in dir and fails the test on error.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := runGit(dir, args...); err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
}

// gitOut runs a git command in dir and returns its trimmed output.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGit(dir, args...)
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(out)
}

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func readFixture(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// initSyncRepo builds a bare "origin" with one commit on master (files a.txt and
// b.txt) plus a local clone of it, and returns (local, seed). The seed is a
// second working clone used to push new commits into origin so tests can put
// the local clone "behind". origin/HEAD is set by the clone, so
// resolveDefaultBranch reads "master" the same way it does in the field.
func initSyncRepo(t *testing.T) (string, string) {
	t.Helper()
	origin := t.TempDir()
	seed := t.TempDir()
	local := t.TempDir()

	mustGit(t, origin, "init", "--bare", "--quiet", "--initial-branch=master")
	mustGit(t, seed, "init", "--quiet", "--initial-branch=master")
	mustGit(t, seed, "config", "user.email", "test@a-novel.dev")
	mustGit(t, seed, "config", "user.name", "test")
	writeFixture(t, seed, "a.txt", "a0\n")
	writeFixture(t, seed, "b.txt", "b0\n")
	mustGit(t, seed, "add", "-A")
	mustGit(t, seed, "commit", "--quiet", "-m", "init")
	mustGit(t, seed, "remote", "add", "origin", origin)
	mustGit(t, seed, "push", "--quiet", "-u", "origin", "master")

	// Clone into the (empty) local TempDir; the origin URL is absolute so the
	// -C directory is irrelevant.
	mustGit(t, seed, "clone", "--quiet", origin, local)
	mustGit(t, local, "config", "user.email", "test@a-novel.dev")
	mustGit(t, local, "config", "user.name", "test")
	return local, seed
}

// advanceOrigin rewrites a.txt to "a1" in the seed clone and pushes it, so a
// subsequent updateExistingRepo on the local clone sees origin ahead by one
// fast-forwardable commit.
func advanceOrigin(t *testing.T, seed string) {
	t.Helper()
	writeFixture(t, seed, "a.txt", "a1\n")
	mustGit(t, seed, "add", "-A")
	mustGit(t, seed, "commit", "--quiet", "-m", "advance a.txt")
	mustGit(t, seed, "push", "--quiet", "origin", "master")
}

var testRepo = repoEntry{Org: orgAnovelKit, Name: "golib"}

// On the default branch with a clean tree, sync fast-forwards it.
func TestUpdateExistingRepo_OnDefaultBranch_CleanFastForwards(t *testing.T) {
	t.Parallel()
	local, seed := initSyncRepo(t)
	advanceOrigin(t, seed)

	var counts syncCounts
	updateExistingRepo(io.Discard, local, testRepo, &counts)

	if counts.Updated != 1 {
		t.Fatalf("Updated=%d, want 1 (%v)", counts.Updated, counts.Lines)
	}
	if got := gitOut(t, local, "rev-parse", "refs/heads/master"); got != gitOut(t, local, "rev-parse", "refs/remotes/origin/master") {
		t.Errorf("local master %s not fast-forwarded to origin", got)
	}
	if got := readFixture(t, local, "a.txt"); got != "a1\n" {
		t.Errorf("a.txt = %q, want the pulled content", got)
	}
}

// The heart of the ask: on the default branch, an UNRELATED unstaged change is
// preserved across the fast-forward.
func TestUpdateExistingRepo_OnDefaultBranch_UnrelatedDirtyPreserved(t *testing.T) {
	t.Parallel()
	local, seed := initSyncRepo(t)
	advanceOrigin(t, seed)                         // origin moves a.txt
	writeFixture(t, local, "b.txt", "local-wip\n") // local edits a different file

	var counts syncCounts
	updateExistingRepo(io.Discard, local, testRepo, &counts)

	if counts.Updated != 1 {
		t.Fatalf("Updated=%d, want 1 (%v)", counts.Updated, counts.Lines)
	}
	if got := readFixture(t, local, "b.txt"); got != "local-wip\n" {
		t.Errorf("unstaged b.txt = %q, want it preserved", got)
	}
	if got := readFixture(t, local, "a.txt"); got != "a1\n" {
		t.Errorf("a.txt = %q, want the pulled content", got)
	}
}

// The other half: a CONFLICTING unstaged change is never clobbered — sync skips
// and leaves both HEAD and the working tree exactly as they were.
func TestUpdateExistingRepo_OnDefaultBranch_ConflictingDirtySkipped(t *testing.T) {
	t.Parallel()
	local, seed := initSyncRepo(t)
	advanceOrigin(t, seed)                      // origin moves a.txt
	writeFixture(t, local, "a.txt", "my-wip\n") // local edits the SAME file
	headBefore := gitOut(t, local, "rev-parse", "HEAD")

	var counts syncCounts
	updateExistingRepo(io.Discard, local, testRepo, &counts)

	if counts.Skipped != 1 {
		t.Fatalf("Skipped=%d, want 1 (%v)", counts.Skipped, counts.Lines)
	}
	if got := gitOut(t, local, "rev-parse", "HEAD"); got != headBefore {
		t.Errorf("HEAD moved to %s despite the conflict; want %s", got, headBefore)
	}
	if got := readFixture(t, local, "a.txt"); got != "my-wip\n" {
		t.Errorf("unstaged a.txt = %q, want it left untouched", got)
	}
}

// Off the default branch: the master ref is fast-forwarded without leaving the
// feature branch or touching the working tree.
func TestUpdateExistingRepo_OffDefaultBranch_UpdatesRefOnly(t *testing.T) {
	t.Parallel()
	local, seed := initSyncRepo(t)
	mustGit(t, local, "checkout", "--quiet", "-b", "feature")
	masterBefore := gitOut(t, local, "rev-parse", "refs/heads/master")
	advanceOrigin(t, seed)
	writeFixture(t, local, "b.txt", "feature-wip\n") // dirty tree on the feature branch

	var counts syncCounts
	updateExistingRepo(io.Discard, local, testRepo, &counts)

	if counts.Updated != 1 {
		t.Fatalf("Updated=%d, want 1 (%v)", counts.Updated, counts.Lines)
	}
	if got := gitOut(t, local, "symbolic-ref", "--short", "HEAD"); got != "feature" {
		t.Errorf("HEAD = %q, want to stay on 'feature'", got)
	}
	master := gitOut(t, local, "rev-parse", "refs/heads/master")
	if master == masterBefore {
		t.Errorf("master ref did not advance (still %s)", master)
	}
	if origin := gitOut(t, local, "rev-parse", "refs/remotes/origin/master"); master != origin {
		t.Errorf("master %s not fast-forwarded to origin %s", master, origin)
	}
	if got := readFixture(t, local, "b.txt"); got != "feature-wip\n" {
		t.Errorf("unstaged b.txt = %q, want it preserved", got)
	}
	// The feature checkout never advanced, so a.txt keeps the base content.
	if got := readFixture(t, local, "a.txt"); got != "a0\n" {
		t.Errorf("a.txt = %q, want the feature-branch content untouched", got)
	}
}

// Already up to date is reported as such and touches nothing.
func TestUpdateExistingRepo_UpToDate(t *testing.T) {
	t.Parallel()
	local, _ := initSyncRepo(t)

	var counts syncCounts
	updateExistingRepo(io.Discard, local, testRepo, &counts)

	if counts.UpToDate != 1 {
		t.Fatalf("UpToDate=%d, want 1 (%v)", counts.UpToDate, counts.Lines)
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
		// The org→subdir routing the loader guards is what makes the entry usable.
		if got[0].TargetSubdir() != "kit" || got[1].TargetSubdir() != "app" {
			t.Errorf("subdir routing wrong: %q / %q", got[0].TargetSubdir(), got[1].TargetSubdir())
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
