package cli

import (
	"strings"
	"testing"
)

// Not parallel: every sub-test calls t.Setenv, which forbids it.
func TestDefaultStackPath(t *testing.T) {
	t.Run("first entry wins", func(t *testing.T) {
		t.Setenv("A_NOVEL_STACKS", "main:/srv/main,fork:/srv/fork")
		got, err := defaultStackPath()
		if err != nil {
			t.Fatalf("defaultStackPath: %v", err)
		}
		if got != "/srv/main" {
			t.Errorf("got %q, want /srv/main", got)
		}
	})

	t.Run("unset falls back to the implicit default", func(t *testing.T) {
		t.Setenv("A_NOVEL_STACKS", "")
		t.Setenv("HOME", "/home/tester")
		got, err := defaultStackPath()
		if err != nil {
			t.Fatalf("defaultStackPath: %v", err)
		}
		if got != "/home/tester/git-projects/a-novel" {
			t.Errorf("got %q, want the implicit ~/git-projects/a-novel", got)
		}
	})

	t.Run("malformed entry errors", func(t *testing.T) {
		t.Setenv("A_NOVEL_STACKS", "no-path-here")
		if _, err := defaultStackPath(); err == nil {
			t.Fatal("want an error for an entry with no name:path separator, got nil")
		}
	})
}

// TestClaudeCmdForwardsFlags pins the point of `a-novel claude`: every flag
// reaches claude, so `a-novel claude --continue` works.
func TestClaudeCmdForwardsFlags(t *testing.T) {
	cmd := newClaudeCmd()
	if !cmd.DisableFlagParsing {
		t.Error("DisableFlagParsing must stay on so claude's own flags pass through")
	}
	if !strings.Contains(cmd.Long, "a-novel help claude") {
		t.Error("Long help must point at 'a-novel help claude', since --help goes to claude")
	}
}
