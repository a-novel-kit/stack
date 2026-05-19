// Package detect discovers buildable targets under a directory tree.
//
// It recognises three kinds of build, matching the conventions used across the
// a-novel / a-novel-kit repositories:
//
//   - [KindGo]     — any directory containing a go.mod (one target per module,
//     including nested modules).
//   - [KindPnpm]   — any package.json whose "scripts" map has one or more keys
//     starting with "build" (one target per matching script).
//   - [KindPodman] — any builds/ directory containing *.Dockerfile files (one
//     target per Dockerfile). The image tag is derived heuristically; see
//     docker.go.
//
// Discovery is recursive from the scan root so nested modules and workspace
// sub-packages are found, while vendored / generated trees (node_modules,
// .git, …) are pruned for speed and signal.
package detect

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Kind classifies a build target. Its string value doubles as the user-facing
// group label and the value accepted by the `--type` filter.
type Kind string

const (
	KindGo     Kind = "go"
	KindPnpm   Kind = "pnpm"
	KindPodman Kind = "podman"
)

// Target is a single selectable, runnable build unit.
type Target struct {
	Kind Kind

	// Name is the unit's short identity within its directory, e.g. the module
	// name (go), the script name "build:rest" (pnpm), or "rest.Dockerfile"
	// (podman). It is NOT unique on its own — pair it with RelDir.
	Name string

	// RelDir is the target's directory relative to the scan root ("." for the
	// root itself). Used for display grouping and de-duplication.
	RelDir string

	// Dir is the absolute working directory the command executes in.
	Dir string

	// Detail is a one-line, human-readable summary shown under the target in
	// the menu and report (the resolved image tag, the script body, …).
	Detail string

	// Cmd and Args are the exact process to spawn, executed with Dir as CWD.
	Cmd  string
	Args []string
}

// ID is a stable, unique key for a target (used as a selection-map key and to
// keep the menu order deterministic across runs).
func (t Target) ID() string {
	return string(t.Kind) + "\x00" + t.RelDir + "\x00" + t.Name
}

// prunedDirs are directory names never descended into: dependency caches,
// VCS metadata, build output, and editor/secret scratch dirs. Pruning here is
// what keeps a full-stack scan fast.
var prunedDirs = map[string]struct{}{
	"node_modules": {},
	".git":         {},
	"vendor":       {},
	"dist":         {},
	".idea":        {},
	".secrets":     {},
	".svelte-kit":  {},
	".turbo":       {},
	".next":        {},
}

// Detect walks root and returns every build target found, sorted for stable
// presentation (by kind, then directory, then name).
func Detect(root string) ([]Target, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	var targets []Target

	walkErr := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// A single unreadable subtree shouldn't abort the whole scan —
			// skip it and keep discovering everything else.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !d.IsDir() {
			return nil
		}

		// Always descend into the root itself; prune known-noise dirs anywhere
		// below it (but never prune the root even if it is itself named e.g.
		// "dist").
		if path != absRoot {
			if _, pruned := prunedDirs[d.Name()]; pruned {
				return filepath.SkipDir
			}
		}

		rel, _ := filepath.Rel(absRoot, path)

		targets = append(targets, detectGo(path, rel)...)
		targets = append(targets, detectPnpm(path, rel)...)
		targets = append(targets, detectPodman(path, rel)...)

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

// kindOrder fixes the group order in the menu: Go first, then pnpm, then
// podman — cheapest/fastest builds first so feedback arrives sooner.
func kindOrder(k Kind) int {
	switch k {
	case KindGo:
		return 0
	case KindPnpm:
		return 1
	case KindPodman:
		return 2
	default:
		return 3
	}
}

// detectGo emits a single `go build ./...` target when dir holds a go.mod.
func detectGo(dir, rel string) []Target {
	modPath := filepath.Join(dir, "go.mod")
	if !fileExists(modPath) {
		return nil
	}

	name := goModuleName(modPath)
	if name == "" {
		name = filepath.Base(dir)
	}

	return []Target{{
		Kind:   KindGo,
		Name:   name,
		RelDir: rel,
		Dir:    dir,
		Detail: "go build ./...",
		Cmd:    "go",
		Args:   []string{"build", "./..."},
	}}
}

// packageJSON is the minimal shape we need out of a package.json.
type packageJSON struct {
	Name    string            `json:"name"`
	Scripts map[string]string `json:"scripts"`
}

// detectPnpm emits one target per "build"-prefixed script in dir's
// package.json. Each script is listed individually so the user can build, say,
// only `build:rest` without triggering the umbrella `build`.
func detectPnpm(dir, rel string) []Target {
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
		// "build", "build:rest", "build:types" … but not "prebuild" or
		// "rebuild" — the script must start the build.
		if name == "build" || strings.HasPrefix(name, "build:") {
			scripts = append(scripts, name)
		}
	}
	sort.Strings(scripts)

	targets := make([]Target, 0, len(scripts))
	for _, s := range scripts {
		targets = append(targets, Target{
			Kind:   KindPnpm,
			Name:   s,
			RelDir: rel,
			Dir:    dir,
			Detail: truncate(pkg.Scripts[s], 70),
			Cmd:    "pnpm",
			Args:   []string{"run", s},
		})
	}
	return targets
}

// fileExists reports whether path is an existing regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// goModuleName extracts the module short name from a go.mod's `module` line:
// "github.com/a-novel/service-json-keys/v2" → "service-json-keys". The major
// version suffix is dropped so v1 and v2 of the same module read identically.
func goModuleName(modPath string) string {
	full := goModulePath(modPath)
	if full == "" {
		return ""
	}
	full = stripMajorSuffix(full)
	parts := strings.Split(full, "/")
	return parts[len(parts)-1]
}

// goModulePath returns the raw module path from a go.mod's `module` directive,
// or "" if it cannot be read.
func goModulePath(modPath string) string {
	f, err := os.Open(modPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// stripMajorSuffix removes a trailing "/vN" semantic-import-version segment.
func stripMajorSuffix(modulePath string) string {
	i := strings.LastIndex(modulePath, "/v")
	if i < 0 {
		return modulePath
	}
	suffix := modulePath[i+2:]
	if suffix == "" {
		return modulePath
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return modulePath
		}
	}
	return modulePath[:i]
}

// truncate shortens s to max runes, appending an ellipsis when cut. Used to
// keep one-line details from wrapping the menu.
func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
