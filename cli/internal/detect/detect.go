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
	"bytes"
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
	// KindContainer is a run-mode target: a compose service guarded by a
	// profile that the runner brings up via `podman compose --profile X
	// up <svc>`. Distinct from KindPodman (build's Dockerfile targets) so
	// the two never collide in pickers / dispatch — KindContainer only
	// appears via DetectRun in container mode.
	KindContainer Kind = "container"
)

// buildArg is the "build" token shared by the go/podman subcommands and the
// canonical pnpm script name — one constant so it reads identically everywhere.
const buildArg = "build"

// pkgAll is the Go "all packages under here" selector, shared by the build and
// test target builders.
const pkgAll = "./..."

// testArg is the "test" token shared by `go test`, the canonical pnpm script
// name, and the env-id environment — one constant, like buildArg.
const testArg = "test"

// generateArg is the "generate" token: the canonical pnpm codegen script name
// and the node-generation check kind.
const generateArg = "generate"

// runArg is the "run" token: the pnpm `pnpm run <script>` subcommand, the
// canonical "run"/"run:*" script name, and the run env id — one constant.
const runArg = "run"

// InitOrder is the priority list of one-shot init Go entrypoints by Name.
// Anything in this list, when selected, must run to completion BEFORE the
// long-lived service targets — and IN this order (init seeds, migrations
// applies schema, rotate-keys refreshes JWKs). Shared between detect (mode
// filter), runner (launch barrier) and main (mode resolution) so the policy
// lives in one place.
var InitOrder = []string{"init", "migrations", "rotate-keys"}

// IsInit reports whether t is one of the InitOrder entrypoints (a Go
// `cmd/<name>` main). It is the same predicate used everywhere — the
// container-mode picker keeps these alongside KindContainer targets, the
// runner barriers on them, the picker auto-pull (migrations only) pulls
// them.
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

// ciSuffix marks a pnpm script as CI-only (e.g. "test:ci", "build:ci"): it is
// tailored for the GitHub pipeline (pnpm i, doc/build prep, …), not for a
// local developer run, so discovery skips it.
const ciSuffix = ":ci"

// pnpmScript reports whether a package.json script name is a discoverable
// "<kind>" target: it is "<kind>" or "<kind>:<x>", but never the CI-only
// "<kind>:ci" variant.
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
	// name (go), the script name "build:rest" (pnpm), or "rest.Dockerfile"
	// (podman). It is NOT unique on its own — pair it with RelDir.
	Name string

	// Service is the owning repo/module short name (e.g. "service-json-keys").
	// Set for `run` targets so the UI can disambiguate identically-named
	// entrypoints across services (a "rest" exists in every service). Empty
	// for build/test where Name+RelDir already suffice.
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

	// ComposeService is the compose service name that would run this target
	// dockerised — populated by `run` detection when the target's name
	// matches a profile in its env's compose file (e.g. "rest" →
	// "service-json-keys-rest"). Empty means dockerised mode cannot run this
	// target (one-shots like migrations / rotate-keys / init have no compose
	// service); the runner falls back to local exec for those.
	ComposeService string
}

// ComposeEnv is a podman-compose test environment discovered from a
// builds/podman-compose.<id>.test.yaml file.
type ComposeEnv struct {
	// File is the absolute path to the compose YAML.
	File string
	// Project is the `podman compose -p` project name — unique per env file
	// so parallel test targets never collide on container/network names.
	Project string
	// ID is the parsed identifier, e.g. "go.internal" or "pnpm".
	ID string
	// Ports are the env-var names the compose file binds on the HOST side of
	// a `ports:` mapping (e.g. POSTGRES_PORT, GRPC_PORT) — exactly the ports
	// the host test process talks to. The runner allocates a free TCP port
	// for each so parallel targets never collide, replacing setup-env.sh's
	// node/get-port-please randomisation.
	Ports []string
	// Refs is every ${VAR} the compose file interpolates (host-exposed or
	// not). The runner fills known test defaults (POSTGRES_USER/PASSWORD/DB/
	// HOST) for any it references, so an internal-only postgres — which has
	// no host port and thus no entry in Ports — still gets credentials.
	Refs []string
	// Profiles maps a compose `profiles: ["x"]` value to the service name
	// that carries it (e.g. "rest" → "service-json-keys-rest"). `run` uses
	// this to know which compose service to bring up when a target is
	// requested in dockerised mode (`podman compose --profile x up <svc>`).
	Profiles map[string]string
	// Services lists every compose service declared under `services:`, in
	// source order. The runner uses it to compute the set of services to
	// bring up at env-up time — in global mode it skips any sibling service
	// that is also being run from its own repo (avoiding duplicates).
	Services []string
}

