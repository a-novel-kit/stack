package detect

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/shared/compose"
)

// DetectTests is the test-suite counterpart of [Detect].
//
//   - A Go module gets one target per builds/podman-compose.go[.<path>].test.yaml
//     file, scoped to that path (`go test ./<path>/...`) with the compose env
//     attached, plus one env-less target for the test packages no file covers.
//     A module without such a file gets a single env-less `go test ./...`.
//   - Each package.json script named "test" or "test:*" is a target. A
//     builds/podman-compose.pnpm[.<path>].test.yaml whose path matches the
//     script is attached as its env.
//
// The env filename convention is `podman-compose.<id>.test.yaml` where <id>
// is the environment ("go"/"pnpm") followed by the dotted test path it covers
// ("go.internal", "go.pkg", "pnpm"); a bare env id covers the whole suite.
func DetectTests(root string) ([]Target, error) {
	return walk(root, func(dir, rel string) []Target {
		envs := composeEnvs(dir)
		return slices.Concat(goTests(dir, rel, envs), pnpmTests(dir, rel, envs))
	})
}

// envFile is one parsed builds/podman-compose.<id>.test.yaml.
type envFile struct {
	env  string   // "go" / "pnpm"
	path []string // dotted test-path segments after the env ("internal" → ["internal"])
	file string   // absolute path to the YAML
	id   string   // full identifier ("go.internal")
}

// uncoveredTargetName identifies the catch-all Go target that runs the test
// packages no scoped env covers.
const uncoveredTargetName = "go.uncovered"

var composeNameRe = regexp.MustCompile(`^podman-compose\.([a-z0-9.]+)\.test\.yaml$`)

// composeEnvs reads dir/builds and returns every test-env compose file it
// recognizes (parsed into env + dotted test path).
func composeEnvs(dir string) []envFile {
	entries, err := os.ReadDir(filepath.Join(dir, "builds"))
	if err != nil {
		return nil
	}
	var out []envFile
	for _, e := range entries {
		m := composeNameRe.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			continue
		}
		segs := strings.Split(m[1], ".")
		parsed := envFile{
			env:  segs[0],
			path: segs[1:],
			file: filepath.Join(dir, "builds", e.Name()),
			id:   m[1],
		}
		out = append(out, parsed)
	}
	return out
}

var nonProjectChar = regexp.MustCompile(`[^a-z0-9]+`)

// composeProject builds a podman-safe (lowercase, [a-z0-9-]) project name for
// env id of the module at the absolute path dir. The name of dir keeps it
// readable, and a hash of dir tells apart two repositories, or two worktrees of
// one, so their runs never share containers.
//
// A checkout keeps its names across runs, which lets the preflight find its own
// leftovers and nothing else.
func composeProject(dir, id string) string {
	sum := sha256.Sum256([]byte(dir))
	loc := filepath.Base(dir) + "-" + hex.EncodeToString(sum[:4])
	slug := nonProjectChar.ReplaceAllString(strings.ToLower(loc+"-"+id), "-")
	return "anovel-test-" + strings.Trim(slug, "-")
}

// toEnv describes the env file f of the module at dir as the runner needs it:
// its services, split into dependency-free ones and dependents, the host ports
// to allocate, and every variable it references. A file that does not parse
// lists nothing, and its env comes up in one piece.
func (f envFile) toEnv(dir string) *ComposeEnv {
	env := &ComposeEnv{File: f.file, Project: composeProject(dir, f.id), ID: f.id}
	raw, err := os.ReadFile(f.file)
	if err != nil {
		return env
	}
	env.Refs = compose.Refs(string(raw))
	parsed, err := compose.Parse(raw)
	if err != nil {
		return env
	}
	env.Services = slices.Sorted(maps.Keys(parsed.Services))
	for _, name := range env.Services {
		svc := parsed.Services[name]
		if len(svc.DependsOn) > 0 {
			env.Dependents = append(env.Dependents, name)
		}
		for _, mapping := range svc.Ports {
			if port, _, ok := compose.HostPort(mapping); ok && !slices.Contains(env.Ports, port) {
				env.Ports = append(env.Ports, port)
			}
		}
	}
	return env
}

