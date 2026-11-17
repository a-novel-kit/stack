package env

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveOwner(t *testing.T) {
	t.Parallel()

	// The services list is sorted longest-first, the shape SetServices hands
	// out.
	services := []string{
		"service-authentication",
		"service-template-extra",
		"service-json-keys",
		"service-template",
	}

	testCases := []struct {
		name      string
		varName   string
		wantOwner string
		wantLocal string
	}{
		{name: "Success/CrossService", varName: "SERVICE_JSON_KEYS_GRPC_PORT", wantOwner: "service-json-keys", wantLocal: "GRPC_PORT"},
		{name: "Success/OtherService", varName: "SERVICE_AUTHENTICATION_REST_PORT", wantOwner: "service-authentication", wantLocal: "REST_PORT"},
		// An unprefixed name yields an empty owner and itself as the local.
		{name: "Success/Unprefixed", varName: "POSTGRES_PORT", wantOwner: "", wantLocal: "POSTGRES_PORT"},
		// A var that merely starts with a service prefix still resolves to
		// that service: naming one SERVICE_JSON_KEYS_X owns the consequence.
		{name: "Success/BareVarAfterPrefix", varName: "SERVICE_JSON_KEYS_PORT", wantOwner: "service-json-keys", wantLocal: "PORT"},
		// When two service names share a prefix the longer must match first, or
		// `service-template-extra/X` resolves against `service-template` and
		// silently misroutes.
		{name: "Success/LongestPrefixWins", varName: "SERVICE_TEMPLATE_EXTRA_PORT", wantOwner: "service-template-extra", wantLocal: "PORT"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			owner, local := resolveOwner(testCase.varName, services)
			require.Equal(t, testCase.wantOwner, owner)
			require.Equal(t, testCase.wantLocal, local)
		})
	}
}

// TestIsAllocatedKind pins the suffix rule: a var is allocated only with its
// `_PORT` suffix, so a bare PORT allocates nothing.
func TestIsAllocatedKind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		in   string
		want bool
	}{
		{name: "Success/RestPort", in: "REST_PORT", want: true},
		{name: "Success/GrpcPort", in: "GRPC_PORT", want: true},
		{name: "Success/SmtpPort", in: "SMTP_PORT", want: true},
		{name: "Success/BarePort", in: "PORT"},
		{name: "Success/PortNotSuffix", in: "REST_PORT_EXTRA"},
		{name: "Success/Host", in: "REST_HOST"},
		{name: "Success/BareHost", in: "HOST"},
		{name: "Success/URL", in: "REST_URL"},
		{name: "Success/BareURL", in: "URL"},
		{name: "Success/Empty", in: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, isAllocatedKind(testCase.in))
		})
	}
}

func TestDerivedFor(t *testing.T) {
	t.Parallel()

	require.Equal(t, map[string]string{
		"REST_PORT": "12345",
		"REST_HOST": "localhost",
		"REST_URL":  "http://localhost:12345",
	}, derivedFor("REST_PORT", 12345))
}

// TestUrlFor pins the URL forms: a gRPC URL stays schemeless, since
// `localhost:port` is what grpc-go's Dial takes, and REST keeps its http://
// scheme.
func TestUrlFor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		base string
		port int
		want string
	}{
		{name: "Success/GRPCSchemeless", base: "GRPC", port: 9090, want: "localhost:9090"},
		{name: "Success/HTTP", base: "REST", port: 8080, want: "http://localhost:8080"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, urlFor(testCase.base, testCase.port))
		})
	}
}