// ID is a stable, unique key for a target (used as a selection-map key and to
// keep the menu order deterministic across runs).
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
// targets a handful of levels (e.g. app/<repo>/pkg/js/test/rest); this cap is
// generous for that yet stops an accidental run from $HOME or / from walking
// the whole filesystem and appearing to hang.
const maxScanDepth = 10

// skipDir reports whether the walk should not descend into path. It prunes
// git-ignored directories (so a scan from the stack root never recurses the
// gitignored app/ and kit/ checkouts), the known-noise names, every hidden
// directory, and anything past maxScanDepth — the root itself is never
// skipped. This is what keeps a scan from an invalid or workspace directory
// fast and bounded instead of frozen.
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

// ExistsUnder reports whether any of relPaths (slash-separated, relative to a
// directory) exists at root or in any directory below it, using the same
// bounded, pruned, gitignore-aware walk as Detect. It lets a caller detect a
// signal file that lives in a sub-module directory — e.g. a go.mod or buf.yaml
// under cli/ rather than at the repo root — without descending the gitignored
// sibling checkouts (app/, kit/), node_modules, or hidden trees, and without
// runaway depth. The walk short-circuits on the first match.
func ExistsUnder(root string, relPaths []string) bool {
	if len(relPaths) == 0 {
		return false
	}
	found := false
	walkRepoDirs(root, func(absDir, _ string) bool {
		for _, rel := range relPaths {
			if _, statErr := os.Stat(filepath.Join(absDir, filepath.FromSlash(rel))); statErr == nil {
				found = true
				return true
			}
		}
		return false
	})
	return found
}

// walkRepoDirs walks root with the bounded, pruned, gitignore-aware policy
// shared by the detection probes — never descending the gitignored sibling
// checkouts (app/, kit/), node_modules, hidden trees, or past the depth cap. It
// invokes visit(absDir, relDir) for each surviving directory ("." for the root);
// visit returns true to stop the walk early.
func walkRepoDirs(root string, visit func(absDir, relDir string) bool) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return
	}
	ignored := gitIgnoredDirs(absRoot)
	_ = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// An unreadable subtree shouldn't abort the probe — skip it.
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
		// path is always absRoot or a descendant, so the relative dir is a plain
		// prefix trim ("." for the root) — no filepath.Rel error to handle.
		rel := "."
		if path != absRoot {
			rel = filepath.ToSlash(strings.TrimPrefix(path, absRoot+string(filepath.Separator)))
		}
		if visit(path, rel) {
			return filepath.SkipAll
		}
		return nil
	})
}

// GoModuleDirs returns the repo-relative directories (slash-separated, "." for
// the root) that contain a go.mod, found by the shared recursive walk. Used by
// freeform-library discovery to name a module's checks by its location
// (cli/go.mod → the "-cli" path segment).
func GoModuleDirs(root string) []string {
	var out []string
	walkRepoDirs(root, func(absDir, relDir string) bool {
		if fileExists(filepath.Join(absDir, "go.mod")) {
			out = append(out, relDir)
		}
		return false
	})
	return out
}

