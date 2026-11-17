package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/detect"
)

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
			require.NoError(t, err)
			vars := map[string]string{}
			for _, kv := range added {
				key, value, _ := strings.Cut(kv, "=")
				require.NotContains(t, vars, key, "composeEnv lists %s twice: %v", key, added)
				vars[key] = value
			}
			require.NotEqual(t, "5432", vars["POSTGRES_PORT"], "composeEnv kept an inherited port: %v", added)
			require.NotEqual(t, "8080", vars["REST_PORT"], "composeEnv kept an inherited port: %v", added)
			require.Equal(t, "http://localhost:"+vars["REST_PORT"], vars["REST_URL"])
			runEnv := slices.Concat(testCase.base, added)
			for key, want := range testCase.wantPostgres {
				require.Equal(t, strings.ReplaceAll(want, "PORT", vars["POSTGRES_PORT"]), envVal(runEnv, key), key)
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
	require.NoError(t, os.WriteFile(filepath.Join(bin, "podman"), []byte(podman), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// The first output line proves the background sleep runs, so the cancel
	// lands while it holds the pipe.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	start := time.Now()
	err := compose(ctx, cancelOnWrite{cancel}, os.Environ(), &detect.ComposeEnv{Project: "p", File: "f"}, nil, "up")
	require.Error(t, err, "compose succeeded after its context was cancelled")
	require.LessOrEqual(t, time.Since(start), 5*time.Second, "a child of podman outlived the cancel")
}

// TestRunCommand pins that a cancelled target returns once its wait delay
// passes, even while a process that left its group holds the output pipe.
func TestRunCommand(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// The detached shell writes the first line once it has left the group, so
	// the cancel lands while it holds the pipe.
	tgt := detect.Target{Dir: t.TempDir(), Cmd: "sh", Args: []string{"-c", "setsid sh -c 'echo started; exec sleep 20' &\nwait\n"}}
	start := time.Now()
	err := runCommand(ctx, tgt, os.Environ(), cancelOnWrite{cancel})
	require.Error(t, err, "runCommand succeeded after its context was cancelled")
	require.LessOrEqual(t, time.Since(start), 15*time.Second, "a detached child held runCommand")
}

// TestEnvDown pins that teardown untags each image the env built by its exact
// name. A bare `podman untag <image>` would also strip other checkouts' tags
// on the same image.
//
// Not parallel: t.Setenv puts a stand-in podman first on PATH.
func TestEnvDown(t *testing.T) {
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	podman := fmt.Sprintf("#!/bin/sh\necho \"$*\" >> %q\n", calls)
	require.NoError(t, os.WriteFile(filepath.Join(bin, "podman"), []byte(podman), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	env := &detect.ComposeEnv{Project: "p", File: "f", Images: []string{"localhost/p_a:latest", "localhost/p_b:latest"}}
	envDown(t.Context(), env, os.Environ(), io.Discard)

	got, err := os.ReadFile(calls)
	require.NoError(t, err)
	//nolint:dupword // untag names each tag twice to drop that tag alone
	want := "compose -p p -f f down --volume\n" +
		"untag localhost/p_a:latest localhost/p_a:latest\n" +
		"untag localhost/p_b:latest localhost/p_b:latest\n"
	require.Equal(t, want, string(got))
}

// TestAllocPorts pins that one call never hands two vars the same port.
func TestAllocPorts(t *testing.T) {
	t.Parallel()

	names := make([]string, 64)
	for i := range names {
		names[i] = fmt.Sprintf("PORT_%d", i)
	}
	entries, err := allocPorts(names)
	require.NoError(t, err)
	seen := map[string]string{}
	for i, entry := range entries {
		name, port, _ := strings.Cut(entry, "=")
		require.Equal(t, names[i], name, "entry %d = %q", i, entry)
		require.NotContains(t, seen, port, "%s reuses the port of %s", name, seen[port])
		seen[port] = name
	}
}
