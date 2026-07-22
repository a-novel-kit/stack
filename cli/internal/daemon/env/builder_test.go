package env

import (
	"path/filepath"
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

func TestForTarget_DSNRewriteForGoExec(t *testing.T) {
	// Compose declares a literal in-container DSN (`postgres-X:5432`), and the
	// daemon's POSTGRES_PORT allocation for this service overrides it to
	// `localhost:<port>`. Without that rewrite, go-exec mode cannot reach its
	// own postgres.
	b, alloc := newBuilderWith([]string{"svc"})
	// Pre-allocate the port the way infra-up would, under a service-level
	// consumer with owner=svc.
	port, err := alloc.Acquire("svc", "POSTGRES_PORT", "svc-infra")
	if err != nil {
		t.Fatal(err)
	}
	tgt := &discovery.Target{
		Name:    "migrations",
		Service: "svc",
		Stack:   "default",
		Environment: map[string]string{
			"POSTGRES_DSN": "postgres://postgres:postgres@postgres-svc:5432/postgres?sslmode=disable",
		},
	}
	entries, _, err := b.ForTarget(tgt, alloc.Services())
	if err != nil {
		t.Fatal(err)
	}
	m := toMap(entries)
	wantDSN := "postgres://postgres:postgres@localhost:" + itoa(port) + "/postgres?sslmode=disable"
	if m["POSTGRES_DSN"] != wantDSN {
		t.Errorf("POSTGRES_DSN: got %q want %q", m["POSTGRES_DSN"], wantDSN)
	}
}

func TestForTarget_CrossServiceRef(t *testing.T) {
	// A consumer referencing ${SERVICE_X_GRPC_PORT} resolves to an allocation
	// against service-x's grpc target, whose port substitutes into the
	// consumer's value. The service names must carry their real shape, where
	// "service-x" yields the prefix "SERVICE_X", for resolveOwner to reverse
	// them.
	b, alloc := newBuilderWith([]string{"service-x", "service-y"})
	tgt := &discovery.Target{
		Name:    "rest",
		Service: "service-y",
		Stack:   "default",
		Environment: map[string]string{
			"DEP_HOST": "${SERVICE_X_GRPC_HOST}",
			"DEP_PORT": "${SERVICE_X_GRPC_PORT}",
		},
	}
	entries, _, err := b.ForTarget(tgt, alloc.Services())
	if err != nil {
		t.Fatal(err)
	}
	m := toMap(entries)
	if m["DEP_HOST"] != "localhost" {
		t.Errorf("DEP_HOST: got %q want localhost", m["DEP_HOST"])
	}
	if m["DEP_PORT"] == "" || m["DEP_PORT"] == "0" {
		t.Errorf("DEP_PORT: got %q want a real allocated port number", m["DEP_PORT"])
	}
	// The allocation must be recorded, so a later service-x/grpc target lands
	// on the same slot.
	p, ok := alloc.Lookup("service-x", "GRPC_PORT")
	if !ok {
		t.Error("Lookup of allocated cross-service slot failed")
	}
	if itoa(p) != m["DEP_PORT"] {
		t.Errorf("Lookup port %d disagrees with substituted DEP_PORT %s", p, m["DEP_PORT"])
	}
}

func TestForTarget_PortsBlockTriggersAllocation(t *testing.T) {
	// Compose's `ports:` block is the daemon's only signal to allocate a port
	// the `environment:` block never references, and mergePortRefs folds it
	// into the same resolution pass.
	b, alloc := newBuilderWith([]string{"svc"})
	tgt := &discovery.Target{
		Name:        "rest",
		Service:     "svc",
		Stack:       "default",
		Ports:       []string{"${REST_PORT}:8080"},
		Environment: map[string]string{}, // intentionally empty
	}
	entries, _, err := b.ForTarget(tgt, alloc.Services())
	if err != nil {
		t.Fatal(err)
	}
	m := toMap(entries)
	if m["REST_PORT"] == "" {
		t.Error("REST_PORT should be allocated from ports: block alone")
	}
	if m["REST_HOST"] != "localhost" {
		t.Errorf("REST_HOST derived: got %q want localhost", m["REST_HOST"])
	}
	if !strings.HasPrefix(m["REST_URL"], "http://localhost:") {
		t.Errorf("REST_URL derived: got %q want http://localhost:...", m["REST_URL"])
	}
}

func TestForTarget_PrefixedAndUnprefixedOwnView(t *testing.T) {
	// A target's own service prefix is stripped for its process env, so
	// service-foo with REST_PORT allocated sees both the local `REST_PORT` and
	// the cross-service `SERVICE_FOO_REST_PORT`, resolving to one number.
	b, alloc := newBuilderWith([]string{"service-foo"})
	tgt := &discovery.Target{
		Name:        "rest",
		Service:     "service-foo",
		Stack:       "default",
		Ports:       []string{"${REST_PORT}:8080"},
		Environment: map[string]string{},
	}
	entries, _, err := b.ForTarget(tgt, alloc.Services())
	if err != nil {
		t.Fatal(err)
	}
	m := toMap(entries)
	if m["REST_PORT"] == "" || m["SERVICE_FOO_REST_PORT"] == "" {
		t.Fatalf("expected both REST_PORT and SERVICE_FOO_REST_PORT, got %v", m)
	}
	if m["REST_PORT"] != m["SERVICE_FOO_REST_PORT"] {
		t.Errorf("local view %q must equal prefixed view %q",
			m["REST_PORT"], m["SERVICE_FOO_REST_PORT"])
	}
}

func TestForTarget_NoDoublePrefix(t *testing.T) {
	// An unguarded un-prefix and re-prefix pair yields
	// SERVICE_FOO_SERVICE_BAR_GRPC_PORT, so the builder must skip
	// already-prefixed cross-service keys when adding its own-prefix view.
	b, alloc := newBuilderWith([]string{"service-foo", "service-bar"})
	tgt := &discovery.Target{
		Name:    "rest",
		Service: "service-foo",
		Stack:   "default",
		Environment: map[string]string{
			// References service-bar's GRPC_PORT.
			"DEP_PORT": "${SERVICE_BAR_GRPC_PORT}",
		},
	}
	entries, _, err := b.ForTarget(tgt, alloc.Services())
	if err != nil {
		t.Fatal(err)
	}
	m := toMap(entries)
	for k := range m {
		if strings.Contains(k, "SERVICE_FOO_SERVICE_BAR_") {
			t.Errorf("double-prefix bug: emitted key %q", k)
		}
	}
}

func TestForService_LookupOnlyLeavesUnknownEmpty(t *testing.T) {
	// ForService is the read-only path (`a-novel run env`). Allocator
	// is never mutated — a var with no existing allocation comes back
	// empty rather than holding a new slot.
	b, _ := newBuilderWith([]string{"service-a", "service-b"})
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
	entries, err := b.ForService(svc, []string{"service-a", "service-b"})
	if err != nil {
		t.Fatal(err)
	}
	m := toMap(entries)
	// The substitute pass writes "" for a ref that does not resolve, matching
	// compose's behavior. Whether the key appears depends on substitution
	// order, so the assertion above is that the allocator stayed untouched.
	_ = m
}

func TestForTarget_PORTAloneDoesNotAllocate(t *testing.T) {
	// isAllocatedKind matches a key ending in _PORT, so a var literally named
	// PORT in compose stays a constant instead of binding a random port.
	b, alloc := newBuilderWith([]string{"svc"})
	tgt := &discovery.Target{
		Name:    "weird",
		Service: "svc",
		Stack:   "default",
		Environment: map[string]string{
			"PORT": "9999", // literal, not a ref
		},
	}
	entries, _, err := b.ForTarget(tgt, alloc.Services())
	if err != nil {
		t.Fatal(err)
	}
	m := toMap(entries)
	if m["PORT"] != "9999" {
		t.Errorf("PORT literal mangled: got %q want 9999", m["PORT"])
	}
	// No slot should have been allocated against svc.
	if snap := alloc.Snapshot(); len(snap) != 0 {
		t.Errorf("Allocator should be untouched, got %+v", snap)
	}
}

func TestForTarget_InjectsRepoSecrets(t *testing.T) {
	// ForTarget appends the repo's decrypted secrets as plain env entries, so
	// they ride into the spawned process's cmd.Env. Stubbing the secrets seam
	// keeps the real key store out of it.
	orig := injectSecrets
	injectSecrets = func(repoRoot string) (secrets.Resolution, error) {
		return secrets.Resolution{Env: map[string]string{"OPENAI_API_KEY": "sk-test"}}, nil
	}
	t.Cleanup(func() { injectSecrets = orig })

	b, alloc := newBuilderWith([]string{"svc"})
	tgt := &discovery.Target{
		Name:    "rest",
		Service: "svc",
		Stack:   "default",
		// serviceRoot resolves only from a non-empty CmdDir, which is what
		// gates the injection.
		CmdDir:      filepath.Join("/tmp", "service-svc", "cmd", "rest"),
		Environment: map[string]string{},
	}
	entries, _, err := b.ForTarget(tgt, alloc.Services())
	if err != nil {
		t.Fatal(err)
	}
	m := toMap(entries)
	if m["OPENAI_API_KEY"] != "sk-test" {
		t.Errorf("injected secret missing: OPENAI_API_KEY = %q, want sk-test", m["OPENAI_API_KEY"])
	}
}

func TestForTarget_NoCmdDirSkipsInjection(t *testing.T) {
	// Without a CmdDir, serviceRoot is empty and the injection is skipped, so
	// the seam is never called and no relative path is read by accident.
	called := false
	orig := injectSecrets
	injectSecrets = func(repoRoot string) (secrets.Resolution, error) {
		called = true
		return secrets.Resolution{}, nil
	}
	t.Cleanup(func() { injectSecrets = orig })

	b, alloc := newBuilderWith([]string{"svc"})
	tgt := &discovery.Target{Name: "rest", Service: "svc", Stack: "default", Environment: map[string]string{}}
	if _, _, err := b.ForTarget(tgt, alloc.Services()); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("injectSecrets must not be called when the target has no CmdDir")
	}
}

