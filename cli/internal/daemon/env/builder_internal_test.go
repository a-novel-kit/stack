package env

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/secrets"
	"github.com/a-novel-kit/stack/cli/internal/workspace/workspacetest"
)

// toMap collapses the sorted entry slice into a map keyed by variable name,
// which is easier to assert against than a positional slice.
func toMap(entries []Entry) map[string]string {
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		out[e.Key] = e.Value
	}
	return out
}

// TestBuilderForTarget exercises refs.go and allocator.go together against the
// compose-env wiring rules a service depends on at runtime. Each case builds a
// minimal in-memory discovery.Target, touching neither the filesystem nor
// podman, and asserts on the resulting env map.
//
//nolint:paralleltest // swaps the package-level injectSecrets seam.
func TestBuilderForTarget(t *testing.T) {
	// serviceRoot resolves only from a non-empty CmdDir, which gates the
	// secrets injection.
	cmdDir := filepath.Join("/tmp", "service-svc", "cmd", "rest")
	// hostPostgres is the connection a host process gets to svc's postgres infra
	// with the default credentials, PORT standing for the allocated port.
	hostPostgres := map[string]string{
		"POSTGRES_DSN":         "postgres://postgres:postgres@localhost:PORT/postgres?sslmode=disable",
		"POSTGRES_HOST":        "localhost",
		"POSTGRES_PORT":        "PORT",
		"POSTGRES_USER":        "postgres",
		"POSTGRES_PASSWORD":    "postgres",
		"POSTGRES_DATABASE":    "postgres",
		"POSTGRES_TLS_ENABLED": "false",
	}

	testCases := []struct {
		name      string
		services  []string
		infraPort string // acquired for the target's service first, as infra-up does
		target    discovery.Target
		secrets   *secrets.Resolution // nil: injectSecrets must not be called
		// builds runs ForTarget this many times, 0 meaning once, so a result that
		// depends on map order shows.
		builds int
		// wantEnv holds entries the env must carry, "" for an unset one, with PORT
		// standing for the service's allocated POSTGRES_PORT.
		wantEnv map[string]string
		check   func(t *testing.T, env map[string]string, warnings []string, alloc *Allocator)
	}{
		{
			// Compose declares a literal in-container DSN (`postgres-X:5432`), and the
			// service's POSTGRES_PORT allocation overrides it to `localhost:<port>`.
			// Without that rewrite, go-exec mode cannot reach its own postgres. The
			// discrete settings describe the same login, since a service prefers them
			// once POSTGRES_HOST is set.
			name: "Success/DSNRewriteForGoExec", services: []string{"svc"}, infraPort: "POSTGRES_PORT",
			target: discovery.Target{Name: "migrations", Service: "svc", Environment: map[string]string{
				"POSTGRES_DSN": "postgres://postgres:postgres@postgres-svc:5432/postgres?sslmode=disable",
			}},
			wantEnv: hostPostgres,
		},
		{
			// The rewritten DSN carries the compose credentials percent-escaped, so a
			// password holding `@` or `/` still parses. The discrete settings carry
			// them as they are.
			name: "Success/DSNEscapesCredentials", services: []string{"svc"}, infraPort: "POSTGRES_PORT",
			target: discovery.Target{Name: "migrations", Service: "svc", Environment: map[string]string{
				"POSTGRES_USER":     "alice",
				"POSTGRES_PASSWORD": "p@ss/word",
				"POSTGRES_DB":       "novels",
			}},
			wantEnv: map[string]string{
				"POSTGRES_DSN":      "postgres://alice:p%40ss%2Fword@localhost:PORT/novels?sslmode=disable",
				"POSTGRES_USER":     "alice",
				"POSTGRES_PASSWORD": "p@ss/word",
				"POSTGRES_DATABASE": "novels",
			},
		},
		{
			// A target on the discrete settings names the in-network host, which go-exec
			// cannot resolve. Its database and its TLS choice stay, and the DSN names
			// the same database.
			name: "Success/DiscreteRewriteForGoExec", services: []string{"svc"}, infraPort: "POSTGRES_PORT",
			target: discovery.Target{Name: "rest", Service: "svc", Environment: map[string]string{
				"POSTGRES_HOST":        "postgres-svc",
				"POSTGRES_USER":        "postgres",
				"POSTGRES_PASSWORD":    "postgres",
				"POSTGRES_DATABASE":    "novels",
				"POSTGRES_TLS_ENABLED": "true",
			}},
			wantEnv: map[string]string{
				"POSTGRES_DSN":         "postgres://postgres:postgres@localhost:PORT/novels?sslmode=disable",
				"POSTGRES_HOST":        "localhost",
				"POSTGRES_PORT":        "PORT",
				"POSTGRES_DATABASE":    "novels",
				"POSTGRES_TLS_ENABLED": "true",
			},
		},
		{
			// A target's own POSTGRES_PORT is the database's port inside the compose
			// network, so the service's host-port allocation replaces it.
			name: "Success/InNetworkPortYieldsToAllocation", services: []string{"svc"}, infraPort: "POSTGRES_PORT",
			target: discovery.Target{Name: "rest", Service: "svc", Environment: map[string]string{
				"POSTGRES_HOST": "postgres-svc",
				"POSTGRES_PORT": "5432",
			}},
			wantEnv: hostPostgres,
		},
		{
			// A setting that names itself with a default, so the operator can override
			// it, takes the default in go-exec as in container mode.
			name: "Success/SelfReferenceTakesDefault", services: []string{"svc"},
			target: discovery.Target{Name: "grpc", Service: "svc", Environment: map[string]string{
				"OPENAI_API_KEY": "${OPENAI_API_KEY:-placeholder}",
			}},
			wantEnv: map[string]string{"OPENAI_API_KEY": "placeholder"},
		},
		{
			// Without the allocation, which infra-up makes, nothing listens on the host,
			// and the compose values stay.
			name: "Success/NoAllocationKeepsCompose", services: []string{"svc"},
			target: discovery.Target{Name: "rest", Service: "svc", Environment: map[string]string{
				"POSTGRES_HOST": "postgres-svc",
				"POSTGRES_DSN":  "postgres://postgres:postgres@postgres-svc:5432/postgres?sslmode=disable",
			}},
			wantEnv: map[string]string{
				"POSTGRES_DSN":         "postgres://postgres:postgres@postgres-svc:5432/postgres?sslmode=disable",
				"POSTGRES_HOST":        "postgres-svc",
				"POSTGRES_PORT":        "",
				"POSTGRES_TLS_ENABLED": "",
			},
		},
		{
			// ${SERVICE_X_GRPC_PORT} resolves to an allocation against service-x's
			// grpc target, recorded so a later service-x/grpc target lands on the
			// same slot. "service-x" yields the prefix "SERVICE_X", which
			// resolveOwner must reverse.
			name: "Success/CrossServiceRef", services: []string{"service-x", "service-y"},
			target: discovery.Target{Name: "rest", Service: "service-y", Environment: map[string]string{
				"DEP_HOST": "${SERVICE_X_GRPC_HOST}",
				"DEP_PORT": "${SERVICE_X_GRPC_PORT}",
			}},
			wantEnv: map[string]string{"DEP_HOST": "localhost"},
			check: func(t *testing.T, env map[string]string, _ []string, alloc *Allocator) {
				t.Helper()
				port, ok := alloc.Lookup("service-x", "GRPC_PORT")
				require.True(t, ok)
				require.NotZero(t, port)
				require.Equal(t, strconv.Itoa(port), env["DEP_PORT"])
			},
		},
		{
			// Compose's `ports:` block is the only signal to allocate a port the
			// `environment:` block never references; mergePortRefs folds it into the
			// same resolution pass.
			name: "Success/PortsBlockTriggersAllocation", services: []string{"svc"},
			target:  discovery.Target{Name: "rest", Service: "svc", Ports: []string{"${REST_PORT}:8080"}},
			wantEnv: map[string]string{"REST_HOST": "localhost"},
			check: func(t *testing.T, env map[string]string, _ []string, _ *Allocator) {
				t.Helper()
				require.NotEmpty(t, env["REST_PORT"])
				require.Equal(t, "http://localhost:"+env["REST_PORT"], env["REST_URL"])
			},
		},
		{
			// A target's own service prefix is stripped for its process env, so it
			// sees both the local `REST_PORT` and the cross-service
			// `SERVICE_FOO_REST_PORT`, resolving to one number.
			name: "Success/PrefixedAndUnprefixedOwnView", services: []string{"service-foo"},
			target: discovery.Target{Name: "rest", Service: "service-foo", Ports: []string{"${REST_PORT}:8080"}},
			check: func(t *testing.T, env map[string]string, _ []string, _ *Allocator) {
				t.Helper()
				require.NotEmpty(t, env["REST_PORT"])
				require.Equal(t, env["REST_PORT"], env["SERVICE_FOO_REST_PORT"])
			},
		},
		{
			// An unguarded un-prefix and re-prefix pair yields
			// SERVICE_FOO_SERVICE_BAR_GRPC_PORT, so the own-prefix view must skip
			// already-prefixed cross-service keys.
			name: "Success/NoDoublePrefix", services: []string{"service-foo", "service-bar"},
			target: discovery.Target{Name: "rest", Service: "service-foo", Environment: map[string]string{
				"DEP_PORT": "${SERVICE_BAR_GRPC_PORT}",
			}},
			check: func(t *testing.T, env map[string]string, _ []string, _ *Allocator) {
				t.Helper()
				for k := range env {
					require.NotContains(t, k, "SERVICE_FOO_SERVICE_BAR_")
				}
			},
		},
		{
			// isAllocatedKind matches a key ending in _PORT, so a var literally
			// named PORT stays a constant and allocates nothing.
			name: "Success/PORTAloneDoesNotAllocate", services: []string{"svc"},
			target:  discovery.Target{Name: "weird", Service: "svc", Environment: map[string]string{"PORT": "9999"}},
			wantEnv: map[string]string{"PORT": "9999"},
			check: func(t *testing.T, _ map[string]string, _ []string, alloc *Allocator) {
				t.Helper()
				require.Empty(t, alloc.Snapshot())
			},
		},
		{
			// A URL beside its port carries that port.
			name: "Success/URLBesideItsPort", services: []string{"svc"}, builds: 50,
			target: discovery.Target{Name: "rest", Service: "svc", Environment: map[string]string{
				"A": "${REST_URL}",
				"B": "${REST_PORT}",
			}},
			check: func(t *testing.T, env map[string]string, _ []string, _ *Allocator) {
				t.Helper()
				require.Equal(t, "http://localhost:"+env["B"], env["A"])
			},
		},
		{
			name: "Success/CrossServiceURLBesideItsPort", services: []string{"svc", "service-y"}, builds: 50,
			target: discovery.Target{Name: "rest", Service: "svc", Environment: map[string]string{
				"A": "${SERVICE_Y_GRPC_URL}",
				"B": "${SERVICE_Y_GRPC_PORT}",
			}},
			check: func(t *testing.T, env map[string]string, _ []string, _ *Allocator) {
				t.Helper()
				require.Equal(t, "localhost:"+env["B"], env["A"])
			},
		},
		{
			// A cross-service URL never carries the consumer's own port.
			name: "Success/CrossServiceURLBesideOwnPort", services: []string{"svc", "service-y"}, builds: 50,
			target: discovery.Target{Name: "rest", Service: "svc", Environment: map[string]string{
				"A": "${SERVICE_Y_GRPC_URL}",
				"B": "${GRPC_PORT}",
			}},
			wantEnv: map[string]string{"A": ""},
		},
		{
			// The repo's decrypted secrets ride into the spawned process's cmd.Env
			// as plain entries.
			name: "Success/InjectsRepoSecrets", services: []string{"svc"},
			target:  discovery.Target{Name: "rest", Service: "svc", CmdDir: cmdDir},
			secrets: &secrets.Resolution{Env: map[string]string{"OPENAI_API_KEY": "sk-test"}},
			wantEnv: map[string]string{"OPENAI_API_KEY": "sk-test"},
		},
		{
			// Without a CmdDir, serviceRoot is empty and the injection is skipped,
			// so no relative path is read by accident.
			name: "Success/NoCmdDirSkipsInjection", services: []string{"svc"},
			target: discovery.Target{Name: "rest", Service: "svc"},
		},
		{
			// A declared-but-unset secret stays out of the env and raises one
			// value-free warning line naming what to set.
			name: "Success/MissingSecretWarns", services: []string{"svc"},
			target: discovery.Target{Name: "rest", Service: "svc", CmdDir: cmdDir},
			secrets: &secrets.Resolution{Missing: []secrets.Declaration{
				{Env: "OPENAI_API_KEY", ID: "openai-key", Description: "used by generation"},
			}},
			check: func(t *testing.T, env map[string]string, warnings []string, _ *Allocator) {
				t.Helper()
				require.NotContains(t, env, "OPENAI_API_KEY")
				require.Len(t, warnings, 1)
				for _, want := range []string{"OPENAI_API_KEY", "openai-key", "used by generation", "a-novel secrets set openai-key"} {
					require.Contains(t, warnings[0], want)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			workspacetest.Swap(t, &injectSecrets, func(string) (secrets.Resolution, error) {
				require.NotNil(t, testCase.secrets, "injectSecrets must not be called")
				return *testCase.secrets, nil
			})
			alloc := NewAllocator()
			alloc.SetServices(testCase.services)
			b := NewBuilder(alloc)
			if testCase.infraPort != "" {
				_, err := alloc.Acquire(testCase.target.Service, testCase.infraPort, "svc-infra")
				require.NoError(t, err)
			}
			target := testCase.target
			target.Stack = "default"

			for range max(testCase.builds, 1) {
				entries, warnings, err := b.ForTarget(&target)
				require.NoError(t, err)
				env := toMap(entries)
				port, _ := alloc.Lookup(target.Service, "POSTGRES_PORT")
				for key, want := range testCase.wantEnv {
					require.Equal(t, strings.ReplaceAll(want, "PORT", strconv.Itoa(port)), env[key], key)
				}
				if testCase.check != nil {
					testCase.check(t, env, warnings, alloc)
				}
			}
		})
	}
}

// TestIsHostKind pins the suffix rule: a var is a host kind only with its
// `_HOST` suffix.
func TestIsHostKind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		in   string
		want bool
	}{
		{name: "Success/RestPort", in: "REST_PORT"},
		{name: "Success/GrpcPort", in: "GRPC_PORT"},
		{name: "Success/SmtpPort", in: "SMTP_PORT"},
		{name: "Success/BarePort", in: "PORT"},
		{name: "Success/PortNotSuffix", in: "REST_PORT_EXTRA"},
		{name: "Success/Host", in: "REST_HOST", want: true},
		{name: "Success/BareHost", in: "HOST"},
		{name: "Success/URL", in: "REST_URL"},
		{name: "Success/BareURL", in: "URL"},
		{name: "Success/Empty", in: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, isHostKind(testCase.in))
		})
	}
}

// TestIsURLKind pins the suffix rule: a var is a URL kind only with its `_URL`
// suffix.
func TestIsURLKind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		in   string
		want bool
	}{
		{name: "Success/RestPort", in: "REST_PORT"},
		{name: "Success/GrpcPort", in: "GRPC_PORT"},
		{name: "Success/SmtpPort", in: "SMTP_PORT"},
		{name: "Success/BarePort", in: "PORT"},
		{name: "Success/PortNotSuffix", in: "REST_PORT_EXTRA"},
		{name: "Success/Host", in: "REST_HOST"},
		{name: "Success/BareHost", in: "HOST"},
		{name: "Success/URL", in: "REST_URL", want: true},
		{name: "Success/BareURL", in: "URL"},
		{name: "Success/Empty", in: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, isURLKind(testCase.in))
		})
	}
}
