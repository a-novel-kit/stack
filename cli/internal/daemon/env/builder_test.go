package env

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/secrets"
)

// Builder tests exercise refs.go and allocator.go together against the
// compose-env wiring rules a service depends on at runtime. Each one builds a
// minimal in-memory discovery.Target, touching neither the filesystem nor
// podman, and asserts on the resulting env map.

// toMap collapses the sorted entry slice into a map keyed by variable name,
// which is easier to assert against than a positional slice.
func toMap(entries []Entry) map[string]string {
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		out[e.Key] = e.Value
	}
	return out
}

func newBuilderWith(services []string) (*Builder, *Allocator) {
	a := NewAllocator()
	a.SetServices(services)
	return NewBuilder(a), a
}

// TestBuilderForTarget swaps the package-level injectSecrets seam, so its cases
// run sequentially.
func TestBuilderForTarget(t *testing.T) {
	orig := injectSecrets
	t.Cleanup(func() { injectSecrets = orig })
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
			name: "DSNRewriteForGoExec", services: []string{"svc"}, infraPort: "POSTGRES_PORT",
			target: discovery.Target{Name: "migrations", Service: "svc", Environment: map[string]string{
				"POSTGRES_DSN": "postgres://postgres:postgres@postgres-svc:5432/postgres?sslmode=disable",
			}},
			wantEnv: hostPostgres,
		},
		{
			// The rewritten DSN carries the compose credentials percent-escaped, so a
			// password holding `@` or `/` still parses. The discrete settings carry
			// them as they are.
			name: "DSNEscapesCredentials", services: []string{"svc"}, infraPort: "POSTGRES_PORT",
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
			name: "DiscreteRewriteForGoExec", services: []string{"svc"}, infraPort: "POSTGRES_PORT",
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
			name: "InNetworkPortYieldsToAllocation", services: []string{"svc"}, infraPort: "POSTGRES_PORT",
			target: discovery.Target{Name: "rest", Service: "svc", Environment: map[string]string{
				"POSTGRES_HOST": "postgres-svc",
				"POSTGRES_PORT": "5432",
			}},
			wantEnv: hostPostgres,
		},
		{
			// Without the allocation, which infra-up makes, nothing listens on the host,
			// and the compose values stay.
			name: "NoAllocationKeepsCompose", services: []string{"svc"},
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
			name: "CrossServiceRef", services: []string{"service-x", "service-y"},
			target: discovery.Target{Name: "rest", Service: "service-y", Environment: map[string]string{
				"DEP_HOST": "${SERVICE_X_GRPC_HOST}",
				"DEP_PORT": "${SERVICE_X_GRPC_PORT}",
			}},
			check: func(t *testing.T, env map[string]string, _ []string, alloc *Allocator) {
				p, ok := alloc.Lookup("service-x", "GRPC_PORT")
				if !ok || p == 0 || strconv.Itoa(p) != env["DEP_PORT"] || env["DEP_HOST"] != "localhost" {
					t.Errorf("DEP_HOST/DEP_PORT = %q/%q, want localhost and the recorded slot %d (found %v)",
						env["DEP_HOST"], env["DEP_PORT"], p, ok)
				}
			},
		},
		{
			// Compose's `ports:` block is the only signal to allocate a port the
			// `environment:` block never references; mergePortRefs folds it into the
			// same resolution pass.
			name: "PortsBlockTriggersAllocation", services: []string{"svc"},
			target: discovery.Target{Name: "rest", Service: "svc", Ports: []string{"${REST_PORT}:8080"}},
			check: func(t *testing.T, env map[string]string, _ []string, _ *Allocator) {
				if env["REST_PORT"] == "" || env["REST_HOST"] != "localhost" ||
					!strings.HasPrefix(env["REST_URL"], "http://localhost:") {
					t.Errorf("env = %v, want REST_PORT allocated with its derived host and URL", env)
				}
			},
		},
		{
			// A target's own service prefix is stripped for its process env, so it
			// sees both the local `REST_PORT` and the cross-service
			// `SERVICE_FOO_REST_PORT`, resolving to one number.
			name: "PrefixedAndUnprefixedOwnView", services: []string{"service-foo"},
			target: discovery.Target{Name: "rest", Service: "service-foo", Ports: []string{"${REST_PORT}:8080"}},
			check: func(t *testing.T, env map[string]string, _ []string, _ *Allocator) {
				if env["REST_PORT"] == "" || env["REST_PORT"] != env["SERVICE_FOO_REST_PORT"] {
					t.Errorf("REST_PORT/SERVICE_FOO_REST_PORT = %q/%q, want one allocated port",
						env["REST_PORT"], env["SERVICE_FOO_REST_PORT"])
				}
			},
		},
		{
			// An unguarded un-prefix and re-prefix pair yields
			// SERVICE_FOO_SERVICE_BAR_GRPC_PORT, so the own-prefix view must skip
			// already-prefixed cross-service keys.
			name: "NoDoublePrefix", services: []string{"service-foo", "service-bar"},
			target: discovery.Target{Name: "rest", Service: "service-foo", Environment: map[string]string{
				"DEP_PORT": "${SERVICE_BAR_GRPC_PORT}",
			}},
			check: func(t *testing.T, env map[string]string, _ []string, _ *Allocator) {
				for k := range env {
					if strings.Contains(k, "SERVICE_FOO_SERVICE_BAR_") {
						t.Errorf("double-prefix bug: emitted key %q", k)
					}
				}
			},
		},
		{
			// isAllocatedKind matches a key ending in _PORT, so a var literally
			// named PORT stays a constant and allocates nothing.
			name: "PORTAloneDoesNotAllocate", services: []string{"svc"},
			target: discovery.Target{Name: "weird", Service: "svc", Environment: map[string]string{"PORT": "9999"}},
			check: func(t *testing.T, env map[string]string, _ []string, alloc *Allocator) {
				if env["PORT"] != "9999" || len(alloc.Snapshot()) != 0 {
					t.Errorf("PORT = %q, allocations = %+v, want the literal and none", env["PORT"], alloc.Snapshot())
				}
			},
		},
		{
			// The repo's decrypted secrets ride into the spawned process's cmd.Env
			// as plain entries.
			name: "InjectsRepoSecrets", services: []string{"svc"},
			target:  discovery.Target{Name: "rest", Service: "svc", CmdDir: cmdDir},
			secrets: &secrets.Resolution{Env: map[string]string{"OPENAI_API_KEY": "sk-test"}},
			check: func(t *testing.T, env map[string]string, _ []string, _ *Allocator) {
				if env["OPENAI_API_KEY"] != "sk-test" {
					t.Errorf("injected secret missing: OPENAI_API_KEY = %q, want sk-test", env["OPENAI_API_KEY"])
				}
			},
		},
		{
			// Without a CmdDir, serviceRoot is empty and the injection is skipped,
			// so no relative path is read by accident.
			name: "NoCmdDirSkipsInjection", services: []string{"svc"},
			target: discovery.Target{Name: "rest", Service: "svc"},
		},
		{
			// A declared-but-unset secret stays out of the env and raises one
			// value-free warning line naming what to set.
			name: "MissingSecretWarns", services: []string{"svc"},
			target: discovery.Target{Name: "rest", Service: "svc", CmdDir: cmdDir},
			secrets: &secrets.Resolution{Missing: []secrets.Declaration{
				{Env: "OPENAI_API_KEY", ID: "openai-key", Description: "used by generation"},
			}},
			check: func(t *testing.T, env map[string]string, warnings []string, _ *Allocator) {
				if _, ok := env["OPENAI_API_KEY"]; ok {
					t.Error("a missing secret must not be injected into the env")
				}
				if len(warnings) != 1 {
					t.Fatalf("expected 1 warning line, got %d: %v", len(warnings), warnings)
				}
				wantWarning := []string{"OPENAI_API_KEY", "openai-key", "used by generation", "a-novel secrets set openai-key"}
				for _, want := range wantWarning {
					if !strings.Contains(warnings[0], want) {
						t.Errorf("warning %q lacks %q", warnings[0], want)
					}
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			injectSecrets = func(string) (secrets.Resolution, error) {
				if testCase.secrets == nil {
					t.Error("injectSecrets must not be called")
					return secrets.Resolution{}, nil
				}
				return *testCase.secrets, nil
			}
			b, alloc := newBuilderWith(testCase.services)
			if testCase.infraPort != "" {
				if _, err := alloc.Acquire(testCase.target.Service, testCase.infraPort, "svc-infra"); err != nil {
					t.Fatal(err)
				}
			}
			testCase.target.Stack = "default"
			entries, warnings, err := b.ForTarget(&testCase.target)
			if err != nil {
				t.Fatal(err)
			}
			env := toMap(entries)
			port, _ := alloc.Lookup(testCase.target.Service, "POSTGRES_PORT")
			for key, want := range testCase.wantEnv {
				if want = strings.ReplaceAll(want, "PORT", strconv.Itoa(port)); env[key] != want {
					t.Errorf("%s = %q, want %q", key, env[key], want)
				}
			}
			if testCase.check != nil {
				testCase.check(t, env, warnings, alloc)
			}
		})
	}
}

