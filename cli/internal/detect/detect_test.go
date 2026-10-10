package detect

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// mustWrite writes content to path, creating parent directories. It panics on
// failure — a setup error, not a runtime outcome under test.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		panic(err)
	}
}

// TestEnvFileToEnv guards the classifier that drives build.composeUpPhased:
// a service is a "dependent" (second wave) iff it declares a depends_on block,
// in either form. The host ports to allocate are the ${VAR} host sides of port
// mappings, and a file that does not parse comes up in one piece.
func TestEnvFileToEnv(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		yaml       string
		services   []string
		dependents []string
		ports      []string
	}{
		{
			name: "MapAndListDependsOn",
			yaml: `services:
  postgres-x:
    image: postgres
    ports: ["${POSTGRES_PORT}:5432", "5433:5433"]
  seed-x:
    depends_on: [postgres-x]
  service-x:
    depends_on:
      seed-x:
        condition: service_completed_successfully
    ports: ["${REST_PORT}:8080"]
`,
			services:   []string{"postgres-x", "seed-x", "service-x"},
			dependents: []string{"seed-x", "service-x"},
			ports:      []string{"POSTGRES_PORT", "REST_PORT"},
		},
		{
			name:     "DBOnlyNoDependents",
			yaml:     "services:\n  postgres-x:\n    ports: [\"${POSTGRES_PORT}:5432\"]\n",
			services: []string{"postgres-x"},
			ports:    []string{"POSTGRES_PORT"},
		},
		{name: "Malformed", yaml: "services: [unclosed\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := filepath.Join(t.TempDir(), "podman-compose.go.test.yaml")
			mustWrite(t, f, tc.yaml)
			env := envFile{env: "go", file: f, id: "go"}.toEnv(".")
			if !reflect.DeepEqual(env.Services, tc.services) || !reflect.DeepEqual(env.Dependents, tc.dependents) ||
				!reflect.DeepEqual(env.Ports, tc.ports) {
				t.Errorf("toEnv = services %v, dependents %v, ports %v; want %v, %v, %v",
					env.Services, env.Dependents, env.Ports, tc.services, tc.dependents, tc.ports)
			}
		})
	}
}

// TestGoTests locks in the -count=1 policy and the catch-all. Env-less kit-lib
// targets omit -count=1 so Go's test cache serves the tight loop, while
// env-backed targets keep it, since their Postgres state is invisible to that
// cache. A test package no scoped env names gets an env-less, cacheable
// catch-all; before it, that package ran nowhere and the suite reported success.
// A bare go env (podman-compose.go.test.yaml, no path) runs ./... and covers the
// whole module: a spurious catch-all in service-authentication's layout would
// re-run its DB-backed tests with no env and fail.
func TestGoTests(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		tests []string  // test package dirs
		envs  []envFile // go envs over compose.yaml; a nil path is a bare env
		want  []string  // each target's name and args, marked +env when it carries one
	}{
		{name: "EnvLessIsCacheable", want: []string{"x test ./..."}},
		{
			name: "EnvBackedKeepsCount1",
			envs: []envFile{{id: "go.internal", path: []string{"internal"}}},
			want: []string{"go.internal test -count=1 ./internal/... +env"},
		},
		{
			name:  "ScopedEnvCatchAll",
			tests: []string{"internal/dao", "pkg/go"},
			envs:  []envFile{{id: "go.internal", path: []string{"internal"}}},
			want:  []string{"go.internal test -count=1 ./internal/... +env", uncoveredTargetName + " test ./pkg/go"},
		},
		{
			name:  "NoCatchAllWhenFullyCovered",
			tests: []string{"internal/dao", "pkg/go"},
			envs:  []envFile{{id: "go.internal", path: []string{"internal"}}, {id: "go.pkg", path: []string{"pkg", "go"}}},
			want:  []string{"go.internal test -count=1 ./internal/... +env", "go.pkg test -count=1 ./pkg/go/... +env"},
		},
		{
			name:  "BareEnvCoversWholeModule",
			tests: []string{"internal/dao", "cmd/rest"},
			envs:  []envFile{{id: "go"}},
			want:  []string{"go test -count=1 ./... +env"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			mustWrite(t, filepath.Join(dir, "go.mod"), "module x\n\ngo 1.26.4\n")
			for _, pkg := range tc.tests {
				mustWrite(t, filepath.Join(dir, pkg, "x_test.go"), "package x\n")
			}
			compose := filepath.Join(dir, "compose.yaml")
			mustWrite(t, compose, "services:\n  db:\n    image: postgres\n")
			envs := slices.Clone(tc.envs)
			for i := range envs {
				envs[i].env, envs[i].file = string(KindGo), compose
			}

			targets := goTests(dir, ".", envs)
			got := make([]string, len(targets))
			for i, target := range targets {
				got[i] = target.Name + " " + strings.Join(target.Args, " ")
				if target.Env != nil {
					got[i] += " +env"
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("targets = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUncoveredGoSelectors(t *testing.T) {
	t.Parallel()

	seg := func(parts ...string) []string { return parts }

	cases := []struct {
		name    string
		testPkg [][]string
		scoped  [][]string
		want    []string
	}{
		{
			name:    "pkg/go uncovered by an internal-only env",
			testPkg: [][]string{seg("internal", "dao"), seg("pkg", "go")},
			scoped:  [][]string{seg("internal")},
			want:    []string{"./pkg/go"},
		},
		{
			name:    "everything covered yields nothing",
			testPkg: [][]string{seg("internal", "dao"), seg("pkg", "go")},
			scoped:  [][]string{seg("internal"), seg("pkg", "go")},
			want:    nil,
		},
		{
			name:    "the module root is a selector on its own",
			testPkg: [][]string{{}, seg("cmd", "rest")},
			scoped:  [][]string{seg("internal")},
			want:    []string{".", "./cmd/rest"},
		},
		{
			// pkg is not covered by pkg/go — the prefix must match whole segments,
			// not a string prefix.
			name:    "a sibling under a partly-scoped tree stays uncovered",
			testPkg: [][]string{seg("pkg", "js"), seg("pkg", "go")},
			scoped:  [][]string{seg("pkg", "go")},
			want:    []string{"./pkg/js"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := uncoveredGoSelectors(c.testPkg, c.scoped)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("uncoveredGoSelectors = %v, want %v", got, c.want)
			}
		})
	}
}
