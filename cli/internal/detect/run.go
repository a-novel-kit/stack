package detect

import (
	"encoding/json"
	"fmt"
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

	// Global mode: when run from a stack root that contains `app/service-*`
	// checkouts, fan out across every service repo instead of scanning the
	// stack itself (which has no service entrypoints; the CLI is the
	// orchestrator). Single-repo behaviour is unchanged.
	walkRoots := []string{absRoot}
	if rs := globalServiceRoots(absRoot); len(rs) > 0 {
		walkRoots = rs
	}

	var targets []Target
	for _, walkRoot := range walkRoots {
		// Per-walk-root .gitignore (each service repo is independent).
		ignored := gitIgnoredDirs(walkRoot)
		walkErr := filepath.WalkDir(walkRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			// skipDir uses walkRoot as its "root" so the walk depth/ignore
			// semantics are per-repo, but filepath.Rel for `pnpmRun` and
			// `goRun`'s moduleRootOf bound use the OUTER absRoot — that is
			// how each service's modRel comes out as "app/service-X" instead
			// of "." (which would collide across services in the compose
			// project name).
			if skipDir(walkRoot, path, d.Name(), ignored) {
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
		// One container target per profile-guarded compose service in this
		// walk root's run env. Container targets are emitted alongside
		// live targets; main.go filters by run mode before the picker so
		// each mode sees only its own list.
		targets = append(targets, containerTargets(absRoot, walkRoot)...)
	}

	sort.SliceStable(targets, func(i, j int) bool {
		a, b := targets[i], targets[j]
		if a.Service != b.Service {
			return a.Service < b.Service
		}
		if a.Kind != b.Kind {
			return kindOrder(a.Kind) < kindOrder(b.Kind)
		}
		return a.Name < b.Name
	})
	return targets, nil
}

// globalServiceRoots auto-detects a stack-root scan: the directory contains
// `app/service-*` subdirectories (each its own git repo). Returns those
// service-repo paths, or nil if the root is not a stack. Platforms
// (`platform-*`) and non-service dirs under app/ are ignored.
func globalServiceRoots(absRoot string) []string {
	appDir := filepath.Join(absRoot, "app")
	info, err := os.Stat(appDir)
	if err != nil || !info.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return nil
	}
	var roots []string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "service-") {
			continue
		}
		svc := filepath.Join(appDir, e.Name())
		// Must be its own git repo to count — guards against an unrelated
		// `app/service-something/` directory that happens to share the name.
		if _, err := os.Stat(filepath.Join(svc, ".git")); err != nil {
			continue
		}
		roots = append(roots, svc)
	}
	sort.Strings(roots)
	return roots
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
	env := runEnvFor(modRoot, modRel)
	name := filepath.Base(dir)
	return []Target{{
		Kind:           KindGo,
		Name:           name,
		Service:        service,
		RelDir:         modRel,
		Dir:            modRoot,
		Detail:         "go run " + pkg,
		Cmd:            "go",
		Args:           []string{runArg, pkg},
		Env:            env,
		ComposeService: composeServiceFor(env, name),
	}}
}

// composeServiceFor looks up the compose service that would run a target's
// profile, if any. Returns "" when the env has no compose file or no service
// carries this profile (one-shots like migrations / rotate-keys / init).
func composeServiceFor(env *ComposeEnv, profile string) string {
	if env == nil {
		return ""
	}
	return env.Profiles[profile]
}