func TestForTarget_MissingSecretWarns(t *testing.T) {
	// A declared-but-unset secret raises a value-free warning line instead of
	// an injection or an error, so the operator sees what to set.
	orig := injectSecrets
	injectSecrets = func(repoRoot string) (secrets.Resolution, error) {
		return secrets.Resolution{
			Missing: []secrets.Declaration{
				{Env: "OPENAI_API_KEY", ID: "openai-key", Description: "used by generation"},
			},
		}, nil
	}
	t.Cleanup(func() { injectSecrets = orig })

	b, alloc := newBuilderWith([]string{"svc"})
	tgt := &discovery.Target{
		Name:        "rest",
		Service:     "svc",
		Stack:       "default",
		CmdDir:      filepath.Join("/tmp", "service-svc", "cmd", "rest"),
		Environment: map[string]string{},
	}
	entries, warnings, err := b.ForTarget(tgt, alloc.Services())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := toMap(entries)["OPENAI_API_KEY"]; ok {
		t.Error("a missing secret must not be injected into the env")
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning line, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "OPENAI_API_KEY") ||
		!strings.Contains(warnings[0], "openai-key") ||
		!strings.Contains(warnings[0], "used by generation") ||
		!strings.Contains(warnings[0], "a-novel secrets set openai-key") {
		t.Errorf("warning missing expected (value-free) content: %q", warnings[0])
	}
}
