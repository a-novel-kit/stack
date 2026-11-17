package compose_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/compose"
	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
)

// TestReadFile pins the parsed shape, including both YAML forms of the
// polymorphic depends_on and environment keys.
func TestReadFile(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		yaml    string
		want    *compose.File
		wantErr bool
	}{
		{
			name: "Success/FullDocument",
			yaml: `
services:
  postgres-svc:
    build: {context: .., dockerfile: ./builds/database.Dockerfile}
    ports: ["${POSTGRES_PORT}:5432"]
    environment: {POSTGRES_DSN: "postgres://${USER}@h/db?sslmode=disable"}
    healthcheck: {test: ["CMD", "pg_isready"]}
  svc-rest:
    profiles: ["rest"]
    depends_on: {postgres-svc: {condition: service_healthy}, init: {condition: service_completed_successfully}}
    environment: ["DSN=postgres://u:p@h/d?sslmode=disable", "USER=postgres"]
  svc-job:
    depends_on: [postgres-svc, mailserver]
volumes:
  postgres-data:
`,
			want: &compose.File{
				Services: map[string]compose.Service{
					"postgres-svc": {
						Build:       &compose.Build{Context: "..", Dockerfile: "./builds/database.Dockerfile"},
						Ports:       []string{"${POSTGRES_PORT}:5432"},
						Environment: compose.Environment{"POSTGRES_DSN": "postgres://${USER}@h/db?sslmode=disable"},
						Healthcheck: &struct{}{},
					},
					"svc-rest": {
						Profiles:  []string{"rest"},
						DependsOn: compose.DependsOn{"postgres-svc": {}, "init": {}},
						// The list form splits on the first '=' only.
						Environment: compose.Environment{"DSN": "postgres://u:p@h/d?sslmode=disable", "USER": "postgres"},
					},
					"svc-job": {DependsOn: compose.DependsOn{"postgres-svc": {}, "mailserver": {}}},
				},
				Volumes: map[string]struct{}{"postgres-data": {}},
			},
		},
		{name: "Error/ScalarDependsOn", yaml: "services:\n  x:\n    depends_on: just-a-string\n", wantErr: true},
		{name: "Error/ScalarEnvironment", yaml: "services:\n  x:\n    environment: just-a-string\n", wantErr: true},
		{name: "Error/MissingFile", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if testCase.yaml != "" {
				fsutiltest.Write(t, dir, map[string]string{"podman-compose.yaml": testCase.yaml})
			}
			got, err := compose.ReadFile(filepath.Join(dir, "podman-compose.yaml"))
			if testCase.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.want, got)
		})
	}
}

func TestRefs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		in   string
		want []string
	}{
		{name: "Success/Single", in: "${POSTGRES_PORT}", want: []string{"POSTGRES_PORT"}},
		{name: "Success/Several", in: "${SMTP_HOST}:${SMTP_PORT}", want: []string{"SMTP_HOST", "SMTP_PORT"}},
		{name: "Success/WithDefault", in: "${X:-fallback}", want: []string{"X"}},
		{
			name: "Success/FirstSeenOrder",
			in:   "postgres://${USER}:${PASS}@${HOST}:${PORT}/db",
			want: []string{"USER", "PASS", "HOST", "PORT"},
		},
		{name: "Success/Distinct", in: "${X}/${X}", want: []string{"X"}},
		{name: "Success/NoRef", in: "plain-literal"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, compose.Refs(testCase.in))
		})
	}
}

func TestSubstitute(t *testing.T) {
	t.Parallel()

	vars := map[string]string{"HOST": "localhost", "PORT": "5432", "EMPTY": ""}
	testCases := []struct {
		name string
		in   string
		want string
	}{
		{name: "Success/Known", in: "${HOST}:${PORT}", want: "localhost:5432"},
		{name: "Success/KnownIgnoresDefault", in: "${HOST:-ignored}", want: "localhost"},
		// An unknown or empty reference takes its default, as compose's `:-` does.
		{name: "Success/UnknownTakesDefault", in: "${UNKNOWN:-placeholder}", want: "placeholder"},
		{name: "Success/EmptyTakesDefault", in: "${EMPTY:-placeholder}", want: "placeholder"},
		// An unknown reference resolves empty, as compose does.
		{name: "Success/UnknownResolvesEmpty", in: "prefix ${UNKNOWN} suffix", want: "prefix  suffix"},
		{name: "Success/NoRef", in: "plain", want: "plain"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, compose.Substitute(testCase.in, vars))
		})
	}
}

func TestHostPort(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		in             string
		wantName       string
		wantPort       string
		wantAllocation bool
	}{
		{
			name: "Success/Variable", in: "${POSTGRES_PORT}:5432",
			wantName: "POSTGRES_PORT", wantPort: "5432", wantAllocation: true,
		},
		{
			name: "Success/DefaultAndSpaces", in: " ${REST_PORT:-8080}:8080 ",
			wantName: "REST_PORT", wantPort: "8080", wantAllocation: true,
		},
		{name: "Success/Literal", in: "5432:5432"},
		{name: "Success/ContainerPath", in: "${DATA}:/var/lib/data"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			name, port, ok := compose.HostPort(testCase.in)
			require.Equal(t, testCase.wantName, name)
			require.Equal(t, testCase.wantPort, port)
			require.Equal(t, testCase.wantAllocation, ok)
		})
	}
}
