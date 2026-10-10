package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/detect"
	"github.com/a-novel-kit/stack/cli/internal/secrets"
)

// TestPrepareEnv verifies the test/build env-assembly seam: PrepareEnv appends
// the repo's locally-stored secrets (decrypted) to the child env. The value must
// appear only in the returned env — never in the progress written to `out`. A
// repo without a mapping injects nothing and does not fail, and a
// declared-but-unset secret is not injected, does not fail the build, and
// surfaces a value-free warning so the developer knows what to set.
//
// Not parallel: uses t.Setenv("XDG_DATA_HOME", ...) for store isolation.
func TestPrepareEnv(t *testing.T) {
	const manifest = "secrets:\n  - env: OPENAI_API_KEY\n    id: openai-key\n    description: used by generation\n"
	cases := []struct {
		name     string
		secret   string // stored as openai-key; empty: never set
		manifest string // empty: no .a-novel/secrets.yaml
		wantEnv  string // the OPENAI_API_KEY entry, empty when none
		outHas   []string
	}{
		{name: "InjectsRepoSecrets", secret: "sk-prepare-env", manifest: manifest, wantEnv: "OPENAI_API_KEY=sk-prepare-env"},
		{name: "NoMappingNoSecrets"},
		{
			name: "MissingSecretWarns", manifest: manifest,
			outHas: []string{"OPENAI_API_KEY", "openai-key", "used by generation", "a-novel secrets set openai-key"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			if c.secret != "" {
				st, err := secrets.Open()
				if err != nil {
					t.Fatalf("open store: %v", err)
				}
				st.Set("openai-key", c.secret)
				if err := st.Save(); err != nil {
					t.Fatalf("save store: %v", err)
				}
			}
			// With no compose Env, repoRoot(t) == t.Dir, which is this directory.
			repoRoot := t.TempDir()
			if c.manifest != "" {
				if err := os.MkdirAll(filepath.Join(repoRoot, ".a-novel"), 0o755); err != nil {
					t.Fatalf("mkdir mapping: %v", err)
				}
				if err := os.WriteFile(filepath.Join(repoRoot, ".a-novel", "secrets.yaml"), []byte(c.manifest), 0o600); err != nil {
					t.Fatalf("write mapping: %v", err)
				}
			}

			var out strings.Builder
			runEnv, err := PrepareEnv(t.Context(), detect.Target{Kind: detect.KindGo, Name: "x", Dir: repoRoot}, &out)
			if err != nil {
				t.Fatalf("PrepareEnv: %v", err)
			}
			var got string
			for _, e := range runEnv {
				if strings.HasPrefix(e, "OPENAI_API_KEY=") {
					got = e
				}
			}
			if got != c.wantEnv {
				t.Errorf("runEnv OPENAI_API_KEY entry = %q, want %q", got, c.wantEnv)
			}
			if c.secret != "" && strings.Contains(out.String(), c.secret) {
				t.Errorf("PrepareEnv progress output leaked the secret value:\n%s", out.String())
			}
			for _, want := range c.outHas {
				if !strings.Contains(out.String(), want) {
					t.Errorf("expected a descriptive missing-secret warning naming %q, got:\n%s", want, out.String())
				}
			}
		})
	}
}

// TestJob_GOMAXPROCSCap verifies a job injects GOMAXPROCS from its maxProcs
// argument (the per-target CPU cap the parallel runner uses), and leaves it
// untouched when maxProcs is 0 (the sequential path).
func TestJob_GOMAXPROCSCap(t *testing.T) {
	// The child echoes its inherited GOMAXPROCS; ensure it isn't already set in
	// this process, so we observe only what the job injects.
	if orig, ok := os.LookupEnv("GOMAXPROCS"); ok {
		_ = os.Unsetenv("GOMAXPROCS")
		t.Cleanup(func() { _ = os.Setenv("GOMAXPROCS", orig) })
	}

	tests := []struct {
		name     string
		maxProcs int
		want     string // expected value of $GOMAXPROCS in the child ("" = unset)
	}{
		{name: "caps when positive", maxProcs: 3, want: "3"},
		{name: "untouched when zero", maxProcs: 0, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tgt := detect.Target{
				Kind: detect.KindGo,
				Name: "echo-gomaxprocs",
				Dir:  t.TempDir(),
				Cmd:  "sh",
				Args: []string{"-c", `printf 'GMP=[%s]' "$GOMAXPROCS"`},
			}
			var out strings.Builder
			if _, err := Job(tgt, 0, tt.maxProcs, false).Run(t.Context(), &out); err != nil {
				t.Fatalf("target failed: %v\n%s", err, out.String())
			}
			if want := "GMP=[" + tt.want + "]"; !strings.Contains(out.String(), want) {
				t.Errorf("output %q does not contain %q", out.String(), want)
			}
		})
	}
}