// HasGoGenerate reports whether any .go file in the module rooted at moduleDir
// carries a `//go:generate` directive — the signal that the module produces
// generated code and so warrants a generated-go check. It does not descend into
// nested modules, vendor, node_modules, or hidden dirs.
func HasGoGenerate(moduleDir string) bool {
	found := false
	_ = filepath.WalkDir(moduleDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != moduleDir {
				if name == "node_modules" || name == "vendor" || strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				// A nested module is its own unit — don't attribute its
				// directives to the parent.
				if fileExists(filepath.Join(path, "go.mod")) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if b, readErr := os.ReadFile(path); readErr == nil && bytes.Contains(b, []byte("//go:generate")) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// ProtoDirs returns the repo-relative directories containing a buf.yaml or
// buf.gen.yaml (proto modules), found by the shared recursive walk.
func ProtoDirs(root string) []string {
	var out []string
	walkRepoDirs(root, func(absDir, relDir string) bool {
		if fileExists(filepath.Join(absDir, "buf.yaml")) || fileExists(filepath.Join(absDir, "buf.gen.yaml")) {
			out = append(out, relDir)
		}
		return false
	})
	return out
}

// NodeScriptRoot is a pnpm package whose scripts define checks: its repo-relative
// directory and which canonical script kinds it declares.
type NodeScriptRoot struct {
	RelDir string
	Kinds  map[string]bool
}

// nodeScriptKinds are the canonical pnpm script names that map 1:1 to a node
// check kind: lint→lint-js, test→test-js, build→build-js. `generate` is handled
// separately (see pnpmScriptKinds) — it is lane-sensitive, since generate:go is
// Go codegen gated by generated-go, not a node-side generation.
var nodeScriptKinds = []string{"lint", "test", "build"}

// NodeScriptRoots returns the pnpm "script roots" under root: package.json files
// that own runnable scripts. In a pnpm workspace only the workspace root counts —
// its scripts bundle the members' (a root "test" runs every sub-package test) —
// so package.json files inside a workspace are skipped. A package.json with none
// of the canonical script kinds is omitted. Used by freeform-library discovery
// to derive js-lane checks (lint-js, test-js, build-js, generated-js).
func NodeScriptRoots(root string) []NodeScriptRoot {
	var workspaces []string
	type pkgDir struct{ abs, rel string }
	var pkgs []pkgDir
	walkRepoDirs(root, func(absDir, relDir string) bool {
		if fileExists(filepath.Join(absDir, "pnpm-workspace.yaml")) {
			workspaces = append(workspaces, relDir)
		}
		if fileExists(filepath.Join(absDir, "package.json")) {
			pkgs = append(pkgs, pkgDir{absDir, relDir})
		}
		return false
	})

	var out []NodeScriptRoot
	for _, p := range pkgs {
		if isWorkspaceMember(p.rel, workspaces) {
			continue
		}
		kinds := pnpmScriptKinds(filepath.Join(p.abs, "package.json"))
		if len(kinds) == 0 {
			continue
		}
		out = append(out, NodeScriptRoot{RelDir: p.rel, Kinds: kinds})
	}
	return out
}

// isWorkspaceMember reports whether relDir sits inside a pnpm workspace whose
// root is a different directory — i.e. its scripts belong to that root, not here.
func isWorkspaceMember(relDir string, workspaces []string) bool {
	for _, wr := range workspaces {
		if wr == relDir {
			continue
		}
		if wr == "." || strings.HasPrefix(relDir, wr+"/") {
			return true
		}
	}
	return false
}

// pnpmScriptKinds reads a package.json and returns the set of canonical script
// kinds it declares (ignoring the CI-only ":ci" variants).
func pnpmScriptKinds(pkgPath string) map[string]bool {
	raw, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil
	}
	var pkg packageJSON
	if json.Unmarshal(raw, &pkg) != nil {
		return nil
	}
	kinds := map[string]bool{}
	for name := range pkg.Scripts {
		for _, k := range nodeScriptKinds {
			if pnpmScript(name, k) {
				kinds[k] = true
			}
		}
		// A "generate" script counts as a node-side generation ONLY for a
		// non-go lane (generate:mjml, …). generate:go is Go codegen, already
		// gated by generated-go (//go:generate); the bare umbrella "generate"
		// just chains the per-lane scripts, so those are the real signal.
		if strings.HasPrefix(name, generateArg+":") && name != generateArg+":go" && !strings.HasSuffix(name, ciSuffix) {
			kinds[generateArg] = true
		}
	}
	return kinds
}

// HasNodeGenerate reports whether any pnpm script root under root declares a
// node-side generation — a non-go `generate:<lane>` script (e.g. generate:mjml).
// It tells a repo that generates JS/assets (warranting generated-js /
// generated-pnpm) apart from one whose only `generate` is Go codegen.
func HasNodeGenerate(root string) bool {
	for _, nr := range NodeScriptRoots(root) {
		if nr.Kinds[generateArg] {
			return true
		}
	}
	return false
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
		// "build", "build:rest", … but not "prebuild"/"rebuild" (must start
		// the build) and not "build:ci" (CI-only).
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
