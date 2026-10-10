package env

import (
	"reflect"
	"testing"
)

// Tests for the substitution-rule primitives the env builder hangs off. They
// are pure functions, and the daemon's cross-service env wiring reads through
// each of them on every Acquire.

func TestServicePrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"service-json-keys", "SERVICE_JSON_KEYS"},
		{"service-authentication", "SERVICE_AUTHENTICATION"},
		// A bare name without hyphens is still uppercased, covering a
		// service that skips the service-X convention.
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

func TestResolveOwner(t *testing.T) {
	// The services list is sorted longest-first, the shape SetServices hands
	// out.
	services := []string{
		"service-authentication",
		"service-template-extra",
		"service-json-keys",
		"service-template",
	}
	cases := []struct {
		varName   string
		wantOwner string
		wantLocal string
	}{
		{"SERVICE_JSON_KEYS_GRPC_PORT", "service-json-keys", "GRPC_PORT"},
		{"SERVICE_AUTHENTICATION_REST_PORT", "service-authentication", "REST_PORT"},
		// An unprefixed name yields an empty owner and itself as the local.
		{"POSTGRES_PORT", "", "POSTGRES_PORT"},
		// A var that merely starts with a service prefix still resolves to
		// that service: naming one SERVICE_JSON_KEYS_X owns the consequence.
		{"SERVICE_JSON_KEYS_PORT", "service-json-keys", "PORT"},
		// When two service names share a prefix the longer must match first, or
		// `service-template-extra/X` resolves against `service-template` and
		// silently misroutes.
		{"SERVICE_TEMPLATE_EXTRA_PORT", "service-template-extra", "PORT"},
	}
	for _, c := range cases {
		gotOwner, gotLocal := resolveOwner(c.varName, services)
		if gotOwner != c.wantOwner || gotLocal != c.wantLocal {
			t.Errorf("resolveOwner(%q): got (%q, %q) want (%q, %q)",
				c.varName, gotOwner, gotLocal, c.wantOwner, c.wantLocal)
		}
	}
}

// TestVarKinds pins the suffix rules: a var is allocated, host or URL kind only
// with its `_PORT`, `_HOST` or `_URL` suffix, so a bare PORT allocates nothing.
func TestVarKinds(t *testing.T) {
	cases := []struct {
		in              string
		port, host, url bool
	}{
		{"REST_PORT", true, false, false},
		{"GRPC_PORT", true, false, false},
		{"SMTP_PORT", true, false, false},
		{"PORT", false, false, false},
		{"REST_PORT_EXTRA", false, false, false},
		{"REST_HOST", false, true, false},
		{"HOST", false, false, false},
		{"REST_URL", false, false, true},
		{"URL", false, false, false},
		{"", false, false, false},
	}
	for _, c := range cases {
		if port, host, url := isAllocatedKind(c.in), isHostKind(c.in), isURLKind(c.in); port != c.port || host != c.host || url != c.url {
			t.Errorf("kinds(%q): got port=%v host=%v url=%v, want %v/%v/%v", c.in, port, host, url, c.port, c.host, c.url)
		}
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
	// A gRPC URL stays schemeless: `localhost:port` is what grpc-go's Dial
	// takes. REST keeps its http:// scheme.
	if got, want := urlFor("GRPC", 9090), "localhost:9090"; got != want {
		t.Errorf("urlFor(GRPC, 9090): got %q want %q", got, want)
	}
	if got, want := urlFor("REST", 8080), "http://localhost:8080"; got != want {
		t.Errorf("urlFor(REST, 8080): got %q want %q", got, want)
	}
}
