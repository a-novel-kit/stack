package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
			runEnv, err := PrepareEnv(detect.Target{Kind: detect.KindGo, Name: "x", Dir: repoRoot}, &out)
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
// port variable. Referenced credentials get their local defaults. The test
// process reaches Postgres through the DSN and the discrete settings alike,
// both carrying the credentials Postgres receives, inherited ones included.
func TestComposeEnv(t *testing.T) {
	t.Parallel()

	inheritedPorts := []string{"POSTGRES_PORT=5432", "REST_PORT=8080"}
	inheritedCredentials := []string{"POSTGRES_USER=alice", "POSTGRES_PASSWORD=p@ss", "POSTGRES_DB=novels"}
	credentialRefs := []string{"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB"}
	// defaultPostgres is the connection with the local defaults, PORT standing
	// for the allocated port.
	defaultPostgres := map[string]string{
		"POSTGRES_DSN":         "postgres://postgres:postgres@localhost:PORT/postgres?sslmode=disable",
		"POSTGRES_HOST":        "localhost",
		"POSTGRES_PORT":        "PORT",
		"POSTGRES_USER":        "postgres",
		"POSTGRES_PASSWORD":    "postgres",
		"POSTGRES_DATABASE":    "postgres",
		"POSTGRES_TLS_ENABLED": "false",
	}

	testCases := []struct {
		name string
		base []string
		refs []string
		// wantPostgres is the connection the test process sees, base overlaid
		// with what composeEnv adds, with PORT standing for the allocated port.
		wantPostgres map[string]string
	}{
		{
			name:         "Success/DefaultCredentials",
			base:         inheritedPorts,
			refs:         credentialRefs,
			wantPostgres: defaultPostgres,
		},
		{
			name: "Success/InheritedCredentials",
			base: slices.Concat(inheritedPorts, inheritedCredentials),
			refs: credentialRefs,
			wantPostgres: map[string]string{
				"POSTGRES_DSN":         "postgres://alice:p%40ss@localhost:PORT/novels?sslmode=disable",
				"POSTGRES_HOST":        "localhost",
				"POSTGRES_PORT":        "PORT",
				"POSTGRES_USER":        "alice",
				"POSTGRES_PASSWORD":    "p@ss",
				"POSTGRES_DATABASE":    "novels",
				"POSTGRES_TLS_ENABLED": "false",
			},
		},
		{
			// Discrete settings inherited from the shell name another database, and
			// the test database's replace them.
			name: "Success/InheritedDiscreteSettings",
			base: slices.Concat(inheritedPorts,
				[]string{"POSTGRES_HOST=db.example", "POSTGRES_DATABASE=other", "POSTGRES_TLS_ENABLED=true"}),
			refs:         credentialRefs,
			wantPostgres: defaultPostgres,
		},
		{
			// A compose file that references no credentials still gets a discrete
			// login matching the DSN's defaults.
			name:         "Success/UnreferencedCredentials",
			base:         inheritedPorts,
			wantPostgres: defaultPostgres,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			env := &detect.ComposeEnv{Ports: []string{"POSTGRES_PORT", "REST_PORT"}, Refs: testCase.refs}
			added, err := composeEnv(env, testCase.base)
			if err != nil {
				t.Fatalf("composeEnv: %v", err)
			}
			vars := map[string]string{}
			for _, kv := range added {
				key, value, _ := strings.Cut(kv, "=")
				if _, listed := vars[key]; listed {
					t.Errorf("composeEnv lists %s twice: %v", key, added)
				}
				vars[key] = value
			}
			if vars["POSTGRES_PORT"] == "5432" || vars["REST_PORT"] == "8080" {
				t.Fatalf("composeEnv kept an inherited port: %v", added)
			}
			if want := "http://localhost:" + vars["REST_PORT"]; vars["REST_URL"] != want {
				t.Errorf("REST_URL = %q, want %q", vars["REST_URL"], want)
			}
			runEnv := slices.Concat(testCase.base, added)
			for key, want := range testCase.wantPostgres {
				if want = strings.ReplaceAll(want, "PORT", vars["POSTGRES_PORT"]); envVal(runEnv, key) != want {
					t.Errorf("%s = %q, want %q", key, envVal(runEnv, key), want)
				}
			}
		})
	}
}

// cancelOnWrite cancels a context on the first write it receives.
type cancelOnWrite struct{ cancel context.CancelFunc }

func (w cancelOnWrite) Write(p []byte) (int, error) {
	w.cancel()
	return len(p), nil
}

// TestCompose pins that a cancelled compose returns at once, even while a
// child of podman holds its output pipe, as podman-compose's children do.
//
// Not parallel: t.Setenv puts a stand-in podman first on PATH.
func TestCompose(t *testing.T) {
	bin := t.TempDir()
	podman := "#!/bin/sh\nsleep 30 &\necho started\nwait\n"
	if err := os.WriteFile(filepath.Join(bin, "podman"), []byte(podman), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// The first output line proves the background sleep runs, so the cancel
	// lands while it holds the pipe.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	start := time.Now()
	err := compose(ctx, cancelOnWrite{cancel}, os.Environ(), &detect.ComposeEnv{Project: "p", File: "f"}, nil, "up")
	if err == nil {
		t.Fatal("compose succeeded after its context was cancelled")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("compose returned %s after the cancel: a child of podman outlived it", elapsed)
	}
}

// TestAllocPorts pins that one call never hands two vars the same port.
func TestAllocPorts(t *testing.T) {
	t.Parallel()

	names := make([]string, 64)
	for i := range names {
		names[i] = fmt.Sprintf("PORT_%d", i)
	}
	entries, err := allocPorts(names)
	if err != nil {
		t.Fatalf("allocPorts: %v", err)
	}
	seen := map[string]string{}
	for i, entry := range entries {
		name, port, _ := strings.Cut(entry, "=")
		if name != names[i] {
			t.Fatalf("entry %d = %q, want var %s", i, entry, names[i])
		}
		if other, ok := seen[port]; ok {
			t.Fatalf("%s and %s share port %s", other, name, port)
		}
		seen[port] = name
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
		{name: "Success/CapsWhenPositive", maxProcs: 3, want: "3"},
		{name: "Success/UntouchedWhenZero", maxProcs: 0, want: ""},
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
