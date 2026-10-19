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
	testCases := []struct {
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
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			if testCase.secret != "" {
				st, err := secrets.Open()
				if err != nil {
					t.Fatalf("open store: %v", err)
				}
				st.Set("openai-key", testCase.secret)
				if err := st.Save(); err != nil {
					t.Fatalf("save store: %v", err)
				}
			}
			// With no compose Env, repoRoot(t) == t.Dir, which is this directory.
			repoRoot := t.TempDir()
			if testCase.manifest != "" {
				if err := os.MkdirAll(filepath.Join(repoRoot, ".a-novel"), 0o755); err != nil {
					t.Fatalf("mkdir mapping: %v", err)
				}
				if err := os.WriteFile(filepath.Join(repoRoot, ".a-novel", "secrets.yaml"), []byte(testCase.manifest), 0o600); err != nil {
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
			if got != testCase.wantEnv {
				t.Errorf("runEnv OPENAI_API_KEY entry = %q, want %q", got, testCase.wantEnv)
			}
			if testCase.secret != "" && strings.Contains(out.String(), testCase.secret) {
				t.Errorf("PrepareEnv progress output leaked the secret value:\n%s", out.String())
			}
			for _, want := range testCase.outHas {
				if !strings.Contains(out.String(), want) {
					t.Errorf("expected a descriptive missing-secret warning naming %q, got:\n%s", want, out.String())
				}
			}
		})
	}
}

// TestComposeEnv pins that the derived connection vars follow the ports the
// run allocates, even when the inherited environment already sets the same
// port variable, and that referenced credentials get their local defaults.
func TestComposeEnv(t *testing.T) {
	t.Parallel()

	env := &detect.ComposeEnv{Ports: []string{"POSTGRES_PORT", "REST_PORT"}, Refs: []string{"POSTGRES_USER"}}
	added, err := composeEnv(env, []string{"POSTGRES_PORT=5432", "REST_PORT=8080"})
	if err != nil {
		t.Fatalf("composeEnv: %v", err)
	}
	vars := map[string]string{}
	for _, kv := range added {
		key, value, _ := strings.Cut(kv, "=")
		vars[key] = value
	}
	if vars["POSTGRES_PORT"] == "5432" || vars["REST_PORT"] == "8080" {
		t.Fatalf("composeEnv kept an inherited port: %v", added)
	}
	wantDSN := "postgres://postgres:postgres@localhost:" + vars["POSTGRES_PORT"] + "/postgres?sslmode=disable"
	if vars["POSTGRES_DSN"] != wantDSN {
		t.Errorf("POSTGRES_DSN = %q, want %q", vars["POSTGRES_DSN"], wantDSN)
	}
	if want := "http://localhost:" + vars["REST_PORT"]; vars["REST_URL"] != want {
		t.Errorf("REST_URL = %q, want %q", vars["REST_URL"], want)
	}
	if vars["POSTGRES_USER"] != "postgres" {
		t.Errorf("POSTGRES_USER = %q, want the local default", vars["POSTGRES_USER"])
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

	testCases := []struct {
		name     string
		maxProcs int
		want     string // expected value of $GOMAXPROCS in the child ("" = unset)
	}{
		{name: "caps when positive", maxProcs: 3, want: "3"},
		{name: "untouched when zero", maxProcs: 0, want: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tgt := detect.Target{
				Kind: detect.KindGo,
				Name: "echo-gomaxprocs",
				Dir:  t.TempDir(),
				Cmd:  "sh",
				Args: []string{"-c", `printf 'GMP=[%s]' "$GOMAXPROCS"`},
			}
			var out strings.Builder
			if _, err := Job(tgt, 0, testCase.maxProcs, false).Run(t.Context(), &out); err != nil {
				t.Fatalf("target failed: %v\n%s", err, out.String())
			}
			if want := "GMP=[" + testCase.want + "]"; !strings.Contains(out.String(), want) {
				t.Errorf("output %q does not contain %q", out.String(), want)
			}
		})
	}
}
