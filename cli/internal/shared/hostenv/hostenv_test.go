package hostenv_test

import (
	"net"
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

	testCases := []struct {
		name                     string
		user, password, database string
		expect                   map[string]string
	}{
		{
			name: "Success/Defaults",
			expect: map[string]string{
				"POSTGRES_DSN":      "postgres://postgres:postgres@localhost:5433/postgres?sslmode=disable",
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
				"POSTGRES_DSN":      "postgres://app:p%40ss%2Fword@localhost:5433/json-keys?sslmode=disable",
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

			require.Equal(t, testCase.expect, hostenv.Postgres(testCase.user, testCase.password, testCase.database, 5433))
		})
	}
}