func TestForService_LookupOnlyLeavesUnknownEmpty(t *testing.T) {
	// ForService is the read-only path (`a-novel run env`). The allocator
	// is never mutated, and a var with no existing allocation comes back
	// empty.
	b, alloc := newBuilderWith([]string{"service-a", "service-b"})
	svc := &discovery.Service{
		Name:  "service-a",
		Stack: "default",
		Targets: []*discovery.Target{
			{
				Name: "rest", Service: "service-a", Stack: "default",
				Environment: map[string]string{
					"DEP_PORT": "${SERVICE_B_GRPC_PORT}",
				},
			},
		},
	}
	entries, err := b.ForService(svc, "")
	if err != nil {
		t.Fatal(err)
	}
	// The substitute pass writes "" for a ref that does not resolve, matching
	// compose's behavior.
	if got := toMap(entries)["DEP_PORT"]; got != "" {
		t.Errorf("DEP_PORT: got %q, want empty for an unallocated port", got)
	}
	if snap := alloc.Snapshot(); len(snap) != 0 {
		t.Errorf("read-only ForService allocated: %+v", snap)
	}
}

// TestBuilder_OverlappingServiceNamesResolveLongestFirst covers service names
// registered in discovery order, which is alphabetical, so service-template
// precedes the service-template-extra it shadows. Every builder path must still
// route SERVICE_TEMPLATE_EXTRA_* to service-template-extra.
func TestBuilder_OverlappingServiceNamesResolveLongestFirst(t *testing.T) {
	b, alloc := newBuilderWith([]string{"service-consumer", "service-template", "service-template-extra"})
	tgt := &discovery.Target{
		Name:        "rest",
		Service:     "service-consumer",
		Stack:       "default",
		Environment: map[string]string{"DEP_PORT": "${SERVICE_TEMPLATE_EXTRA_GRPC_PORT}"},
	}

	entries, _, err := b.ForTarget(tgt)
	if err != nil {
		t.Fatal(err)
	}
	port, ok := alloc.Lookup("service-template-extra", "GRPC_PORT")
	if !ok {
		t.Fatalf("ForTarget allocated %+v, want service-template-extra/GRPC_PORT", alloc.Snapshot())
	}
	if got := toMap(entries)["DEP_PORT"]; got != strconv.Itoa(port) {
		t.Errorf("ForTarget DEP_PORT: got %q, want %d", got, port)
	}

	svc := &discovery.Service{Name: "service-consumer", Stack: "default", Targets: []*discovery.Target{tgt}}
	viewed, err := b.ForService(svc, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := toMap(viewed)["DEP_PORT"]; got != strconv.Itoa(port) {
		t.Errorf("ForService DEP_PORT: got %q, want %d", got, port)
	}
}

// TestForService_ConsumerAllocatesInfraPorts covers the infra-up path: with a
// consumer, the ports the infra references are acquired for it, and Release
// frees them again. Compose publishes the infra on what the block holds, so
// every allocated port appears with its number, whether a target names it or
// not. A target's own POSTGRES_PORT, the port inside the compose network,
// yields to the allocation.
func TestForService_ConsumerAllocatesInfraPorts(t *testing.T) {
	b, alloc := newBuilderWith([]string{"svc"})
	svc := &discovery.Service{
		Name:  "svc",
		Stack: "default",
		Targets: []*discovery.Target{
			{Name: "migrations", Service: "svc", Stack: "default", Environment: map[string]string{
				"POSTGRES_HOST": "postgres-svc",
				"POSTGRES_PORT": "5432",
			}},
			{Name: "rest", Service: "svc", Stack: "default", Environment: map[string]string{"MAIL_ADDR": "${MAIL_HOST}:${MAIL_PORT}"}},
		},
		Infra: []*discovery.Infra{
			{
				Name:        "postgres-svc",
				Service:     "svc",
				Stack:       "default",
				Ports:       []string{"${POSTGRES_PORT}:5432"},
				Environment: map[string]string{"POSTGRES_USER": "postgres"},
			},
			{Name: "mail-svc", Service: "svc", Stack: "default", Ports: []string{"${MAIL_PORT}:1025"}},
		},
	}

	entries, err := b.ForService(svc, "default/svc-infra")
	if err != nil {
		t.Fatal(err)
	}
	port, ok := alloc.Lookup("svc", "POSTGRES_PORT")
	if !ok {
		t.Fatal("infra-up ForService did not allocate POSTGRES_PORT")
	}
	mailPort, ok := alloc.Lookup("svc", "MAIL_PORT")
	if !ok {
		t.Fatal("infra-up ForService did not allocate MAIL_PORT")
	}
	m := toMap(entries)
	if m["POSTGRES_USER"] != "postgres" || m["POSTGRES_PORT"] != strconv.Itoa(port) || m["POSTGRES_HOST"] != "localhost" {
		t.Errorf("infra env: got %v, want the constant, POSTGRES_PORT=%d and the host", m, port)
	}
	if want := "localhost:" + strconv.Itoa(mailPort); m["MAIL_PORT"] != strconv.Itoa(mailPort) || m["MAIL_ADDR"] != want {
		t.Errorf("MAIL_PORT/MAIL_ADDR = %q/%q, want %d and %q", m["MAIL_PORT"], m["MAIL_ADDR"], mailPort, want)
	}
	alloc.Release("default/svc-infra")
	if _, ok := alloc.Lookup("svc", "POSTGRES_PORT"); ok {
		t.Errorf("port %d still allocated after releasing the infra consumer", port)
	}
}

// TestBuilderKeepPorts covers the reinstall relaunch: the ports the previous
// daemon recorded are reserved for the target, so its env and every other
// consumer resolve to what the relaunched process binds.
func TestBuilderKeepPorts(t *testing.T) {
	t.Parallel()

	target := func() *discovery.Target {
		return &discovery.Target{
			Name: "rest", Service: "svc", Stack: "default",
			Ports: []string{"${REST_PORT}:8080"},
			Environment: map[string]string{
				"DEP_PORT": "${SERVICE_OTHER_GRPC_PORT}",
				// A constant named like a port is no reference, so it claims nothing.
				"ALIAS_PORT": "1234",
			},
		}
	}
	held := []string{"REST_PORT=41001", "SERVICE_OTHER_GRPC_PORT=41002", "ALIAS_PORT=41003", "STRAY_PORT=41004", "PATH=/bin"}

	testCases := []struct {
		name string
		// seed reserves a slot before KeepPorts runs, as adopting a container does.
		seed     func(a *Allocator)
		wantRest int
	}{
		{name: "Success/KeepsReferencedPorts", seed: func(*Allocator) {}, wantRest: 41001},
		{name: "Success/AllocatedSlotKeepsItsPort", seed: func(a *Allocator) { a.Reserve("svc", "REST_PORT", 40000, "default/svc-infra") }, wantRest: 40000},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			b, alloc := newBuilderWith([]string{"svc", "service-other"})
			testCase.seed(alloc)
			tgt := target()
			b.KeepPorts(tgt, held)

			entries, _, err := b.ForTarget(tgt)
			if err != nil {
				t.Fatal(err)
			}
			env := toMap(entries)
			if env["REST_PORT"] != strconv.Itoa(testCase.wantRest) || env["DEP_PORT"] != "41002" || env["ALIAS_PORT"] != "1234" {
				t.Errorf("env: REST_PORT=%s DEP_PORT=%s ALIAS_PORT=%s, want %d, 41002, 1234",
					env["REST_PORT"], env["DEP_PORT"], env["ALIAS_PORT"], testCase.wantRest)
			}
			snap := alloc.Snapshot()
			claimed := make([]string, len(snap))
			for i, slot := range snap {
				claimed[i] = slot.Owner + "/" + slot.LocalVar
			}
			if want := []string{"service-other/GRPC_PORT", "svc/REST_PORT"}; strings.Join(claimed, ",") != strings.Join(want, ",") {
				t.Errorf("claimed slots %v, want %v", claimed, want)
			}
			// A sibling consumer resolves the kept port, not a fresh one.
			sibling := &discovery.Target{
				Name: "web", Service: "service-other", Stack: "default",
				Environment: map[string]string{"API_PORT": "${SVC_REST_PORT}"},
			}
			siblingEnv, _, err := b.ForTarget(sibling)
			if err != nil {
				t.Fatal(err)
			}
			if got := toMap(siblingEnv)["API_PORT"]; got != strconv.Itoa(testCase.wantRest) {
				t.Errorf("sibling API_PORT = %s, want %d", got, testCase.wantRest)
			}
			// Releasing the relaunched target drops its claims like any other.
			alloc.Release(tgt.ID())
			alloc.Release(sibling.ID())
			if _, ok := alloc.Lookup("service-other", "GRPC_PORT"); ok {
				t.Error("service-other/GRPC_PORT outlived its consumers")
			}
		})
	}
}

