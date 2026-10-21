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

// TestEnvFileToEnv guards the classifier the phased env start relies on: a
// service is a dependent, started in the second wave, when it declares a
// depends_on block in either form. The host ports to allocate are the ${VAR}
// host sides of port mappings, and a file that does not parse comes up in one
// piece.
func TestEnvFileToEnv(t *testing.T) {
	t.Parallel()

	testCases := []struct {
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
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			f := filepath.Join(t.TempDir(), "podman-compose.go.test.yaml")
			mustWrite(t, f, testCase.yaml)
			env := envFile{env: "go", file: f, id: "go"}.toEnv(".")
			if !reflect.DeepEqual(env.Services, testCase.services) || !reflect.DeepEqual(env.Dependents, testCase.dependents) ||
				!reflect.DeepEqual(env.Ports, testCase.ports) {
				t.Errorf("toEnv = services %v, dependents %v, ports %v; want %v, %v, %v",
					env.Services, env.Dependents, env.Ports, testCase.services, testCase.dependents, testCase.ports)
			}
		})
	}
}

// TestGoTests locks in the -count=1 policy and the catch-all. Env-less targets
// omit -count=1 so Go's test cache serves the tight loop, while env-backed
// targets keep it, since their Postgres state is invisible to that cache. A test
// package no scoped env names runs in an env-less, cacheable catch-all. A bare
// go env (podman-compose.go.test.yaml, no path) runs ./... and covers the whole
// module, so it leaves no catch-all.
func TestGoTests(t *testing.T) {
	t.Parallel()

	testCases := []struct {
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
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			mustWrite(t, filepath.Join(dir, "go.mod"), "module x\n\ngo 1.26.4\n")
			for _, pkg := range testCase.tests {
				mustWrite(t, filepath.Join(dir, pkg, "x_test.go"), "package x\n")
			}
			compose := filepath.Join(dir, "compose.yaml")
			mustWrite(t, compose, "services:\n  db:\n    image: postgres\n")
			envs := slices.Clone(testCase.envs)
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
			if !slices.Equal(got, testCase.want) {
				t.Errorf("targets = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestUncoveredGoSelectors(t *testing.T) {
	t.Parallel()

	seg := func(parts ...string) []string { return parts }

	testCases := []struct {
		name    string
		testPkg [][]string
		scoped  [][]string
		want    []string
	}{
		{
			name:    "Success/PkgGoUncoveredByAnInternalEnv",
			testPkg: [][]string{seg("internal", "dao"), seg("pkg", "go")},
			scoped:  [][]string{seg("internal")},
			want:    []string{"./pkg/go"},
		},
		{
			name:    "Success/EverythingCovered",
			testPkg: [][]string{seg("internal", "dao"), seg("pkg", "go")},
			scoped:  [][]string{seg("internal"), seg("pkg", "go")},
			want:    nil,
		},
		{
			name:    "Success/ModuleRootIsItsOwnSelector",
			testPkg: [][]string{{}, seg("cmd", "rest")},
			scoped:  [][]string{seg("internal")},
			want:    []string{".", "./cmd/rest"},
		},
		{
			// A scoped path covers whole segments: pkg/go covers neither
			// pkg/js nor pkg/gox.
			name:    "Success/SiblingOfAScopedTreeStaysUncovered",
			testPkg: [][]string{seg("pkg", "js"), seg("pkg", "gox"), seg("pkg", "go")},
			scoped:  [][]string{seg("pkg", "go")},
			want:    []string{"./pkg/gox", "./pkg/js"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := uncoveredGoSelectors(testCase.testPkg, testCase.scoped)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("uncoveredGoSelectors = %v, want %v", got, testCase.want)
			}
		})
	}
}
