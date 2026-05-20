package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	ignored := gitIgnoredDirs(absRoot)
	var targets []Target

	walkErr := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if skipDir(absRoot, path, d.Name(), ignored) {
			return filepath.SkipDir
		}

		rel, _ := filepath.Rel(absRoot, path)
		envs := composeEnvs(path)

		targets = append(targets, goTests(path, rel, envs)...)
		targets = append(targets, pnpmTests(path, rel, envs)...)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	sort.SliceStable(targets, func(i, j int) bool {
		a, b := targets[i], targets[j]
		if a.Kind != b.Kind {
			return kindOrder(a.Kind) < kindOrder(b.Kind)
		}
		if a.RelDir != b.RelDir {
			return a.RelDir < b.RelDir
		}
		return a.Name < b.Name
	})
	return targets, nil
}

// envFile is one parsed builds/podman-compose.<id>.test.yaml.
type envFile struct {
	env  string   // "go" / "pnpm"
	path []string // dotted test-path segments after the env ("internal" → ["internal"])
	file string   // absolute path to the YAML
	id   string   // full identifier ("go.internal")
}

var composeNameRe = regexp.MustCompile(`^podman-compose\.([a-z0-9.]+)\.test\.yaml$`)

// composeEnvs reads dir/builds and returns every test-env compose file it
// recognises (parsed into env + dotted test path).
func composeEnvs(dir string) []envFile {
	entries, err := os.ReadDir(filepath.Join(dir, "builds"))
	if err != nil {
		return nil
	}
	var out []envFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := composeNameRe.FindStringSubmatch(e.Name())
		if m == nil {
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

// composeProject builds a podman-safe (lowercase, [a-z0-9-]) project name that
// is unique per (location, env id) so concurrent test targets never share a
// compose project.
var nonProjectChar = regexp.MustCompile(`[^a-z0-9]+`)

func composeProject(rel, id string) string {
	return composeProjectP("anovel-test-", rel, id)
}

// composeProjectP is composeProject with a caller-chosen prefix, so `run`
// projects (anovel-run-…) never collide with `test` projects (anovel-test-…)
// even for the same repo/env.
func composeProjectP(prefix, rel, id string) string {
	loc := rel
	if loc == "." {
		loc = "root"
	}
	slug := nonProjectChar.ReplaceAllString(strings.ToLower(loc+"-"+id), "-")
	return prefix + strings.Trim(slug, "-")
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
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// composeParse reads the compose file once and returns the distinct host-side
// port vars and the distinct set of every interpolated var.
func composeParse(file string) ([]string, []string) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, nil
	}
	src := string(raw)
	return distinct(hostPortVar, src), distinct(anyVar, src)
}

// profilesHeadRe matches a compose service heading at the canonical 2-space
// indent: e.g. `  service-json-keys-rest:`.
var profilesHeadRe = regexp.MustCompile(`^  ([a-zA-Z0-9_-]+):\s*(?:#.*)?$`)

// profilesLineRe matches the inline-list profile line this codebase uses:
// `    profiles: ["rest"]`. Multi-line YAML profile lists are not supported
// (none in this codebase).
var profilesLineRe = regexp.MustCompile(`^    profiles:\s*\[(.+?)\]\s*(?:#.*)?$`)

// profilesItemRe extracts individual profile names from the inline list,
// tolerating bare, double- and single-quoted forms.
var profilesItemRe = regexp.MustCompile(`"([^"]+)"|'([^']+)'|([A-Za-z0-9_-]+)`)

// composeProfiles returns a profile-name → compose-service-name map for the
// inline-list `profiles: ["x"]` form. Used to map a run-target name to the
// compose service that would run it in dockerised mode.
func composeProfiles(file string) map[string]string {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	var svc string
	for _, line := range strings.Split(string(raw), "\n") {
		if m := profilesHeadRe.FindStringSubmatch(line); m != nil {
			svc = m[1]
			continue
		}
		if m := profilesLineRe.FindStringSubmatch(line); m != nil && svc != "" {
			for _, it := range profilesItemRe.FindAllStringSubmatch(m[1], -1) {
				p := it[1]
				if p == "" {
					p = it[2]
				}
				if p == "" {
					p = it[3]
				}
				if p != "" {
					out[p] = svc
				}
			}
		}
	}
	return out
}

func (f envFile) toEnv(rel string) *ComposeEnv {
	ports, refs := composeParse(f.file)
	return &ComposeEnv{
		File:    f.file,
		Project: composeProject(rel, f.id),
		ID:      f.id,
		Ports:   ports,
		Refs:    refs,
	}
}

// goTests emits the Go test target(s) for a module at dir.
func goTests(dir, rel string, envs []envFile) []Target {
	if !fileExists(filepath.Join(dir, "go.mod")) {
		return nil
	}
	module := goModuleName(filepath.Join(dir, "go.mod"))
	if module == "" {
		module = filepath.Base(dir)
	}

	var goEnvs []envFile
	for _, e := range envs {
		if e.env == string(KindGo) {
			goEnvs = append(goEnvs, e)
		}
	}

	// No env files → a single self-contained `go test ./...` (kit libs).
	if len(goEnvs) == 0 {
		return []Target{{
			Kind:   KindGo,
			Name:   module,
			RelDir: rel,
			Dir:    dir,
			Detail: "go test " + pkgAll,
			Cmd:    string(KindGo),
			Args:   []string{testArg, "-count=1", pkgAll},
		}}
	}

	// One target per env file, scoped to the path it covers.
	targets := make([]Target, 0, len(goEnvs))
	for _, e := range goEnvs {
		sel := pkgAll
		if len(e.path) > 0 {
			sel = "./" + strings.Join(e.path, "/") + "/..."
		}
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
	return targets
}

// pnpmTests emits a target per "test"/"test:*" script, attaching a matching
// pnpm compose env when one exists.
func pnpmTests(dir, rel string, envs []envFile) []Target {
	pkgPath := filepath.Join(dir, "package.json")
	if !fileExists(pkgPath) {
		return nil
	}
	raw, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil
	}
	var pkg packageJSON
	if json.Unmarshal(raw, &pkg) != nil {
		return nil
	}

	scripts := make([]string, 0, len(pkg.Scripts))
	for name := range pkg.Scripts {
		if pnpmScript(name, testArg) {
			scripts = append(scripts, name)
		}
	}
	sort.Strings(scripts)

	targets := make([]Target, 0, len(scripts))
	for _, s := range scripts {
		t := Target{
			Kind:   KindPnpm,
			Name:   s,
			RelDir: rel,
			Dir:    dir,
			Detail: truncate(pkg.Scripts[s], 60),
			Cmd:    string(KindPnpm),
			Args:   []string{runArg, s},
		}
		// "test" ↔ pnpm (no path); "test:rest" ↔ pnpm.rest.
		want := strings.TrimPrefix(s, testArg+":")
		for _, e := range envs {
			if e.env != string(KindPnpm) {
				continue
			}
			if (s == testArg && len(e.path) == 0) ||
				(s != testArg && strings.Join(e.path, ":") == want) {
				t.Env = e.toEnv(rel)
				t.Detail += "  ·  env " + filepath.Base(e.file)
				break
			}
		}
		targets = append(targets, t)
	}
	return targets
}
