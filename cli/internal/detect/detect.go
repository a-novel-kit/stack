// Package detect discovers buildable targets under a directory tree.
//
// It recognizes three kinds of build, matching the conventions used across the
// a-novel / a-novel-kit repositories:
//
//   - [KindGo]     — any directory containing a go.mod (one target per module,
//     including nested modules).
//   - [KindPnpm]   — any package.json whose "scripts" map has one or more keys
//     starting with "build" (one target per matching script).
//   - [KindPodman] — a root Dockerfile or builds/*.Dockerfile file, with an
//     image tag derived from its repository and filename.
//
// Discovery recurses from the scan root so nested modules and workspace
// sub-packages are found; vendored and generated trees (node_modules, .git, …)
// are pruned.
package detect

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// Kind classifies a build target. Its string value doubles as the user-facing
// group label and the value accepted by the `--type` filter.
type Kind string

const (
	// KindGo is a Go module, built and tested through the go toolchain.
	KindGo Kind = "go"
	// KindPnpm is a package.json script, run through pnpm.
	KindPnpm Kind = "pnpm"
	// KindPodman is a root Dockerfile or one under builds/, built into an image.
	KindPodman Kind = "podman"
)

// Kinds lists every kind in presentation order.
var Kinds = []Kind{KindGo, KindPnpm, KindPodman}

// buildArg is the "build" token shared by the go and podman subcommands and
// the canonical pnpm script name.
const buildArg = "build"

// pkgAll is the Go "all packages under here" selector, shared by the build and
// test target builders.
const pkgAll = "./..."

// testArg is the "test" token shared by `go test`, the canonical pnpm script
// name, and the env id.
const testArg = "test"

// ciSuffix marks a pnpm script as CI-only ("test:ci", "build:ci"). Those are
// tailored to the GitHub pipeline, so discovery skips them.
const ciSuffix = ":ci"

// pnpmScript reports whether a package.json script name is a discoverable
// "<kind>" target: "<kind>" or "<kind>:<x>", excluding the CI-only
// "<kind>:ci".
func pnpmScript(name, kind string) bool {
	if strings.HasSuffix(name, ciSuffix) {
		return false
	}
	return name == kind || strings.HasPrefix(name, kind+":")
}

// Target is a single selectable, runnable build unit.
type Target struct {
	Kind Kind

	// Name is the unit's short identity within its directory, e.g. the module
	// name (go), the script name "build:rest" (pnpm), or "Dockerfile" /
	// "rest.Dockerfile" (podman). It is not unique on its own — pair it with
	// RelDir.
	Name string

	// RelDir is the target's directory relative to the scan root ("." for the
	// root itself). Used for display grouping and de-duplication.
	RelDir string

	// Dir is the absolute working directory the command executes in.
	Dir string

	// Detail is a one-line, human-readable summary shown under the target in
	// the picker (the resolved image tag, the script body, …).
	Detail string

	// Cmd and Args are the exact process to spawn, executed with Dir as CWD.
	Cmd  string
	Args []string

	// Env, when non-nil, is a podman-compose environment that must be up
	// before the command runs and torn down after. Only test targets set it.
	Env *ComposeEnv
}

// ComposeEnv is a podman-compose test environment discovered from a
// builds/podman-compose.<id>.test.yaml file.
type ComposeEnv struct {
	// File is the absolute path to the compose YAML.
	File string
	// Project is the `podman compose -p` project name, unique per env file so
	// parallel test targets never collide on container or network names.
	Project string
	// ID is the parsed identifier, e.g. "go.internal" or "pnpm".
	ID string
	// Ports are the env-var names the compose file binds on the host side of
	// a `ports:` mapping (POSTGRES_PORT, GRPC_PORT, …), the ports the host
	// test process talks to. The runner allocates a free TCP port for each so
	// parallel targets never collide.
	Ports []string
	// Refs is every ${VAR} the compose file interpolates, host-exposed or
	// not. The runner fills known test defaults (POSTGRES_USER, PASSWORD, DB,
	// HOST) for any it references, so an internal-only postgres with no host
	// port, and therefore no entry in Ports, still gets credentials.
	Refs []string
	// Services lists every compose service the file declares, sorted.
	Services []string
	// Dependents lists the services that declare a `depends_on:` block. The
	// test env-up path starts dependency-free services first and dependents
	// second, so ordering never relies on the compose provider's
	// `depends_on` wait, which is broken on podman-compose ≤1.5.x and on
	// setups without systemd. See build.composeUpPhased.
	Dependents []string
}

// ID is a stable, unique key for a target, used as a selection-map key and to
// keep the menu order deterministic across runs.
func (t Target) ID() string {
	return string(t.Kind) + "\x00" + t.RelDir + "\x00" + t.Name
}

