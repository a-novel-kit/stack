package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

// MappingFile is the per-repo, value-free secrets mapping, committed at the
// service repo root. It maps environment-variable names to secret IDs (never
// values), so `a-novel test`/`run`/`ui` know which secrets to decrypt and under
// which env-var name to inject them into the child process:
//
//	# .a-novel/secrets.yaml
//	env:
//	  OPENAI_API_KEY: openai-key
const MappingFile = ".a-novel/secrets.yaml"

// mapping is the on-disk shape of MappingFile.
type mapping struct {
	// Env maps an environment-variable name to a secret ID in the store.
	Env map[string]string `yaml:"env"`
}

// InjectForRepo reads the value-free mapping at repoRoot/.a-novel/secrets.yaml,
// decrypts each referenced secret from the local store, and returns the
// envVar→value pairs to inject into a child process's environment.
//
// It is deliberately forgiving so secrets setup never blocks test/run:
//   - no mapping, or no local key (secrets never initialized) → empty map, no error;
//   - a mapped secret that isn't in the store yet → that var is skipped (not
//     injected), so a repo can still run the tests that don't need it; the code
//     that does need the secret surfaces its own missing-env error.
//
// Only a malformed/unreadable mapping or a corrupt store returns an error.
//
// The returned values are secret material: callers must inject them into the
// child env only and never print or log them.
func InjectForRepo(repoRoot string) (map[string]string, error) {
	return injectForRepoAt(repoRoot, paths.SecretsRoot())
}

// injectForRepoAt is the testable core of InjectForRepo: it resolves the mapping
// relative to repoRoot and the store under root.
func injectForRepoAt(repoRoot, root string) (map[string]string, error) {
	m, err := readMapping(filepath.Join(repoRoot, MappingFile))
	if err != nil {
		return nil, err
	}
	if len(m.Env) == 0 {
		return map[string]string{}, nil // no mapping → nothing to inject
	}

	// If the store/key isn't set up at all, treat it as "no secrets" rather
	// than failing — the common case for a repo whose mapping exists but whose
	// secrets the current developer hasn't provisioned.
	if _, statErr := os.Stat(filepath.Join(root, keyFile)); errors.Is(statErr, os.ErrNotExist) {
		return map[string]string{}, nil
	}

	st, err := openAt(root)
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(m.Env))
	for envVar, id := range m.Env {
		// A mapped secret that isn't set yet is skipped, not an error — so a repo
		// with a mapping but an unprovisioned secret can still run the tests that
		// don't need it. The dependent code reports the missing env var itself.
		if value, ok := st.Get(id); ok {
			out[envVar] = value
		}
	}
	return out, nil
}

// readMapping parses the mapping file at path. An absent file yields an empty
// mapping and no error (most repos declare none).
func readMapping(path string) (mapping, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return mapping{}, nil
	}
	if err != nil {
		return mapping{}, fmt.Errorf("secrets: read %s: %w", path, err)
	}
	var m mapping
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return mapping{}, fmt.Errorf("secrets: parse %s: %w", path, err)
	}
	return m, nil
}
