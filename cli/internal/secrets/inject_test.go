package secrets_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/secrets"
)

// writeMapping creates repoRoot/.a-novel/secrets.yaml with the given content.
func writeMapping(t *testing.T, repoRoot, content string) {
	t.Helper()
	dir := filepath.Join(repoRoot, ".a-novel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir .a-novel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write mapping: %v", err)
	}
}

// TestInjectForRepo covers the value-free mapping → decrypted env-pair flow,
// including the absent-store and unknown-secret cases.
//
// Not parallel: uses t.Setenv("XDG_DATA_HOME", ...) for store isolation.
func TestInjectForRepo(t *testing.T) {
	t.Run("MappingResolvesSecrets", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		st, err := secrets.Open()
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		st.Set("openai-key", "sk-live")
		st.Set("anthropic-key", "ak-live")
		if err := st.Save(); err != nil {
			t.Fatalf("save: %v", err)
		}

		repoRoot := t.TempDir()
		writeMapping(t, repoRoot, "env:\n  OPENAI_API_KEY: openai-key\n  ANTHROPIC_API_KEY: anthropic-key\n")

		got, err := secrets.InjectForRepo(repoRoot)
		if err != nil {
			t.Fatalf("inject: %v", err)
		}
		if got["OPENAI_API_KEY"] != "sk-live" {
			t.Errorf("OPENAI_API_KEY = %q, want %q", got["OPENAI_API_KEY"], "sk-live")
		}
		if got["ANTHROPIC_API_KEY"] != "ak-live" {
			t.Errorf("ANTHROPIC_API_KEY = %q, want %q", got["ANTHROPIC_API_KEY"], "ak-live")
		}
	})

	t.Run("AbsentMappingReturnsEmpty", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		got, err := secrets.InjectForRepo(t.TempDir()) // no .a-novel/secrets.yaml
		if err != nil {
			t.Fatalf("inject: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty map for a repo with no mapping, got %v keys", len(got))
		}
	})

	t.Run("AbsentStoreReturnsEmpty", func(t *testing.T) {
		// XDG points at a fresh dir; we deliberately never Open/Init, so no
		// key/store exists. A mapping that references secrets must NOT fail.
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		repoRoot := t.TempDir()
		writeMapping(t, repoRoot, "env:\n  OPENAI_API_KEY: openai-key\n")

		got, err := secrets.InjectForRepo(repoRoot)
		if err != nil {
			t.Fatalf("inject with no store should not error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty map when the store is absent, got %v", got)
		}
	})

	t.Run("UnknownSecretIsError", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		st, err := secrets.Open()
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		st.Set("present", "v")
		if err := st.Save(); err != nil {
			t.Fatalf("save: %v", err)
		}

		repoRoot := t.TempDir()
		writeMapping(t, repoRoot, "env:\n  MISSING_VAR: not-in-store\n")

		if _, err := secrets.InjectForRepo(repoRoot); err == nil {
			t.Fatal("expected an error when the mapping references an unknown secret id")
		}
	})

	t.Run("EmptyEnvBlockReturnsEmpty", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		repoRoot := t.TempDir()
		writeMapping(t, repoRoot, "env: {}\n")

		got, err := secrets.InjectForRepo(repoRoot)
		if err != nil {
			t.Fatalf("inject: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty map for an empty env block, got %v", got)
		}
	})
}