// containerTargets emits one KindContainer target per profile-guarded compose
// service in walkRoot's run env. The Cmd/Args are pre-baked so the runner can
// launch each target with the SAME exec path the live-mode targets use —
// dispatch is by Target.Kind, not by a runner-side mode flag.
func containerTargets(absRoot, walkRoot string) []Target {
	rel, _ := filepath.Rel(absRoot, walkRoot)
	env := runEnvFor(walkRoot, rel)
	if env == nil || len(env.Profiles) == 0 {
		return nil
	}
	service := goModuleName(filepath.Join(walkRoot, "go.mod"))
	if service == "" {
		service = filepath.Base(walkRoot)
	}
	profiles := make([]string, 0, len(env.Profiles))
	for p := range env.Profiles {
		profiles = append(profiles, p)
	}
	sort.Strings(profiles)
	out := make([]Target, 0, len(profiles))
	for _, p := range profiles {
		svc := env.Profiles[p]
		// Three-step launch in one shell:
		//   1. `compose --profile X up -d --build` — bring up everything
		//      the profile activates (the target + still-needed deps).
		//      No positional service name (some podman-compose versions
		//      reject it: `unrecognized arguments: <svc>`). No
		//      --force-recreate / --no-deps either — both were
		//      version-fragile; --profile alone is the universal selector.
		//   2. `podman ps -q --filter label=…` — resolve the freshly-
		//      started container's ID via the compose-spec labels every
		//      version sets. Goes through podman (not compose) so it is
		//      not subject to compose-side CLI variance.
		//   3. `exec podman logs -f <id>` — attach to the container's
		//      actual stdout/stderr (compose-level streaming was the
		//      source of the "build logs but no live logs" symptom).
		//
		// `exec` replaces bash so the runner's ctx-cancel terminates one
		// process. Single-quoted template values keep the shell literal —
		// none of project / path / profile / service contain quotes.
		// Four-step shell:
		//   1. `compose --profile X up -d --build > /dev/null` — bring up
		//      the profiled service (build / create / start). stdout is
		//      discarded because compose's `up` chatters about EVERY
		//      profile-less service it touches (postgres rebuild check,
		//      etc.); we only want the TARGET's logs in this tab. Errors
		//      still flow on stderr.
		//   2. Resolve the container ID — compose ps for versions that
		//      support a positional `ps`, falling back to `podman ps`
		//      grep-by-name (universal, substring-exclusive across
		//      sibling services).
		//   3. `podman start "$id"` — workaround for podman-compose
		//      versions where `--profile X up -d` creates the container
		//      but doesn't start it. No-op when already running.
		//   4. `exec podman logs -f` — direct streaming of the target's
		//      stdout/stderr.
		// Brief status lines on stderr keep the user oriented while the
		// (suppressed) build is happening.
		shell := fmt.Sprintf(
			"echo '── %s · bringing up container…' >&2 && "+
				"podman compose -p '%s' -f '%s' --profile '%s' up -d --build > /dev/null || exit; "+
				"id=$(podman compose -p '%s' -f '%s' ps -q '%s' 2>/dev/null | head -1); "+
				"[ -z \"$id\" ] && id=$(podman ps -a --format '{{.ID}} {{.Names}}' | grep -F '%s' | head -1 | cut -d' ' -f1); "+
				"[ -z \"$id\" ] && { echo \"Error: container for service '%s' not found\" >&2; exit 1; }; "+
				"podman start \"$id\" > /dev/null 2>&1; "+
				"echo '── %s · streaming logs' >&2; "+
				"exec podman logs -f \"$id\"",
			svc,
			env.Project, env.File, p,
			env.Project, env.File, svc,
			svc,
			svc,
			svc,
		)
		out = append(out, Target{
			Kind:           KindContainer,
			Name:           p,
			Service:        service,
			RelDir:         rel,
			Dir:            walkRoot,
			Detail:         "podman compose --profile " + p + " up " + svc + " · podman logs -f",
			Cmd:            "bash",
			Args:           []string{"-c", shell},
			Env:            env,
			ComposeService: svc,
		})
	}
	return out
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

	env := runEnvFor(dir, rel)
	targets := make([]Target, 0, len(names))
	for _, s := range names {
		targets = append(targets, Target{
			Kind:           KindPnpm,
			Name:           s,
			Service:        service,
			RelDir:         rel,
			Dir:            dir,
			Detail:         truncate(pkg.Scripts[s], 60),
			Cmd:            string(KindPnpm),
			Args:           []string{runArg, s},
			Env:            env,
			ComposeService: composeServiceFor(env, s),
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
				File:     file,
				Project:  composeProjectP("anovel-run-", rel, m[1]),
				ID:       m[1],
				Ports:    ports,
				Refs:     refs,
				Profiles: composeProfiles(file),
				Services: composeServices(file),
			}
		}
	}
	if fallback == "" {
		return nil
	}
	ports, refs := composeParse(fallback)
	return &ComposeEnv{
		File:     fallback,
		Project:  composeProjectP("anovel-run-", rel, runArg),
		ID:       runArg,
		Ports:    ports,
		Refs:     refs,
		Profiles: composeProfiles(fallback),
		Services: composeServices(fallback),
	}
}
