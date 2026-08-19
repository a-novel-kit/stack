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
	// KindGo is a Go module, built and tested through the go toolchain.
	KindGo Kind = "go"
	// KindPnpm is a package.json script, run through pnpm.
	KindPnpm Kind = "pnpm"
	// KindPodman is a root Dockerfile or one under builds/, built into an image.
	KindPodman Kind = "podman"
	// KindContainer is a run-mode target: a compose service guarded by a
	// profile that the runner brings up with `podman compose --profile X
	// up <svc>`. Only DetectRun emits it, in container mode.
	KindContainer Kind = "container"
)

// buildArg is the "build" token shared by the go and podman subcommands and
// the canonical pnpm script name.
const buildArg = "build"

// pkgAll is the Go "all packages under here" selector, shared by the build and
// test target builders.
const pkgAll = "./..."

// testArg is the "test" token shared by `go test`, the canonical pnpm script
// name, and the env id.
const testArg = "test"

// runArg is the "run" token shared by the `pnpm run <script>` subcommand, the
// canonical "run"/"run:*" script name, and the run env id.
const runArg = "run"

// InitOrder lists the one-shot init Go entrypoints by Name, in the order they
// must run to completion before any long-lived service target: init seeds,
// migrations applies the schema, rotate-keys refreshes the JWKs. Detection,
// the runner and mode resolution all read it, so the policy lives in one place.
var InitOrder = []string{"init", "migrations", "rotate-keys"}

// IsInit reports whether t is one of the [InitOrder] entrypoints, a Go
// `cmd/<name>` main. The picker groups these targets with the container
// targets, and the runner barriers on them before launching any long-lived
// service.
func IsInit(t Target) bool {
	if t.Kind != KindGo {
		return false
	}
	for _, n := range InitOrder {
		if t.Name == n {
			return true
		}
	}
	return false
}

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

	// Service is the owning repo/module short name (e.g. "service-json-keys").
	// Set for `run` targets so the UI can disambiguate identically-named
	// entrypoints across services (every service has a "rest"). Empty for
	// build/test, where Name and RelDir already suffice.
	Service string

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

	// Env, when non-nil, is a podman-compose environment that must be up
	// before the command runs and torn down after. Only test targets set it.
	Env *ComposeEnv

	// ComposeService is the compose service name that runs this target
	// dockerized, set by `run` detection when the target's name matches a
	// profile in its env's compose file (e.g. "rest" →
	// "service-json-keys-rest"). Empty when dockerized mode cannot run the
	// target — one-shots such as migrations have no compose service — and
	// the runner falls back to local exec.
	ComposeService string
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
	// Profiles maps a compose `profiles: ["x"]` value to the service name
	// carrying it (e.g. "rest" → "service-json-keys-rest"). `run` reads it to
	// pick the compose service to bring up for a target requested in
	// dockerized mode (`podman compose --profile x up <svc>`).
	Profiles map[string]string
	// Services lists every compose service declared under `services:`, in
	// source order. The runner derives the set to bring up at env-up time
	// from it, skipping in global mode any sibling service already being run
	// from its own repo.
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

// Detect walks root and returns every build target found, sorted for stable
// presentation (by kind, then directory, then name).
func Detect(root string) ([]Target, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	ignored := gitIgnoredDirs(absRoot)
	var targets []Target

	walkErr := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Skip an unreadable subtree and keep discovering the rest.
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

func kindOrder(k Kind) int {
	switch k {
	case KindGo:
		return 0
	case KindPnpm:
		return 1
	case KindPodman:
		return 2
	case KindContainer:
		return 3
	default:
		return 4
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
		Detail: "go build " + pkgAll,
		Cmd:    "go",
		Args:   []string{buildArg, pkgAll},
	}}
}

// packageJSON is the minimal shape read out of a package.json.
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
		if pnpmScript(name, buildArg) {
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
			Cmd:    string(KindPnpm),
			Args:   []string{runArg, s},
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
	defer func() { _ = f.Close() }()

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
func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen-1]) + "…"
}
