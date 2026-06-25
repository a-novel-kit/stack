package build

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/detect"
	"github.com/a-novel-kit/stack/cli/internal/secrets"
)

// TestPrepareEnv_InjectsRepoSecrets verifies the test/build env-assembly seam:
// PrepareEnv appends the repo's locally-stored secrets (decrypted) to the child
// env and folds them into the delta. The value must appear only in the returned
// env — never in the progress written to `out`.
//
// Not parallel: uses t.Setenv("XDG_DATA_HOME", ...) for store isolation.
func TestPrepareEnv_InjectsRepoSecrets(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	// Seed the encrypted store with a secret.
	st, err := secrets.Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	const secretValue = "sk-prepare-env"
	st.Set("openai-key", secretValue)
	if err := st.Save(); err != nil {
		t.Fatalf("save store: %v", err)
	}

	// A repo with a value-free mapping at its root. With no compose Env,
	// repoRoot(t) == t.Dir, which is this directory.
	repoRoot := t.TempDir()
	mappingDir := filepath.Join(repoRoot, ".a-novel")
	if err := os.MkdirAll(mappingDir, 0o755); err != nil {
		t.Fatalf("mkdir mapping: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mappingDir, "secrets.yaml"),
		[]byte("secrets:\n  - env: OPENAI_API_KEY\n    id: openai-key\n"), 0o600); err != nil {
		t.Fatalf("write mapping: %v", err)
	}

	tgt := detect.Target{Kind: detect.KindGo, Name: "x", Dir: repoRoot}

	var out strings.Builder
	runEnv, delta, err := PrepareEnv(t.Context(), tgt, &out)
	if err != nil {
		t.Fatalf("PrepareEnv: %v", err)
	}

	if !slices.Contains(runEnv, "OPENAI_API_KEY="+secretValue) {
		t.Error("injected secret missing from runEnv")
	}
	if !slices.Contains(delta, "OPENAI_API_KEY="+secretValue) {
		t.Error("injected secret missing from delta")
	}
	if strings.Contains(out.String(), secretValue) {
		t.Errorf("PrepareEnv progress output leaked the secret value:\n%s", out.String())
	}
}

// TestPrepareEnv_NoMappingNoSecrets confirms a repo without a mapping injects
// nothing and does not fail.
func TestPrepareEnv_NoMappingNoSecrets(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	repoRoot := t.TempDir() // no .a-novel/secrets.yaml
	tgt := detect.Target{Kind: detect.KindGo, Name: "x", Dir: repoRoot}

	runEnv, _, err := PrepareEnv(t.Context(), tgt, io.Discard)
	if err != nil {
		t.Fatalf("PrepareEnv: %v", err)
	}
	for _, e := range runEnv {
		if strings.HasPrefix(e, "OPENAI_API_KEY=") {
			t.Errorf("unexpected injected secret in a repo with no mapping: %q", e)
		}
	}
}

// TestPrepareEnv_MissingSecretWarns confirms a declared-but-unset secret is not
// injected, does not fail the build, and surfaces a value-free warning in the
// progress output so the developer knows what to set.
func TestPrepareEnv_MissingSecretWarns(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir()) // fresh store, secret never set

	repoRoot := t.TempDir()
	mappingDir := filepath.Join(repoRoot, ".a-novel")
	if err := os.MkdirAll(mappingDir, 0o755); err != nil {
		t.Fatalf("mkdir mapping: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mappingDir, "secrets.yaml"),
		[]byte("secrets:\n  - env: OPENAI_API_KEY\n    id: openai-key\n    description: used by generation\n"),
		0o600); err != nil {
		t.Fatalf("write mapping: %v", err)
	}

	tgt := detect.Target{Kind: detect.KindGo, Name: "x", Dir: repoRoot}

	var out strings.Builder
	runEnv, _, err := PrepareEnv(t.Context(), tgt, &out)
	if err != nil {
		t.Fatalf("PrepareEnv must not fail on a missing secret: %v", err)
	}
	for _, e := range runEnv {
		if strings.HasPrefix(e, "OPENAI_API_KEY=") {
			t.Errorf("missing secret must not be injected: %q", e)
		}
	}
	got := out.String()
	if !strings.Contains(got, "OPENAI_API_KEY") || !strings.Contains(got, "openai-key") ||
		!strings.Contains(got, "used by generation") ||
		!strings.Contains(got, "a-novel secrets set openai-key") {
		t.Errorf("expected a descriptive missing-secret warning, got:\n%s", got)
	}
}
