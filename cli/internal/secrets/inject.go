package secrets

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

// MappingFile is the per-repo, value-free secrets manifest, committed at the
// service repo root. It declares which secrets the service needs — each entry
// pairs a target environment-variable name with a secret ID in the local store
// (never a value) and an optional human description — so `a-novel test`/`run`/
// `ui` know which secrets to decrypt, under which env-var name to inject them,
// and what to say when one isn't set yet:
//
//	# .a-novel/secrets.yaml
//	secrets:
//	  - env: OPENAI_API_KEY
//	    id: openai-key
//	    description: OpenAI API key used by narrative generation.
const MappingFile = ".a-novel/secrets.yaml"

// Declaration is one entry in MappingFile: a target env var, the secret ID that
// supplies its value, and an optional description surfaced when the secret is
// unset. It never carries a value.
type Declaration struct {
	// Env is the environment-variable name the secret is injected under.
	Env string `yaml:"env"`
	// ID is the secret's key in the local store.
	ID string `yaml:"id"`
	// Description is an optional, human-readable note shown in the
	// missing-secret warning. Value-free.
	Description string `yaml:"description"`
}

// mapping is the on-disk shape of MappingFile.
type mapping struct {
	// Secrets is the list of declarations the service needs.
	Secrets []Declaration `yaml:"secrets"`
}

// Resolution is the outcome of resolving a repo's MappingFile against the local
// store: the env pairs ready to inject, and the declarations whose secret isn't
// set yet (surfaced as a descriptive warning, never a hard failure).
type Resolution struct {
	// Env maps env-var name → decrypted secret value. This is secret material:
	// inject it into a child process's environment only; never print or log it.
	Env map[string]string
	// Missing lists declarations whose secret is absent from the store (or whose
	// store/key isn't set up yet). Carries env/id/description — never a value.
	Missing []Declaration
}

// Warnings renders one actionable, value-free line per missing declaration,
// suitable for stderr or a service log. The secret value is never included.
func (r Resolution) Warnings() []string {
	if len(r.Missing) == 0 {
		return nil
	}
	out := make([]string, 0, len(r.Missing))
	for _, d := range r.Missing {
		line := fmt.Sprintf("⚠ secret not set: %s (id: %s)", d.Env, d.ID)
		if d.Description != "" {
			line += " — " + d.Description
		}
		line += " — run: a-novel secrets set " + d.ID
		out = append(out, line)
	}
	return out
}

// InjectForRepo reads the value-free manifest at repoRoot/.a-novel/secrets.yaml,
// decrypts each declared secret from the local store, and returns the env pairs
// to inject plus the declarations still missing.
//
// It is deliberately forgiving so secrets setup never blocks test/run:
//   - no manifest → empty Resolution, no error;
//   - no local key / store (secrets never initialized) → every declared secret
//     is reported Missing so the developer is told what to set, but no error is
//     returned and no key is created;
//   - a declared secret not in the store yet → reported Missing, not injected,
//     so a repo can still run the tests that don't need it.
//
// Only a malformed/unreadable manifest or a corrupt store returns an error.
//
// Env values are secret material: callers must inject them into the child env
// only and never print or log them.
func InjectForRepo(repoRoot string) (Resolution, error) {
	return injectForRepoAt(repoRoot, paths.SecretsRoot())
}

// injectForRepoAt is the testable core of InjectForRepo: it resolves the
// manifest relative to repoRoot and the store under root.
func injectForRepoAt(repoRoot, root string) (Resolution, error) {
	m, err := readMapping(filepath.Join(repoRoot, MappingFile))
	if err != nil {
		return Resolution{}, err
	}
	if len(m.Secrets) == 0 {
		return Resolution{}, nil // no manifest → nothing to inject or warn about
	}

	// If the store/key isn't set up at all, report every declared secret as
	// missing rather than failing — the common case for a developer who hasn't
	// provisioned this service's secrets yet. No key is created.
	if _, statErr := os.Stat(filepath.Join(root, keyFile)); errors.Is(statErr, os.ErrNotExist) {
		return Resolution{Missing: append([]Declaration(nil), m.Secrets...)}, nil
	}

	st, err := openAt(root)
	if err != nil {
		return Resolution{}, err
	}

	res := Resolution{Env: make(map[string]string, len(m.Secrets))}
	for _, d := range m.Secrets {
		// A declared secret that isn't set yet is reported Missing, not an error
		// — so a repo with a manifest but an unprovisioned secret can still run
		// the tests that don't need it. The dependent code reports the missing
		// env var itself; the warning tells the developer how to set it.
		if value, ok := st.Get(d.ID); ok {
			res.Env[d.Env] = value
		} else {
			res.Missing = append(res.Missing, d)
		}
	}
	return res, nil
}

// readMapping parses the manifest at path. An absent file yields an empty
// manifest and no error (most repos declare none).
func readMapping(path string) (mapping, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return mapping{}, nil
	}
	if err != nil {
		return mapping{}, fmt.Errorf("secrets: read %s: %w", path, err)
	}
	// Decode strictly: an unknown top-level key — including the legacy `env:`
	// map shape — is a hard error rather than a silent no-op, so a mistyped or
	// outdated manifest fails loudly instead of injecting nothing.
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var m mapping
	if err := dec.Decode(&m); err != nil && !errors.Is(err, io.EOF) {
		return mapping{}, fmt.Errorf("secrets: parse %s: %w", path, err)
	}
	// Each declaration must name both the target env var and the secret id —
	// otherwise we'd inject under an empty name or look up an empty id. A
	// missing field is a malformed manifest, not a silently skipped entry.
	for i, d := range m.Secrets {
		if d.Env == "" || d.ID == "" {
			return mapping{}, fmt.Errorf("secrets: %s: entry %d must set both env and id", path, i)
		}
	}
	return m, nil
}
