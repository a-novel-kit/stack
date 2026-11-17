package detect

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
)

// TestEnvFile guards the classifier the phased env start relies on: a service
// is a dependent, started in the second wave, when it declares a depends_on
// block in either form. The host ports to allocate are the ${VAR} host sides
// of port mappings, a service with a build section names its image after the
// checkout's project, and a file that does not parse comes up in one piece.
func TestEnvFile(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		yaml       string
		services   []string
		dependents []string
		ports      []string
		built      []string // services whose image the env builds
	}{
		{
			name: "Success/MapAndListDependsOn",
			yaml: `services:
  postgres-x:
    image: postgres
    ports: ["${POSTGRES_PORT}:5432", "5433:5433"]
  seed-x:
    depends_on: [postgres-x]
  service-x:
    build:
      context: ..
      dockerfile: ./builds/x.Dockerfile
    depends_on:
      seed-x:
        condition: service_completed_successfully
    ports: ["${REST_PORT}:8080"]
`,
			services:   []string{"postgres-x", "seed-x", "service-x"},
			dependents: []string{"seed-x", "service-x"},
			ports:      []string{"POSTGRES_PORT", "REST_PORT"},
			built:      []string{"service-x"},
		},
		{
			name:     "Success/DBOnlyNoDependents",
			yaml:     "services:\n  postgres-x:\n    ports: [\"${POSTGRES_PORT}:5432\"]\n",
			services: []string{"postgres-x"},
			ports:    []string{"POSTGRES_PORT"},
		},
		{name: "Error/Malformed", yaml: "services: [unclosed\n"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			fsutiltest.Write(t, dir, map[string]string{"podman-compose.go.test.yaml": testCase.yaml})
			f := filepath.Join(dir, "podman-compose.go.test.yaml")
			env := envFile{env: "go", file: f, id: "go"}.toEnv(dir)
			require.Equal(t, testCase.services, env.Services)
			require.Equal(t, testCase.dependents, env.Dependents)
			require.Equal(t, testCase.ports, env.Ports)
			var images []string
			for _, service := range testCase.built {
				images = append(images, "localhost/"+env.Project+"_"+service+":latest")
			}
			require.Equal(t, images, env.Images)
		})
	}
}

// TestComposeProject pins the env names that keep runs apart. One checkout
// keeps its names across runs, while two repositories, two worktrees of one
// repository, or two envs of one checkout never share one. Every name stays in
// compose's project-name charset and starts with the directory it covers.
func TestComposeProject(t *testing.T) {
	t.Parallel()

	validName := regexp.MustCompile(`^anovel-test-[a-z0-9]+(-[a-z0-9]+)*$`)

	type env struct{ dir, id string }

	testCases := []struct {
		name  string
		a, b  env
		same  bool
		wantA string // pattern a's name matches; empty skips the check
	}{
		{
			name:  "Success/SameCheckoutAcrossRuns",
			a:     env{"/w/service-genai", "go.internal"},
			b:     env{"/w/service-genai", "go.internal"},
			same:  true,
			wantA: `^anovel-test-service-genai-[0-9a-f]{8}-go-internal$`,
		},
		{
			name: "Success/TwoRepositories",
			a:    env{"/w/service-genai", "go.internal"},
			b:    env{"/w/service-template", "go.internal"},
		},
		{
			name: "Success/TwoWorktreesOfOneRepository",
			a:    env{"/w/service-genai", "go.internal"},
			b:    env{"/w/tmp/fix/service-genai", "go.internal"},
		},
		{
			name: "Success/TwoEnvsOfOneCheckout",
			a:    env{"/w/service-genai", "go.internal"},
			b:    env{"/w/service-genai", "go.pkg"},
		},
		{
			name: "Success/PathsThatSlugAlike",
			a:    env{"/w/a-b/cli", "go"},
			b:    env{"/w/a_b/cli", "go"},
		},
		{
			name:  "Success/UnsafeDirectoryName",
			a:     env{"/w/My Repo.v2", "pnpm"},
			b:     env{"/w/my-repo-v2", "pnpm"},
			wantA: `^anovel-test-my-repo-v2-[0-9a-f]{8}-pnpm$`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			a := composeProject(testCase.a.dir, testCase.a.id)
			b := composeProject(testCase.b.dir, testCase.b.id)
			require.Regexp(t, validName, a)
			require.Regexp(t, validName, b)
			if testCase.wantA != "" {
				require.Regexp(t, testCase.wantA, a)
			}
			if testCase.same {
				require.Equal(t, a, b)
			} else {
				require.NotEqual(t, a, b)
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
		{name: "Success/EnvLessIsCacheable", want: []string{"x test ./..."}},
		{
			name: "Success/EnvBackedKeepsCount1",
			envs: []envFile{{id: "go.internal", path: []string{"internal"}}},
			want: []string{"go.internal test -count=1 ./internal/... +env"},
		},
		{
			name:  "Success/ScopedEnvCatchAll",
			tests: []string{"internal/dao", "pkg/go"},
			envs:  []envFile{{id: "go.internal", path: []string{"internal"}}},
			want:  []string{"go.internal test -count=1 ./internal/... +env", uncoveredTargetName + " test ./pkg/go"},
		},
		{
			name:  "Success/NoCatchAllWhenFullyCovered",
			tests: []string{"internal/dao", "pkg/go"},
			envs:  []envFile{{id: "go.internal", path: []string{"internal"}}, {id: "go.pkg", path: []string{"pkg", "go"}}},
			want:  []string{"go.internal test -count=1 ./internal/... +env", "go.pkg test -count=1 ./pkg/go/... +env"},
		},
		{
			name:  "Success/BareEnvCoversWholeModule",
			tests: []string{"internal/dao", "cmd/rest"},
			envs:  []envFile{{id: "go"}},
			want:  []string{"go test -count=1 ./... +env"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			files := map[string]string{
				"go.mod":       "module x\n\ngo 1.26.4\n",
				"compose.yaml": "services:\n  db:\n    image: postgres\n",
			}
			for _, pkg := range testCase.tests {
				files[pkg+"/x_test.go"] = "package x\n"
			}
			fsutiltest.Write(t, dir, files)
			compose := filepath.Join(dir, "compose.yaml")
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
			require.Equal(t, testCase.want, got)
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

			require.Equal(t, testCase.want, uncoveredGoSelectors(testCase.testPkg, testCase.scoped))
		})
	}
}
