package compose

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestReadFile pins the parsed shape, including both YAML forms of the
// polymorphic depends_on and environment keys.
func TestReadFile(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		yaml    string
		want    *File
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
			want: &File{
				Services: map[string]Service{
					"postgres-svc": {
						Build:       &Build{Context: "..", Dockerfile: "./builds/database.Dockerfile"},
						Ports:       []string{"${POSTGRES_PORT}:5432"},
						Environment: Environment{"POSTGRES_DSN": "postgres://${USER}@h/db?sslmode=disable"},
						Healthcheck: &struct{}{},
					},
					"svc-rest": {
						Profiles:  []string{"rest"},
						DependsOn: DependsOn{"postgres-svc": {}, "init": {}},
						// The list form splits on the first '=' only.
						Environment: Environment{"DSN": "postgres://u:p@h/d?sslmode=disable", "USER": "postgres"},
					},
					"svc-job": {DependsOn: DependsOn{"postgres-svc": {}, "mailserver": {}}},
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

			path := filepath.Join(t.TempDir(), "podman-compose.yaml")
			if testCase.yaml != "" {
				if err := os.WriteFile(path, []byte(testCase.yaml), 0o600); err != nil {
					panic(err)
				}
			}
			got, err := ReadFile(path)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("ReadFile error = %v, wantErr %v", err, testCase.wantErr)
			}
			if !testCase.wantErr && !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("ReadFile =\n%+v\nwant\n%+v", got, testCase.want)
			}
		})
	}
}

func TestRefs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		in   string
		want []string
	}{
		{"${POSTGRES_PORT}", []string{"POSTGRES_PORT"}},
		{"${SMTP_HOST}:${SMTP_PORT}", []string{"SMTP_HOST", "SMTP_PORT"}},
		{"${X:-fallback}", []string{"X"}},
		{"postgres://${USER}:${PASS}@${HOST}:${PORT}/db", []string{"USER", "PASS", "HOST", "PORT"}},
		{"${X}/${X}", []string{"X"}},
		{"plain-literal", nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.in, func(t *testing.T) {
			t.Parallel()
			if got := Refs(testCase.in); !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("Refs(%q) = %v, want %v", testCase.in, got, testCase.want)
			}
		})
	}
}

func TestSubstitute(t *testing.T) {
	t.Parallel()

	vars := map[string]string{"HOST": "localhost", "PORT": "5432"}
	testCases := []struct{ in, want string }{
		{"${HOST}:${PORT}", "localhost:5432"},
		{"${HOST:-ignored}", "localhost"},
		// An unknown reference resolves empty, as compose does.
		{"prefix ${UNKNOWN} suffix", "prefix  suffix"},
		{"plain", "plain"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.in, func(t *testing.T) {
			t.Parallel()
			if got := Substitute(testCase.in, vars); got != testCase.want {
				t.Errorf("Substitute(%q) = %q, want %q", testCase.in, got, testCase.want)
			}
		})
	}
}

func TestHostPort(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		in             string
		name, port     string
		wantAllocation bool
	}{
		{in: "${POSTGRES_PORT}:5432", name: "POSTGRES_PORT", port: "5432", wantAllocation: true},
		{in: " ${REST_PORT:-8080}:8080 ", name: "REST_PORT", port: "8080", wantAllocation: true},
		{in: "5432:5432"},
		{in: "${DATA}:/var/lib/data"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.in, func(t *testing.T) {
			t.Parallel()
			name, port, ok := HostPort(testCase.in)
			if name != testCase.name || port != testCase.port || ok != testCase.wantAllocation {
				t.Errorf("HostPort(%q) = %q, %q, %v; want %q, %q, %v", testCase.in, name, port, ok, testCase.name, testCase.port, testCase.wantAllocation)
			}
		})
	}
}
