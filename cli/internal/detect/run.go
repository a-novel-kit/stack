package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DetectRun is the run-suite counterpart of [Detect]/[DetectTests]. It
// discovers long-lived entrypoints:
//
//   - Go: every `package main` directory (one target per cmd/…), run via
//     `go run ./<relpath>` from the owning module root.
//   - pnpm: every package.json script named "run" or "run:*".
//
// Each target is wired to a compose env the same way tests are, with one
// addition: if no target-specific builds/podman-compose.<id>.yaml matches, a
// plain builds/podman-compose.yaml (the runtime stack) is used as a fallback.
// Run projects are prefixed "anovel-run-" so they never collide with the
// "anovel-test-" projects of a concurrent test run.
func DetectRun(root string) ([]Target, error) {
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
		targets = append(targets, goRun(absRoot, path)...)
		targets = append(targets, pnpmRun(path, rel)...)
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
		if a.Service != b.Service {
			return a.Service < b.Service
		}
		return a.Name < b.Name
	})
	return targets, nil
}

var packageMainRe = regexp.MustCompile(`(?m)^package\s+main\b`)

// isMainPkg reports whether dir holds a `package main` Go file (a runnable
// entrypoint), without descending into subdirectories.
func isMainPkg(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") ||
			strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err == nil && packageMainRe.Match(src) {
			return true
		}
	}
	return false
}

// moduleRootOf walks up from dir (bounded by absRoot) to the nearest directory
// containing go.mod — the directory `go run` must execute from.
func moduleRootOf(absRoot, dir string) string {
	for {
		if fileExists(filepath.Join(dir, "go.mod")) {
			return dir
		}
		if dir == absRoot || dir == filepath.Dir(dir) {
			return ""
		}
		dir = filepath.Dir(dir)
	}
}

// goRun emits a run target for a Go main package at dir. The owning module
// root (and its rel-to-absRoot path) is resolved here, so the walk's own rel
// is not needed.
func goRun(absRoot, dir string) []Target {
	if !isMainPkg(dir) {
		return nil
	}
	modRoot := moduleRootOf(absRoot, dir)
	if modRoot == "" {
		return nil
	}
	relCmd, _ := filepath.Rel(modRoot, dir)
	pkg := "./" + filepath.ToSlash(relCmd)
	if relCmd == "." {
		pkg = "."
	}
	service := goModuleName(filepath.Join(modRoot, "go.mod"))
	if service == "" {
		service = filepath.Base(modRoot)
	}
	modRel, _ := filepath.Rel(absRoot, modRoot)
	return []Target{{
		Kind:    KindGo,
		Name:    filepath.Base(dir),
		Service: service,
		RelDir:  modRel,
		Dir:     modRoot,
		Detail:  "go run " + pkg,
		Cmd:     "go",
		Args:    []string{runArg, pkg},
		Env:     runEnvFor(modRoot, modRel),
	}}
}

// pnpmRun emits a target per "run"/"run:*" script in dir's package.json.
func pnpmRun(dir, rel string) []Target {
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
	service := pkg.Name
	if service == "" {
		service = filepath.Base(dir)
	}
	var names []string
	for name := range pkg.Scripts {
		if pnpmScript(name, runArg) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	targets := make([]Target, 0, len(names))
	for _, s := range names {
		targets = append(targets, Target{
			Kind:    KindPnpm,
			Name:    s,
			Service: service,
			RelDir:  rel,
			Dir:     dir,
			Detail:  truncate(pkg.Scripts[s], 60),
			Cmd:     string(KindPnpm),
			Args:    []string{runArg, s},
			Env:     runEnvFor(dir, rel),
		})
	}
	return targets
}

// runComposeRe matches a non-test run compose file: podman-compose.<id>.yaml
// (the `.test.yaml` ones are excluded).
var runComposeRe = regexp.MustCompile(`^podman-compose\.([a-z0-9.]+)\.yaml$`)

// runEnvFor resolves the compose env for a run target: a target-specific
// builds/podman-compose.<id>.yaml if present, else the plain runtime
// builds/podman-compose.yaml, else nil.
func runEnvFor(repoRoot, rel string) *ComposeEnv {
	buildsDir := filepath.Join(repoRoot, "builds")
	entries, err := os.ReadDir(buildsDir)
	if err != nil {
		return nil
	}
	fallback := ""
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "podman-compose.yaml" {
			fallback = filepath.Join(buildsDir, name)
			continue
		}
		// Target-specific run env (skip the .test.yaml family).
		if strings.HasSuffix(name, ".test.yaml") {
			continue
		}
		if m := runComposeRe.FindStringSubmatch(name); m != nil {
			file := filepath.Join(buildsDir, name)
			ports, refs := composeParse(file)
			return &ComposeEnv{
				File:    file,
				Project: composeProjectP("anovel-run-", rel, m[1]),
				ID:      m[1],
				Ports:   ports,
				Refs:    refs,
			}
		}
	}
	if fallback == "" {
		return nil
	}
	ports, refs := composeParse(fallback)
	return &ComposeEnv{
		File:    fallback,
		Project: composeProjectP("anovel-run-", rel, runArg),
		ID:      runArg,
		Ports:   ports,
		Refs:    refs,
	}
}
