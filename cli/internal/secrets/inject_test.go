package secrets_test

import (
	"os"
	"path/filepath"
	"strings"
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

// TestInjectForRepo covers the value-free manifest → Resolution flow: the
// set-secret (injected), unset-secret (reported missing), absent-store (all
// missing), absent-manifest, and malformed-manifest cases.
//
// Not parallel: uses t.Setenv("XDG_DATA_HOME", ...) for store isolation.
func TestInjectForRepo(t *testing.T) {
	t.Run("ManifestResolvesSecrets", func(t *testing.T) {
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
		writeMapping(t, repoRoot, "secrets:\n"+
			"  - env: OPENAI_API_KEY\n    id: openai-key\n"+
			"  - env: ANTHROPIC_API_KEY\n    id: anthropic-key\n")

		res, err := secrets.InjectForRepo(repoRoot)
		if err != nil {
			t.Fatalf("inject: %v", err)
		}
		if res.Env["OPENAI_API_KEY"] != "sk-live" {
			t.Errorf("OPENAI_API_KEY = %q, want %q", res.Env["OPENAI_API_KEY"], "sk-live")
		}
		if res.Env["ANTHROPIC_API_KEY"] != "ak-live" {
			t.Errorf("ANTHROPIC_API_KEY = %q, want %q", res.Env["ANTHROPIC_API_KEY"], "ak-live")
		}
		if len(res.Missing) != 0 {
			t.Errorf("expected no missing, got %v", res.Missing)
		}
	})

	t.Run("AbsentManifestReturnsEmpty", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		res, err := secrets.InjectForRepo(t.TempDir()) // no .a-novel/secrets.yaml
		if err != nil {
			t.Fatalf("inject: %v", err)
		}
		if len(res.Env) != 0 || len(res.Missing) != 0 {
			t.Fatalf("expected empty Resolution for a repo with no manifest, got %+v", res)
		}
	})

	t.Run("AbsentStoreReportsAllMissing", func(t *testing.T) {
		// XDG points at a fresh dir; we deliberately never Open/Init, so no
		// key/store exists. A manifest that references secrets must NOT fail —
		// every declared secret is reported missing so the dev knows what to set.
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		repoRoot := t.TempDir()
		writeMapping(t, repoRoot, "secrets:\n  - env: OPENAI_API_KEY\n    id: openai-key\n")

		res, err := secrets.InjectForRepo(repoRoot)
		if err != nil {
			t.Fatalf("inject with no store should not error: %v", err)
		}
		if len(res.Env) != 0 {
			t.Errorf("expected no injected env when the store is absent, got %v", res.Env)
		}
		if len(res.Missing) != 1 || res.Missing[0].ID != "openai-key" {
			t.Fatalf("expected the declared secret reported missing, got %+v", res.Missing)
		}
	})

	t.Run("UnsetSecretIsReportedMissing", func(t *testing.T) {
		// A manifest that references a secret not in the store must NOT fail or
		// block injection — the unset one is reported missing (with its
		// description), the set ones still inject.
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
		writeMapping(t, repoRoot, "secrets:\n"+
			"  - env: PRESENT_VAR\n    id: present\n"+
			"  - env: MISSING_VAR\n    id: not-in-store\n    description: needed for X\n")

		res, err := secrets.InjectForRepo(repoRoot)
		if err != nil {
			t.Fatalf("inject must not error on an unset declared secret: %v", err)
		}
		if res.Env["PRESENT_VAR"] != "v" {
			t.Errorf("PRESENT_VAR = %q, want %q", res.Env["PRESENT_VAR"], "v")
		}
		if _, ok := res.Env["MISSING_VAR"]; ok {
			t.Error("MISSING_VAR should not be injected when its secret is unset")
		}
		if len(res.Missing) != 1 || res.Missing[0].Env != "MISSING_VAR" ||
			res.Missing[0].Description != "needed for X" {
			t.Fatalf("expected MISSING_VAR reported missing with its description, got %+v", res.Missing)
		}
	})

	t.Run("EmptySecretsBlockReturnsEmpty", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		repoRoot := t.TempDir()
		writeMapping(t, repoRoot, "secrets: []\n")

		res, err := secrets.InjectForRepo(repoRoot)
		if err != nil {
			t.Fatalf("inject: %v", err)
		}
		if len(res.Env) != 0 || len(res.Missing) != 0 {
			t.Fatalf("expected empty Resolution for an empty secrets block, got %+v", res)
		}
	})

	t.Run("MalformedManifestErrors", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		repoRoot := t.TempDir()
		writeMapping(t, repoRoot, "secrets: [this is not: valid: yaml\n")

		if _, err := secrets.InjectForRepo(repoRoot); err == nil {
			t.Fatal("expected an error for a malformed manifest")
		}
	})

	t.Run("LegacyEnvShapeErrors", func(t *testing.T) {
		// The legacy top-level `env:` map shape (pre-list) is an unknown field
		// under strict decoding — it must error loudly, not silently inject
		// nothing.
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		repoRoot := t.TempDir()
		writeMapping(t, repoRoot, "env:\n  OPENAI_API_KEY: openai-key\n")

		if _, err := secrets.InjectForRepo(repoRoot); err == nil {
			t.Fatal("expected an error for the legacy env: map shape")
		}
	})

	t.Run("MissingRequiredFieldErrors", func(t *testing.T) {
		// A declaration without an id (or env) is malformed — it must error, not
		// be silently skipped.
		t.Setenv("XDG_DATA_HOME", t.TempDir())

		repoRoot := t.TempDir()
		writeMapping(t, repoRoot, "secrets:\n  - env: OPENAI_API_KEY\n")

		if _, err := secrets.InjectForRepo(repoRoot); err == nil {
			t.Fatal("expected an error for a declaration missing its id")
		}
	})
}

// TestResolutionWarnings asserts the warning lines are actionable, include the
// optional description when present, and never carry a secret value.
func TestResolutionWarnings(t *testing.T) {
	res := secrets.Resolution{
		Missing: []secrets.Declaration{
			{Env: "OPENAI_API_KEY", ID: "openai-key", Description: "used by generation"},
			{Env: "BARE_VAR", ID: "bare-id"}, // no description
		},
	}
	w := res.Warnings()
	if len(w) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %v", len(w), w)
	}
	if !strings.Contains(w[0], "OPENAI_API_KEY") ||
		!strings.Contains(w[0], "openai-key") ||
		!strings.Contains(w[0], "used by generation") ||
		!strings.Contains(w[0], "a-novel secrets set openai-key") {
		t.Errorf("first warning missing expected content: %q", w[0])
	}
	// A declaration without a description still names the env, id, and the fix.
	if !strings.Contains(w[1], "BARE_VAR") || !strings.Contains(w[1], "bare-id") ||
		!strings.Contains(w[1], "a-novel secrets set bare-id") {
		t.Errorf("second warning missing expected content: %q", w[1])
	}

	// An empty Resolution yields no warnings.
	if got := (secrets.Resolution{}).Warnings(); got != nil {
		t.Errorf("empty Resolution should produce no warnings, got %v", got)
	}
}
