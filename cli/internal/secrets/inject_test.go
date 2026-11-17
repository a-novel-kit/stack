package secrets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/secrets"
	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
)

// TestInjectForRepo covers the value-free manifest → Resolution flow: the
// set-secret (injected), unset-secret (reported missing), absent-store (all
// missing), absent-manifest, and malformed-manifest cases.
//
// Not parallel: every sub-test sets XDG_DATA_HOME to isolate the store.
func TestInjectForRepo(t *testing.T) {
	testCases := []struct {
		name        string
		store       map[string]string // nil: never opened, so no key or store exists
		manifest    string            // empty: the repo has no .a-novel/secrets.yaml
		wantEnv     map[string]string
		wantMissing []secrets.Declaration
		wantErr     bool
	}{
		{
			name:  "Success/ManifestResolvesSecrets",
			store: map[string]string{"openai-key": "sk-live", "anthropic-key": "ak-live"},
			manifest: "secrets:\n" +
				"  - env: OPENAI_API_KEY\n    id: openai-key\n" +
				"  - env: ANTHROPIC_API_KEY\n    id: anthropic-key\n",
			wantEnv: map[string]string{"OPENAI_API_KEY": "sk-live", "ANTHROPIC_API_KEY": "ak-live"},
		},
		{name: "Success/AbsentManifestReturnsEmpty"},
		{
			// A manifest that references secrets must NOT fail without a key or
			// store: every declared secret is reported missing so the dev knows
			// what to set.
			name:        "Success/AbsentStoreReportsAllMissing",
			manifest:    "secrets:\n  - env: OPENAI_API_KEY\n    id: openai-key\n",
			wantMissing: []secrets.Declaration{{Env: "OPENAI_API_KEY", ID: "openai-key"}},
		},
		{
			// A secret not in the store must NOT fail or block injection: the unset
			// one is reported missing (with its description), the set ones still
			// inject.
			name:  "Success/UnsetSecretIsReportedMissing",
			store: map[string]string{"present": "v"},
			manifest: "secrets:\n" +
				"  - env: PRESENT_VAR\n    id: present\n" +
				"  - env: MISSING_VAR\n    id: not-in-store\n    description: needed for X\n",
			wantEnv:     map[string]string{"PRESENT_VAR": "v"},
			wantMissing: []secrets.Declaration{{Env: "MISSING_VAR", ID: "not-in-store", Description: "needed for X"}},
		},
		{name: "Success/EmptySecretsBlockReturnsEmpty", manifest: "secrets: []\n"},
		{name: "Error/MalformedManifest", manifest: "secrets: [this is not: valid: yaml\n", wantErr: true},
		// A top-level `env:` map is an unknown field under strict decoding, so it
		// fails rather than injecting nothing.
		{name: "Error/LegacyEnvShape", manifest: "env:\n  OPENAI_API_KEY: openai-key\n", wantErr: true},
		// A declaration without an id (or env) is malformed: it must error, not be
		// silently skipped.
		{name: "Error/MissingRequiredField", manifest: "secrets:\n  - env: OPENAI_API_KEY\n", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			if testCase.store != nil {
				saveStore(t, testCase.store)
			}
			repoRoot := t.TempDir()
			if testCase.manifest != "" {
				fsutiltest.Write(t, repoRoot, map[string]string{".a-novel/secrets.yaml": testCase.manifest})
			}

			res, err := secrets.InjectForRepo(repoRoot)
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.wantEnv, res.Env)
			require.Equal(t, testCase.wantMissing, res.Missing)
		})
	}
}

// TestResolution asserts the warning lines are actionable, include the
// optional description when present, and never carry a secret value.
func TestResolution(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		res := secrets.Resolution{
			Missing: []secrets.Declaration{
				{Env: "OPENAI_API_KEY", ID: "openai-key", Description: "used by generation"},
				{Env: "BARE_VAR", ID: "bare-id"}, // no description
			},
		}
		w := res.Warnings()
		require.Len(t, w, 2)
		wantWarnings := [][]string{
			{"OPENAI_API_KEY", "openai-key", "used by generation", "a-novel secrets set openai-key"},
			// A declaration without a description still names the env, id, and the fix.
			{"BARE_VAR", "bare-id", "a-novel secrets set bare-id"},
		}

		for i, wants := range wantWarnings {
			for _, want := range wants {
				require.Contains(t, w[i], want, "warning %d", i)
			}
		}
	})

	// An empty Resolution yields no warnings.
	t.Run("Success/NoMissing", func(t *testing.T) {
		t.Parallel()

		require.Nil(t, secrets.Resolution{}.Warnings())
	})
}