// TestBuilderHeldPorts pins the checkpoint side: the ports a running target
// holds, named as its env references them, and nothing else, so the next
// daemon's KeepPorts hands them straight back.
func TestBuilderHeldPorts(t *testing.T) {
	t.Parallel()

	b, alloc := newBuilderWith([]string{"svc", "service-other"})
	tgt := &discovery.Target{
		Name: "rest", Service: "svc", Stack: "default",
		Ports:       []string{"${REST_PORT}:8080"},
		Environment: map[string]string{"DEP_PORT": "${SERVICE_OTHER_GRPC_PORT}", "TOKEN": "secret-value"},
	}
	if _, _, err := b.ForTarget(tgt); err != nil {
		t.Fatal(err)
	}
	rest, _ := alloc.Lookup("svc", "REST_PORT")
	grpc, _ := alloc.Lookup("service-other", "GRPC_PORT")

	held := b.HeldPorts(tgt)
	want := []string{"REST_PORT=" + strconv.Itoa(rest), "SERVICE_OTHER_GRPC_PORT=" + strconv.Itoa(grpc)}
	if strings.Join(held, ",") != strings.Join(want, ",") {
		t.Fatalf("HeldPorts = %v, want %v", held, want)
	}

	next, nextAlloc := newBuilderWith([]string{"svc", "service-other"})
	next.KeepPorts(tgt, held)
	if got, _ := nextAlloc.Lookup("svc", "REST_PORT"); got != rest {
		t.Errorf("next daemon REST_PORT = %d, want %d", got, rest)
	}
	if got, _ := nextAlloc.Lookup("service-other", "GRPC_PORT"); got != grpc {
		t.Errorf("next daemon SERVICE_OTHER_GRPC_PORT = %d, want %d", got, grpc)
	}
}

