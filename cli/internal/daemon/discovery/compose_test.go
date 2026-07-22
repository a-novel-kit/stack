package discovery

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// Compose YAML parser tests. The polymorphic decoders for depends_on and
// environment accept both the short and the long YAML form, and each shape is
// covered here so a cleanup cannot drop one.

func TestComposeDependsOn_ShortForm(t *testing.T) {
	src := `
depends_on:
  - postgres
  - mailserver
`
	var v struct {
		DependsOn composeDependsOn `yaml:"depends_on"`
	}
	if err := yaml.Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	if _, ok := v.DependsOn["postgres"]; !ok {
		t.Errorf("short form: missing 'postgres' dep, got %+v", v.DependsOn)
	}
	if _, ok := v.DependsOn["mailserver"]; !ok {
		t.Errorf("short form: missing 'mailserver' dep, got %+v", v.DependsOn)
	}
	// Short form entries carry no condition.
	if v.DependsOn["postgres"].Condition != "" {
		t.Errorf("short form: condition should be empty, got %q", v.DependsOn["postgres"].Condition)
	}
}

func TestComposeDependsOn_LongForm(t *testing.T) {
	src := `
depends_on:
  postgres:
    condition: service_healthy
  init:
    condition: service_completed_successfully
`
	var v struct {
		DependsOn composeDependsOn `yaml:"depends_on"`
	}
	if err := yaml.Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	if v.DependsOn["postgres"].Condition != "service_healthy" {
		t.Errorf("long form: postgres condition got %q want service_healthy",
			v.DependsOn["postgres"].Condition)
	}
	if v.DependsOn["init"].Condition != "service_completed_successfully" {
		t.Errorf("long form: init condition wrong: %+v", v.DependsOn["init"])
	}
}

func TestComposeDependsOn_RejectsScalar(t *testing.T) {
	// A scalar matches neither the short nor the long form, so it errors.
	src := `depends_on: just-a-string`
	var v struct {
		DependsOn composeDependsOn `yaml:"depends_on"`
	}
	if err := yaml.Unmarshal([]byte(src), &v); err == nil {
		t.Errorf("scalar depends_on should error, got %+v", v.DependsOn)
	}
}

func TestComposeEnv_MapForm(t *testing.T) {
	src := `
environment:
  POSTGRES_USER: postgres
  POSTGRES_DSN: "postgres://${USER}:${PASS}@host:5432/db"
`
	var v struct {
		Environment composeEnv `yaml:"environment"`
	}
	if err := yaml.Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	if v.Environment["POSTGRES_USER"] != "postgres" {
		t.Errorf("map env: got %q want postgres", v.Environment["POSTGRES_USER"])
	}
	if v.Environment["POSTGRES_DSN"] != "postgres://${USER}:${PASS}@host:5432/db" {
		t.Errorf("map env preserves ${} refs as-is, got %q", v.Environment["POSTGRES_DSN"])
	}
}

func TestComposeEnv_ListForm(t *testing.T) {
	// Docker-compose accepts KEY=value list form alongside map form.
	src := `
environment:
  - POSTGRES_USER=postgres
  - POSTGRES_DSN=postgres://u:p@h/d
`
	var v struct {
		Environment composeEnv `yaml:"environment"`
	}
	if err := yaml.Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	if v.Environment["POSTGRES_USER"] != "postgres" {
		t.Errorf("list env: got %q want postgres", v.Environment["POSTGRES_USER"])
	}
	if v.Environment["POSTGRES_DSN"] != "postgres://u:p@h/d" {
		t.Errorf("list env: DSN got %q", v.Environment["POSTGRES_DSN"])
	}
}

func TestComposeEnv_ListSplitOnFirstEquals(t *testing.T) {
	// A value holding '=', such as a DSN with ?key=val, splits on the first
	// one only; splitting further truncates it.
	src := `
environment:
  - DSN=postgres://u:p@h/d?sslmode=disable
`
	var v struct {
		Environment composeEnv `yaml:"environment"`
	}
	if err := yaml.Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	if v.Environment["DSN"] != "postgres://u:p@h/d?sslmode=disable" {
		t.Errorf("split-on-first-= broken: got %q", v.Environment["DSN"])
	}
}

func TestParseComposeFile_FullDocument(t *testing.T) {
	// End to end: a representative compose file written to a tempdir must
	// parse into the expected top-level shape, services, volumes, and
	// networks at once.
	tmp := t.TempDir()
	path := filepath.Join(tmp, "podman-compose.yaml")
	src := `
services:
  postgres-svc:
    build:
      context: ..
      dockerfile: ./builds/database.Dockerfile
    ports:
      - "${POSTGRES_PORT}:5432"
    environment:
      POSTGRES_PASSWORD: postgres
    healthcheck:
      test: ["CMD", "pg_isready"]
      interval: 5s
      timeout: 3s
      retries: 5
  svc-rest:
    profiles: ["rest"]
    ports:
      - "${REST_PORT}:8080"
    depends_on:
      postgres-svc:
        condition: service_healthy
    environment:
      POSTGRES_DSN: "postgres://postgres:postgres@postgres-svc:5432/postgres?sslmode=disable"
networks:
  api:
volumes:
  postgres-data:
`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cf, err := parseComposeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cf.Services["postgres-svc"]; !ok {
		t.Fatal("missing postgres-svc")
	}
	if _, ok := cf.Services["svc-rest"]; !ok {
		t.Fatal("missing svc-rest")
	}
	if _, ok := cf.Volumes["postgres-data"]; !ok {
		t.Fatal("missing postgres-data volume")
	}
	if _, ok := cf.Networks["api"]; !ok {
		t.Fatal("missing api network")
	}
	// Spot-checks on each side of the parser:
	pg := cf.Services["postgres-svc"]
	if pg.Healthcheck == nil || len(pg.Healthcheck.Test) == 0 {
		t.Error("healthcheck not parsed")
	}
	if len(pg.Ports) != 1 || pg.Ports[0] != "${POSTGRES_PORT}:5432" {
		t.Errorf("ports parsed wrong: %+v", pg.Ports)
	}
	rest := cf.Services["svc-rest"]
	if len(rest.Profiles) != 1 || rest.Profiles[0] != "rest" {
		t.Errorf("profiles wrong: %+v", rest.Profiles)
	}
	if rest.DependsOn["postgres-svc"].Condition != "service_healthy" {
		t.Errorf("depends_on condition not parsed: %+v", rest.DependsOn)
	}
}

func TestParseComposeFile_MissingFile(t *testing.T) {
	_, err := parseComposeFile("/no/such/file")
	if err == nil {
		t.Error("missing file should error")
	}
}