// goTests emits the Go test target(s) for a module at dir.
func goTests(dir, rel string, envs []envFile) []Target {
	if !IsFile(filepath.Join(dir, "go.mod")) {
		return nil
	}
	module := goModuleName(dir)

	var goEnvs []envFile
	for _, e := range envs {
		if e.env == string(KindGo) {
			goEnvs = append(goEnvs, e)
		}
	}

	// A module without env files runs a single `go test ./...`, as kit
	// libraries do. It has no external state, so it omits -count=1 and the
	// edit-test loop hits Go's test cache.
	if len(goEnvs) == 0 {
		return []Target{goTarget(dir, rel, module, "go test "+pkgAll, testArg, pkgAll)}
	}

	// One target per env file, scoped to the path it covers.
	targets := make([]Target, 0, len(goEnvs))
	scoped := make([][]string, 0, len(goEnvs))

	for _, e := range goEnvs {
		sel := pkgAll
		if len(e.path) > 0 {
			sel = "./" + strings.Join(e.path, "/") + "/..."
		}

		// A bare env has an empty path. An empty path prefixes every package,
		// so a bare env leaves the catch-all nothing to run.
		scoped = append(scoped, e.path)

		// The env id, such as "go.internal", is unique within the directory.
		// Env-backed targets keep -count=1: Go's test cache cannot see the
		// Postgres state the env stands up.
		detail := "go test " + sel + "  ·  env " + filepath.Base(e.file)
		target := goTarget(dir, rel, e.id, detail, testArg, "-count=1", sel)
		target.Env = e.toEnv(dir)
		targets = append(targets, target)
	}

	// Scoped envs run only their own subtrees. The catch-all runs every test
	// package outside them, such as pkg/go when the only env covers
	// ./internal. It has no env and omits -count=1: a package that needs
	// external state declares its own env file, so an uncovered package is
	// cache-safe or fails on the missing state.
	if uncovered := uncoveredGoSelectors(goTestPackageDirs(dir), scoped); len(uncovered) > 0 {
		detail := "go test " + strings.Join(uncovered, " ") + "  ·  no env"
		args := append([]string{testArg}, uncovered...)
		targets = append(targets, goTarget(dir, rel, uncoveredTargetName, detail, args...))
	}

	return targets
}

// goTestPackageDirs returns the module-relative segment paths of every directory
// under dir holding a *_test.go file. It prunes the same trees the main scan does
// and stops at any nested go.mod: those packages belong to that module, which is
// discovered on its own. That stop also keeps out a sibling checkout dropped
// under tmp/, a worktree git does not report as ignored. The module root itself
// is the empty path.
func goTestPackageDirs(dir string) [][]string {
	absRoot, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}

	ignored := gitIgnoredDirs(absRoot)
	seen := map[string]bool{}
	var pkgs [][]string
	_ = filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // an unreadable subtree is skipped, not fatal to detection
		}
		if entry.IsDir() {
			return moduleDirStep(absRoot, path, entry.Name(), ignored)
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		if rel, err := filepath.Rel(absRoot, filepath.Dir(path)); err == nil && !seen[rel] {
			seen[rel] = true
			pkgs = append(pkgs, pathSegments(rel))
		}
		return nil
	})
	return pkgs
}

// moduleDirStep is the walk's decision on a directory of the module at
// absRoot: it skips the trees the main scan prunes and any nested module.
func moduleDirStep(absRoot, path, name string, ignored map[string]struct{}) error {
	if skipDir(absRoot, path, name, ignored) {
		return filepath.SkipDir
	}
	// A nested go.mod starts another module, discovered on its own.
	if path != absRoot && IsFile(filepath.Join(path, "go.mod")) {
		return filepath.SkipDir
	}
	return nil
}

// pathSegments splits a relative path into its segments, "." being none.
func pathSegments(rel string) []string {
	if rel == "." {
		return []string{}
	}
	return strings.Split(rel, string(filepath.Separator))
}

// uncoveredGoSelectors returns a `go test` selector for each test package no
// scoped path covers. A scoped path covers a package when it is a leading
// segment-run of the package's path, so ["internal"] covers internal/dao but
// not pkg/go.
func uncoveredGoSelectors(testPkgs, scoped [][]string) []string {
	var sels []string
	for _, pkg := range testPkgs {
		covered := slices.ContainsFunc(scoped, func(prefix []string) bool {
			return len(prefix) <= len(pkg) && slices.Equal(pkg[:len(prefix)], prefix)
		})
		switch {
		case covered:
		case len(pkg) == 0:
			sels = append(sels, ".")
		default:
			sels = append(sels, "./"+strings.Join(pkg, "/"))
		}
	}
	slices.Sort(sels)
	return sels
}

// pnpmTests emits a target per "test"/"test:*" script, attaching a matching
// pnpm compose env when one exists: "test" takes the bare pnpm env, "test:rest"
// takes pnpm.rest.
func pnpmTests(dir, rel string, envs []envFile) []Target {
	scripts := pnpmScripts(dir, testArg)
	targets := make([]Target, 0, len(scripts))
	for _, s := range scripts {
		t := pnpmTarget(dir, rel, s)
		want := strings.TrimPrefix(s.name, testArg+":")
		for _, e := range envs {
			if e.env == string(KindPnpm) &&
				((s.name == testArg && len(e.path) == 0) || (s.name != testArg && strings.Join(e.path, ":") == want)) {
				t.Env = e.toEnv(dir)
				t.Detail += "  ·  env " + filepath.Base(e.file)
				break
			}
		}
		targets = append(targets, t)
	}
	return targets
}
