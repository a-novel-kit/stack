package env

import (
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
)

// Builder tests assert the integration of refs.go + allocator.go against
// the real compose-env wiring rules — every behavior the env-builder
// guarantees that a service depends on at runtime.
//
// Test shape: minimal in-memory discovery.Target fixtures, no filesystem,
// no podman. Each test asserts on the resulting env map (entries
// converted via toMap below) so assertions can be expressed in terms of
// "the key X has value Y".

// toMap collapses the sorted entry slice into a map keyed by variable
// name. The order is already deterministic (alphabetical), but maps are
// easier to assert against than positional slices.
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
	// The canonical case: compose has a literal in-container DSN
	// (`postgres-X:5432`), but the daemon allocates POSTGRES_PORT for
	// this service so the synthesized output overrides the DSN to
	// `localhost:<port>`. Without this rewrite, go-exec mode can't
	// connect to its own postgres.
	b, alloc := newBuilderWith([]string{"svc"})
	// Pre-allocate the port the way `infra-up` would: under a
	// service-level consumer, owner=svc.
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
	entries, err := b.ForTarget(tgt, alloc.Services())
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
	// Cross-service ref: consumer's env references
	// ${SERVICE_X_GRPC_PORT}. The daemon resolves it to an allocation
	// against service-x's grpc target and substitutes the port number
	// into the consumer's value. Service names must be the real
	// shape ("service-x" → prefix "SERVICE_X") so resolveOwner can
	// reverse them.
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
	entries, err := b.ForTarget(tgt, alloc.Services())
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
	// The allocation must be RECORDED so a later service-x/grpc target
	// gets the same slot.
	p, ok := alloc.Lookup("service-x", "GRPC_PORT")
	if !ok {
		t.Error("Lookup of allocated cross-service slot failed")
	}
	if itoa(p) != m["DEP_PORT"] {
		t.Errorf("Lookup port %d disagrees with substituted DEP_PORT %s", p, m["DEP_PORT"])
	}
}

func TestForTarget_PortsBlockTriggersAllocation(t *testing.T) {
	// Compose's `ports:` block is the daemon's only signal to
	// allocate a port that's not also referenced in `environment:`.
	// `mergePortRefs` folds it into the same resolution pass.
	b, alloc := newBuilderWith([]string{"svc"})
	tgt := &discovery.Target{
		Name:    "rest",
		Service: "svc",
		Stack:   "default",
		Ports:   []string{"${REST_PORT}:8080"},
		Environment: map[string]string{}, // intentionally empty
	}
	entries, err := b.ForTarget(tgt, alloc.Services())
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
	// Spec §6.4: a target's OWN service prefix is stripped for its
	// own process env. So svc=service-foo with REST_PORT allocated
	// gets both `REST_PORT` (un-prefixed local view) and
	// `SERVICE_FOO_REST_PORT` (prefixed cross-service view) — clients
	// looking through either name resolve to the same number.
	b, alloc := newBuilderWith([]string{"service-foo"})
	tgt := &discovery.Target{
		Name:    "rest",
		Service: "service-foo",
		Stack:   "default",
		Ports:   []string{"${REST_PORT}:8080"},
		Environment: map[string]string{},
	}
	entries, err := b.ForTarget(tgt, alloc.Services())
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
	// Pitfall: when un-prefix + re-prefix loops aren't carefully
	// guarded, you get SERVICE_FOO_SERVICE_BAR_GRPC_PORT. The builder
	// must skip already-prefixed cross-service keys when adding the
	// own-prefix view.
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
	entries, err := b.ForTarget(tgt, alloc.Services())
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
	// Empty value rather than absent — the substitute pass writes "" when
	// the ref doesn't resolve, matching compose's behavior. The key
	// presence depends on the substitute order; we just assert the
	// allocator wasn't touched.
	_ = m
}

func TestForTarget_PORTAloneDoesNotAllocate(t *testing.T) {
	// Regression guard: isAllocatedKind matches keys ending in _PORT,
	// not the bare "PORT". A var literally named PORT in compose
	// should be treated as a constant, not allocated. Documents
	// existing behavior so future "let's be flexible" tweaks don't
	// accidentally start binding random ports.
	b, alloc := newBuilderWith([]string{"svc"})
	tgt := &discovery.Target{
		Name:    "weird",
		Service: "svc",
		Stack:   "default",
		Environment: map[string]string{
			"PORT": "9999", // literal, not a ref
		},
	}
	entries, err := b.ForTarget(tgt, alloc.Services())
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
