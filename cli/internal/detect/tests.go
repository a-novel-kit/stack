package detect

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// DetectTests is the test-suite counterpart of [Detect]. It discovers:
//
//   - Go test targets — one per Go module. When the module ships
//     builds/podman-compose.go[.<path>].test.yaml files, one target per file
//     scoped to that path (`go test ./<path>/...`) with the compose env
//     attached; otherwise a single env-less `go test ./...`.
//   - pnpm test targets — every package.json script named "test" or "test:*".
//     A builds/podman-compose.pnpm[.<path>].test.yaml whose path matches the
//     script is attached as that script's env.
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
		out = append(out, envFile{
			env:  segs[0],
			path: segs[1:],
			file: filepath.Join(dir, "builds", e.Name()),
			id:   m[1],
		})
	}
	return out
}

var nonProjectChar = regexp.MustCompile(`[^a-z0-9]+`)

// composeProject builds a podman-safe (lowercase, [a-z0-9-]) project name that
// is unique per (location, env id), so concurrent test targets never share a
// compose project.
func composeProject(rel, id string) string {
	loc := rel
	if loc == "." {
		loc = "root"
	}
	slug := nonProjectChar.ReplaceAllString(strings.ToLower(loc+"-"+id), "-")
	return "anovel-test-" + strings.Trim(slug, "-")
}

// hostPortVar matches a `${NAME}:1234` host→container port mapping. The
// `:digits` after the brace distinguishes a ports entry from environment
// (`KEY: "${NAME}"`) or volumes (`${NAME}:/path`).
var hostPortVar = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}:\d`)

// anyVar matches every `${NAME}` interpolation, ports/env/anything.
var anyVar = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)`)

// distinct returns the first capture group of every match, de-duplicated in
// first-seen order.
func distinct(re *regexp.Regexp, src string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// composeServices lists the services a compose file declares, in source order,
// and the ones among them with a `depends_on:` block. build.composeUpPhased
// starts dependency-free services first and dependents second, so ordering
// never relies on the provider's `depends_on` wait. A file that does not parse
// lists nothing, and its env comes up in one piece.
func composeServices(raw []byte) ([]string, []string) {
	var doc struct {
		Services yaml.Node `yaml:"services"`
	}
	if yaml.Unmarshal(raw, &doc) != nil {
		return nil, nil
	}
	var services, dependents []string
	for i := 0; i+1 < len(doc.Services.Content); i += 2 {
		name := doc.Services.Content[i].Value
		services = append(services, name)
		var svc struct {
			DependsOn yaml.Node `yaml:"depends_on"`
		}
		if doc.Services.Content[i+1].Decode(&svc) == nil && !svc.DependsOn.IsZero() {
			dependents = append(dependents, name)
		}
	}
	return services, dependents
}

func (f envFile) toEnv(rel string) *ComposeEnv {
	raw, _ := os.ReadFile(f.file)
	services, dependents := composeServices(raw)
	return &ComposeEnv{
		File:       f.file,
		Project:    composeProject(rel, f.id),
		ID:         f.id,
		Ports:      distinct(hostPortVar, string(raw)),
		Refs:       distinct(anyVar, string(raw)),
		Services:   services,
		Dependents: dependents,
	}
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

	// No env files means a single self-contained `go test ./...` (kit libs).
	// These have no external state, so Go's test cache is safe: it tracks the
	// sources plus the env vars and files each test consults, and re-runs when
	// any of those change. Omitting -count=1 lets the tight edit-test loop hit
	// that cache.
	if len(goEnvs) == 0 {
		return []Target{{
			Kind:   KindGo,
			Name:   module,
			RelDir: rel,
			Dir:    dir,
			Detail: "go test " + pkgAll,
			Cmd:    string(KindGo),
			Args:   []string{testArg, pkgAll},
		}}
	}

	// One target per env file, scoped to the path it covers. These keep
	// -count=1: their result depends on the Postgres state the compose env
	// stands up, which Go's cache cannot see, so a cached pass could hide a
	// migration or schema change.
	targets := make([]Target, 0, len(goEnvs))

	scoped := make([][]string, 0, len(goEnvs))

	for _, e := range goEnvs {
		sel := pkgAll
		if len(e.path) > 0 {
			sel = "./" + strings.Join(e.path, "/") + "/..."
		}

		// A bare env (empty path) selects ./... and so covers the whole module;
		// its empty prefix matches every package, leaving nothing for the
		// catch-all. Recording every env's path, empty included, is what makes
		// that fall out.
		scoped = append(scoped, e.path)

		targets = append(targets, Target{
			Kind:   KindGo,
			Name:   e.id, // "go.internal" / "go.pkg" — unique within the dir
			RelDir: rel,
			Dir:    dir,
			Detail: "go test " + sel + "  ·  env " + filepath.Base(e.file),
			Cmd:    string(KindGo),
			Args:   []string{testArg, "-count=1", sel},
			Env:    e.toEnv(rel),
		})
	}

	// Every scoped env narrows the run to its own subtree, so a test package
	// under no env's path runs nowhere and reports nothing. This catch-all
	// selects exactly those packages, so adding pkg/go/client_test.go to a
	// module whose only env covers ./internal does not silently skip it.
	//
	// It carries no env: a package that needs external state declares its own
	// env file, so an uncovered one is either cache-safe or fails loudly on the
	// missing state — which surfaces the absent env rather than hiding it. That
	// is also why it omits -count=1.
	if uncovered := uncoveredGoSelectors(goTestPackageDirs(dir), scoped); len(uncovered) > 0 {
		args := append([]string{testArg}, uncovered...)
		targets = append(targets, Target{
			Kind:   KindGo,
			Name:   uncoveredTargetName,
			RelDir: rel,
			Dir:    dir,
			Detail: "go test " + strings.Join(uncovered, " ") + "  ·  no env",
			Cmd:    string(KindGo),
			Args:   args,
		})
	}

	return targets
}

// goTestPackageDirs returns the module-relative segment paths of every directory
// under dir holding a *_test.go file. It prunes the same trees the main scan does
// and stops at any nested go.mod: those packages belong to that module, which is
// discovered on its own, and this is also what keeps a sibling checkout dropped
// under tmp/ — a worktree git does not report as ignored — out of the count. The
// module root itself is the empty path.
func goTestPackageDirs(dir string) [][]string {
	absRoot, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}

	ignored := gitIgnoredDirs(absRoot)
	seen := map[string]struct{}{}

	var pkgs [][]string

	_ = filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // an unreadable subtree is skipped, not fatal to detection
		}

		if entry.IsDir() {
			if skipDir(absRoot, path, entry.Name(), ignored) {
				return filepath.SkipDir
			}

			if path != absRoot && IsFile(filepath.Join(path, "go.mod")) {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		rel, relErr := filepath.Rel(absRoot, filepath.Dir(path))
		if relErr != nil {
			return nil //nolint:nilerr // a path outside the root is not a package of this module
		}

		if _, dup := seen[rel]; dup {
			return nil
		}

		seen[rel] = struct{}{}

		if rel == "." {
			pkgs = append(pkgs, []string{})

			return nil
		}

		pkgs = append(pkgs, strings.Split(rel, string(filepath.Separator)))

		return nil
	})

	return pkgs
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
				t.Env = e.toEnv(rel)
				t.Detail += "  ·  env " + filepath.Base(e.file)
				break
			}
		}
		targets = append(targets, t)
	}
	return targets
}