// TestBuilderURLRefs pins how a `${X_URL}` reference resolves. A URL beside its
// port carries that port, and a cross-service URL never carries the consumer's
// own port. Each case builds many times, so a result that depends on map
// order shows.
func TestBuilderURLRefs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		services []string
		// env is the target's environment block. Its URL reference is A.
		env map[string]string
		// wantA derives the expected value of A from the built env.
		wantA func(env map[string]string) string
	}{
		{
			name: "Success/BesideItsPort", services: []string{"svc"},
			env:   map[string]string{"A": "${REST_URL}", "B": "${REST_PORT}"},
			wantA: func(env map[string]string) string { return "http://localhost:" + env["B"] },
		},
		{
			name: "Success/CrossServiceBesideItsPort", services: []string{"svc", "service-y"},
			env:   map[string]string{"A": "${SERVICE_Y_GRPC_URL}", "B": "${SERVICE_Y_GRPC_PORT}"},
			wantA: func(env map[string]string) string { return "localhost:" + env["B"] },
		},
		{
			name: "Success/CrossServiceBesideOwnPort", services: []string{"svc", "service-y"},
			env:   map[string]string{"A": "${SERVICE_Y_GRPC_URL}", "B": "${GRPC_PORT}"},
			wantA: func(map[string]string) string { return "" },
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			b, _ := newBuilderWith(testCase.services)
			target := discovery.Target{Name: "rest", Service: "svc", Stack: "default", Environment: testCase.env}
			for range 50 {
				entries, _, err := b.ForTarget(&target)
				if err != nil {
					t.Fatal(err)
				}
				env := toMap(entries)
				if want := testCase.wantA(env); env["A"] != want {
					t.Fatalf("A = %q, want %q (env %v)", env["A"], want, env)
				}
			}
		})
	}
}