// prunedDirs are non-hidden directory names never descended into: dependency
// caches and build output. Hidden dirs (anything starting with ".") are
// pruned unconditionally by skipDir, so .git/.idea/.cache/etc. need no entry.
var prunedDirs = map[string]struct{}{
	"node_modules": {},
	"vendor":       {},
	"dist":         {},
}

// maxScanDepth bounds recursion depth below the scan root. Real layouts nest
// targets a handful of levels (app/<repo>/pkg/js/test/rest), so the cap is
// generous for them while stopping an accidental run from $HOME or / from
// walking the whole filesystem and appearing to hang.
const maxScanDepth = 10

// skipDir reports whether the walk should stay out of path. It prunes
// git-ignored directories (so a scan from the stack root never recurses the
// gitignored app/ and kit/ checkouts), the known-noise names, every hidden
// directory, and anything past maxScanDepth. The root itself is always kept.
func skipDir(absRoot, path, name string, ignored map[string]struct{}) bool {
	if path == absRoot {
		return false
	}
	if _, isIgnored := ignored[path]; isIgnored {
		return true
	}
	if _, pruned := prunedDirs[name]; pruned {
		return true
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	if rel, err := filepath.Rel(absRoot, path); err == nil {
		if strings.Count(rel, string(filepath.Separator)) >= maxScanDepth {
			return true
		}
	}
	return false
}

// Detect walks root and returns every build target found, sorted by kind, then
// directory, then name.
func Detect(root string) ([]Target, error) {
	return walk(root, func(dir, rel string) []Target {
		return slices.Concat(detectGo(dir, rel), detectPnpm(dir, rel), detectPodman(dir, rel))
	})
}

// walk calls visit for every directory under root the scan keeps, and returns
// the targets it emits in presentation order.
func walk(root string, visit func(dir, rel string) []Target) ([]Target, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	ignored := gitIgnoredDirs(absRoot)
	var targets []Target
	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Skip an unreadable entry and keep discovering the rest.
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
		targets = append(targets, visit(path, rel)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(targets, func(a, b Target) int {
		return cmp.Or(
			cmp.Compare(slices.Index(Kinds, a.Kind), slices.Index(Kinds, b.Kind)),
			cmp.Compare(a.RelDir, b.RelDir),
			cmp.Compare(a.Name, b.Name),
		)
	})
	return targets, nil
}

// detectGo emits a single `go build ./...` target when dir holds a go.mod.
func detectGo(dir, rel string) []Target {
	if !IsFile(filepath.Join(dir, "go.mod")) {
		return nil
	}
	return []Target{{
		Kind:   KindGo,
		Name:   goModuleName(dir),
		RelDir: rel,
		Dir:    dir,
		Detail: "go build " + pkgAll,
		Cmd:    string(KindGo),
		Args:   []string{buildArg, pkgAll},
	}}
}

// detectPnpm emits one target per "build"-prefixed script in dir's
// package.json. Each script is listed individually so the user can build, say,
// only `build:rest` without triggering the umbrella `build`.
func detectPnpm(dir, rel string) []Target {
	scripts := pnpmScripts(dir, buildArg)
	targets := make([]Target, 0, len(scripts))
	for _, s := range scripts {
		targets = append(targets, pnpmTarget(dir, rel, s))
	}
	return targets
}

// pnpmScripts returns the "<kind>" and "<kind>:*" scripts of dir's
// package.json, sorted, with their bodies.
func pnpmScripts(dir, kind string) []pnpmScriptDef {
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(raw, &pkg) != nil {
		return nil
	}
	var out []pnpmScriptDef
	for name, body := range pkg.Scripts {
		if pnpmScript(name, kind) {
			out = append(out, pnpmScriptDef{name: name, body: body})
		}
	}
	slices.SortFunc(out, func(a, b pnpmScriptDef) int { return strings.Compare(a.name, b.name) })
	return out
}

// pnpmScriptDef is one package.json script.
type pnpmScriptDef struct{ name, body string }

// pnpmTarget runs one package.json script through pnpm.
func pnpmTarget(dir, rel string, s pnpmScriptDef) Target {
	return Target{
		Kind:   KindPnpm,
		Name:   s.name,
		RelDir: rel,
		Dir:    dir,
		Detail: strings.TrimSpace(s.body),
		Cmd:    string(KindPnpm),
		Args:   []string{"run", s.name},
	}
}

// IsFile reports whether path is an existing regular file.
func IsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// goModulePath returns the module path of dir/go.mod without its major-version
// suffix ("github.com/a-novel/service-json-keys/v2" →
// "github.com/a-novel/service-json-keys"), or "" when there is none.
func goModulePath(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	prefix, _, _ := module.SplitPathVersion(modfile.ModulePath(raw))
	return prefix
}

// goModuleName is the last segment of dir's module path, or the directory name
// when go.mod names no module.
func goModuleName(dir string) string {
	path := goModulePath(dir)
	if path == "" {
		return filepath.Base(dir)
	}
	return path[strings.LastIndexByte(path, '/')+1:]
}
