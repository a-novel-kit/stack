package secrets_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/secrets"
)

// TestInjectForRepo covers the value-free manifest → Resolution flow: the
// set-secret (injected), unset-secret (reported missing), absent-store (all
// missing), absent-manifest, and malformed-manifest cases.
//
// Not parallel: uses t.Setenv("XDG_DATA_HOME", ...) for store isolation.
func TestInjectForRepo(t *testing.T) {
	cases := []struct {
		name        string
		store       map[string]string // nil: never opened, so no key or store exists
		manifest    string            // empty: the repo has no .a-novel/secrets.yaml
		wantEnv     map[string]string
		wantMissing []secrets.Declaration
		wantErr     bool
	}{
		{
			name:  "ManifestResolvesSecrets",
			store: map[string]string{"openai-key": "sk-live", "anthropic-key": "ak-live"},
			manifest: "secrets:\n" +
				"  - env: OPENAI_API_KEY\n    id: openai-key\n" +
				"  - env: ANTHROPIC_API_KEY\n    id: anthropic-key\n",
			wantEnv: map[string]string{"OPENAI_API_KEY": "sk-live", "ANTHROPIC_API_KEY": "ak-live"},
		},
		{name: "AbsentManifestReturnsEmpty"},
		{
			// A manifest that references secrets must NOT fail without a key or
			// store: every declared secret is reported missing so the dev knows
			// what to set.
			name:        "AbsentStoreReportsAllMissing",
			manifest:    "secrets:\n  - env: OPENAI_API_KEY\n    id: openai-key\n",
			wantMissing: []secrets.Declaration{{Env: "OPENAI_API_KEY", ID: "openai-key"}},
		},
		{
			// A secret not in the store must NOT fail or block injection: the unset
			// one is reported missing (with its description), the set ones still
			// inject.
			name:  "UnsetSecretIsReportedMissing",
			store: map[string]string{"present": "v"},
			manifest: "secrets:\n" +
				"  - env: PRESENT_VAR\n    id: present\n" +
				"  - env: MISSING_VAR\n    id: not-in-store\n    description: needed for X\n",
			wantEnv:     map[string]string{"PRESENT_VAR": "v"},
			wantMissing: []secrets.Declaration{{Env: "MISSING_VAR", ID: "not-in-store", Description: "needed for X"}},
		},
		{name: "EmptySecretsBlockReturnsEmpty", manifest: "secrets: []\n"},
		{name: "Error/MalformedManifest", manifest: "secrets: [this is not: valid: yaml\n", wantErr: true},
		// The legacy top-level `env:` map shape (pre-list) is an unknown field under
		// strict decoding — it must error loudly, not silently inject nothing.
		{name: "Error/LegacyEnvShape", manifest: "env:\n  OPENAI_API_KEY: openai-key\n", wantErr: true},
		// A declaration without an id (or env) is malformed — it must error, not be
		// silently skipped.
		{name: "Error/MissingRequiredField", manifest: "secrets:\n  - env: OPENAI_API_KEY\n", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			if c.store != nil {
				saveStore(t, c.store)
			}
			repoRoot := t.TempDir()
			if c.manifest != "" {
				dir := filepath.Join(repoRoot, ".a-novel")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					panic(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "secrets.yaml"), []byte(c.manifest), 0o600); err != nil {
					panic(err)
				}
			}

			res, err := secrets.InjectForRepo(repoRoot)
			if (err != nil) != c.wantErr {
				t.Fatalf("inject: got err %v, want error %v", err, c.wantErr)
			}
			if !maps.Equal(res.Env, c.wantEnv) || !slices.Equal(res.Missing, c.wantMissing) {
				t.Errorf("resolution = env %v, missing %+v; want env %v, missing %+v",
					res.Env, res.Missing, c.wantEnv, c.wantMissing)
			}
		})
	}
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
	for i, wants := range [][]string{
		{"OPENAI_API_KEY", "openai-key", "used by generation", "a-novel secrets set openai-key"},
		// A declaration without a description still names the env, id, and the fix.
		{"BARE_VAR", "bare-id", "a-novel secrets set bare-id"},
	} {
		for _, want := range wants {
			if !strings.Contains(w[i], want) {
				t.Errorf("warning %d %q lacks %q", i, w[i], want)
			}
		}
	}

	// An empty Resolution yields no warnings.
	if got := (secrets.Resolution{}).Warnings(); got != nil {
		t.Errorf("empty Resolution should produce no warnings, got %v", got)
	}
}
