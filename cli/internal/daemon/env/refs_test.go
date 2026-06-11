package env

import (
	"reflect"
	"testing"
)

// Test the small substitution-rule primitives the env builder hangs off
// — ServicePrefix, extractRefs, substitute, the *_KIND classifiers,
// derivedFor, urlFor. These are pure functions; every one of them is a
// silent regression risk because the daemon's cross-service env wiring
// reads through them at every Acquire.

func TestServicePrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"service-json-keys", "SERVICE_JSON_KEYS"},
		{"service-authentication", "SERVICE_AUTHENTICATION"},
		// Bare names without hyphens still get uppercased — defensive
		// against future services that don't follow service-X naming.
		{"plain", "PLAIN"},
		{"", ""},
	}
	for _, c := range cases {
		got := ServicePrefix(c.in)
		if got != c.want {
			t.Errorf("ServicePrefix(%q): got %q want %q", c.in, got, c.want)
		}
	}
}

func TestExtractRefs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		// Single bare reference.
		{"${POSTGRES_PORT}", []string{"POSTGRES_PORT"}},
		// Two references in one value — order preserved.
		{"${SMTP_HOST}:${SMTP_PORT}", []string{"SMTP_HOST", "SMTP_PORT"}},
		// Default-value syntax recognized but the default is discarded.
		{"${X:-fallback}", []string{"X"}},
		// Mixed literal + refs.
		{"postgres://${USER}:${PASS}@${HOST}:${PORT}/db", []string{"USER", "PASS", "HOST", "PORT"}},
		// Same ref twice — deduplicated.
		{"${X}/${X}", []string{"X"}},
		// No refs.
		{"plain-literal", nil},
		{"", nil},
	}
	for _, c := range cases {
		got := extractRefs(c.in)
		// nil and empty slice are both "no refs" — normalize.
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("extractRefs(%q): got %v want %v", c.in, got, c.want)
		}
	}
}

func TestSubstitute(t *testing.T) {
	ctx := map[string]string{
		"HOST": "localhost",
		"PORT": "5432",
	}
	cases := []struct{ in, want string }{
		{"${HOST}:${PORT}", "localhost:5432"},
		{"http://${HOST}", "http://localhost"},
		// Unknown reference → empty (matches compose semantics).
		{"${UNKNOWN}", ""},
		{"prefix ${UNKNOWN} suffix", "prefix  suffix"},
		// No references → identity.
		{"plain", "plain"},
	}
	for _, c := range cases {
		got := substitute(c.in, ctx)
		if got != c.want {
			t.Errorf("substitute(%q): got %q want %q", c.in, got, c.want)
		}
	}
}

func TestResolveOwner(t *testing.T) {
	// Services list sorted longest-first — same shape the allocator
	// hands us via SetServices.
	services := []string{
		"service-authentication",
		"service-json-keys",
	}
	cases := []struct {
		varName   string
		wantOwner string
		wantLocal string
	}{
		{"SERVICE_JSON_KEYS_GRPC_PORT", "service-json-keys", "GRPC_PORT"},
		{"SERVICE_AUTHENTICATION_REST_PORT", "service-authentication", "REST_PORT"},
		// Unprefixed → "" owner, full name as local.
		{"POSTGRES_PORT", "", "POSTGRES_PORT"},
		// Prefix-collision pitfall: a var name that starts with a
		// service prefix but isn't actually a service ref. We don't
		// guard against this — anyone naming a var SERVICE_JSON_KEYS_X
		// is owning the consequence. Documenting current behavior.
		{"SERVICE_JSON_KEYS_PORT", "service-json-keys", "PORT"},
	}
	for _, c := range cases {
		gotOwner, gotLocal := resolveOwner(c.varName, services)
		if gotOwner != c.wantOwner || gotLocal != c.wantLocal {
			t.Errorf("resolveOwner(%q): got (%q, %q) want (%q, %q)",
				c.varName, gotOwner, gotLocal, c.wantOwner, c.wantLocal)
		}
	}
}

func TestResolveOwnerLongestMatchWins(t *testing.T) {
	// When two service names share a prefix, the longer must match
	// first. Without this, `service-template-extra/X` would resolve
	// against `service-template` and silently misroute.
	services := []string{
		// Caller's responsibility to sort longest-first; mirror what
		// allocator.SetServices does.
		"service-template-extra",
		"service-template",
	}
	owner, local := resolveOwner("SERVICE_TEMPLATE_EXTRA_PORT", services)
	if owner != "service-template-extra" || local != "PORT" {
		t.Errorf("longest-match: got (%q, %q) want (service-template-extra, PORT)", owner, local)
	}
}

func TestIsAllocatedKind(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"REST_PORT", true},
		{"GRPC_PORT", true},
		{"SMTP_PORT", true},
		// PORT alone is NOT allocated — the rule is "ends with _PORT",
		// not "is PORT". Documenting because this trips people up.
		{"PORT", false},
		{"HOST", false},
		{"URL", false},
		{"REST_PORT_EXTRA", false},
		{"", false},
	}
	for _, c := range cases {
		got := isAllocatedKind(c.in)
		if got != c.want {
			t.Errorf("isAllocatedKind(%q): got %v want %v", c.in, got, c.want)
		}
	}
}

func TestIsHostKind_IsURLKind(t *testing.T) {
	if !isHostKind("REST_HOST") {
		t.Error("REST_HOST should be host kind")
	}
	if isHostKind("HOST") {
		t.Error("bare HOST should NOT be host kind (no _HOST suffix)")
	}
	if !isURLKind("REST_URL") {
		t.Error("REST_URL should be url kind")
	}
	if isURLKind("URL") {
		t.Error("bare URL should NOT be url kind")
	}
}

func TestDerivedFor(t *testing.T) {
	got := derivedFor("REST_PORT", 12345)
	want := map[string]string{
		"REST_PORT": "12345",
		"REST_HOST": "localhost",
		"REST_URL":  "http://localhost:12345",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("derivedFor(REST_PORT, 12345): got %v want %v", got, want)
	}
}

func TestUrlFor_GRPC_Schemeless(t *testing.T) {
	// gRPC URLs are deliberately schemeless — `localhost:port` is what
	// grpc-go's Dial expects. Adding `grpc://` would force every consumer
	// to strip it. Lock this in.
	if got, want := urlFor("GRPC", 9090), "localhost:9090"; got != want {
		t.Errorf("urlFor(GRPC, 9090): got %q want %q", got, want)
	}
	if got, want := urlFor("REST", 8080), "http://localhost:8080"; got != want {
		t.Errorf("urlFor(REST, 8080): got %q want %q", got, want)
	}
}

func TestItoaAtoi_Roundtrip(t *testing.T) {
	// Cheap sanity — the in-package itoa/atoi exist to avoid an strconv
	// import and have one consumer (urlFor). Make sure they're not
	// off-by-one.
	for _, n := range []int{0, 1, 9, 10, 99, 100, 65535} {
		if got := atoi(itoa(n)); got != n {
			t.Errorf("itoa/atoi roundtrip for %d: got %d", n, got)
		}
	}
}
