package hostenv_test

import (
	"net"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/hostenv"
)

func TestListen(t *testing.T) {
	t.Parallel()

	listener, port, err := hostenv.Listen(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	require.Positive(t, port)
	require.Equal(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), listener.Addr().String())
}

func TestURL(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		base   string
		expect string
	}{
		{name: "Success/GRPC", base: "GRPC", expect: "localhost:4000"},
		{name: "Success/HTTP", base: "REST", expect: "http://localhost:4000"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expect, hostenv.URL(testCase.base, 4000))
		})
	}
}

func TestPostgres(t *testing.T) {
	t.Parallel()

	// The DSN is checked by parsing it back, so no test holds a URL with
	// credentials, which secret scanners flag.
	testCases := []struct {
		name                     string
		user, password, database string
		expect                   map[string]string
	}{
		{
			name: "Success/Defaults",
			expect: map[string]string{
				"POSTGRES_HOST":     "localhost",
				"POSTGRES_PORT":     "5433",
				"POSTGRES_USER":     "postgres",
				"POSTGRES_PASSWORD": "postgres",
				"POSTGRES_DATABASE": "postgres",
			},
		},
		{
			name: "Success/EscapedCredentials",
			user: "app", password: "p@ss/word", database: "json-keys",
			expect: map[string]string{
				"POSTGRES_HOST":     "localhost",
				"POSTGRES_PORT":     "5433",
				"POSTGRES_USER":     "app",
				"POSTGRES_PASSWORD": "p@ss/word",
				"POSTGRES_DATABASE": "json-keys",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := hostenv.Postgres(testCase.user, testCase.password, testCase.database, 5433)

			dsn, err := url.Parse(got["POSTGRES_DSN"])
			require.NoError(t, err)
			password, _ := dsn.User.Password()
			require.Equal(t, "postgres", dsn.Scheme)
			require.Equal(t, testCase.expect["POSTGRES_USER"], dsn.User.Username())
			require.Equal(t, testCase.expect["POSTGRES_PASSWORD"], password)
			require.Equal(t, "localhost:5433", dsn.Host)
			require.Equal(t, "/"+testCase.expect["POSTGRES_DATABASE"], dsn.Path)
			require.Equal(t, "sslmode=disable", dsn.RawQuery)

			delete(got, "POSTGRES_DSN")
			require.Equal(t, testCase.expect, got)
		})
	}
}
