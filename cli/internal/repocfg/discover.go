package repocfg

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/detect"
)

// integrationActions is the checks.yaml key for the GitHub Actions app — the
// poster of every CI-emitted check (lint-go, test-js, …).
const integrationActions = "actions"

// CheckRef is a required status check resolved to its posting app.
type CheckRef struct {
	Context       string
	IntegrationID int64
}

// Discovered is what probing a repo tells us: the required checks, the
// CodeQL languages, and whether the repo has tests (informational; the
// codecov ruleset is gated on actual Codecov reporting, not this).
type Discovered struct {
	Checks      []CheckRef
	CodeQLLangs []string
	HasTests    bool
}

// Discover produces a repo's required checks + CodeQL languages, dispatching on
// class: the freeform `library` class derives them generically from files and
// pnpm scripts (see discoverLibrary); the strong-semantic classes use the fixed
// checks.yaml rules (see discoverStrong).
func Discover(repoPath string, class Class, cc *ChecksConfig) (*Discovered, error) {
	if class == ClassLibrary {
		return discoverLibrary(repoPath, cc)
	}
	return discoverStrong(repoPath, cc)
}

// discoverStrong applies the checks.yaml map (languages + features + docker) for
// the strong-semantic classes (service / workflows / meta), whose structure is
// known and whose check names are fixed. A signal is matched by
// detect.ExistsUnder — a bounded, gitignore-aware walk — and detection adapts to
// the repo's signals, so a docs/meta repo with only a package.json yields just
// lint-node.
func discoverStrong(repoPath string, cc *ChecksConfig) (*Discovered, error) {
	d := &Discovered{}
	seen := map[string]bool{}
	addCheck := func(ctx string, integ int64) {
		if ctx == "" || seen[ctx] {
			return
		}
		seen[ctx] = true
		d.Checks = append(d.Checks, CheckRef{Context: ctx, IntegrationID: integ})
	}
	addDefs := func(defs []CheckDef) {
		for _, cd := range defs {
			addCheck(cd.Context, cc.Integrations[cd.Integration])
		}
	}

	addDefs(cc.Always)

	for _, name := range sortedKeys(cc.Languages) {
		lr := cc.Languages[name]
		if detect.ExistsUnder(repoPath, lr.Detect) {
			addDefs(lr.Checks)
			d.CodeQLLangs = appendUnique(d.CodeQLLangs, lr.CodeQL...)
		}
	}

	for _, name := range sortedFeatureKeys(cc.Features) {
		fr := cc.Features[name]
		if detect.ExistsUnder(repoPath, fr.Detect) {
			addDefs(fr.Checks)
		}
	}

	if targets, err := detect.Detect(repoPath); err == nil {
		for _, t := range targets {
			if t.Kind == detect.KindPodman {
				addCheck(fmt.Sprintf(cc.Docker.ContextFormat, dockerTargetName(t.Name)), cc.Integrations[cc.Docker.Integration])
			}
		}
	}

	if tests, err := detect.DetectTests(repoPath); err == nil {
		d.HasTests = len(tests) > 0
	}

	d.CodeQLLangs = appendUnique(d.CodeQLLangs, cc.CodeQLAlways...)

	sort.Slice(d.Checks, func(i, j int) bool { return d.Checks[i].Context < d.Checks[j].Context })
	return d, nil
}

// discoverLibrary derives a freeform library's checks generically from files and
// pnpm scripts — no folder heuristics, no assumed structure. Every check is
// lane-suffixed (-go / -js / -proto) and path-encoded by its module's location
// (a go.mod in cli/ → test-go-cli; a module at the root → test-go). Go modules
// additionally get generated-go when they carry a //go:generate directive; node
// packages derive checks from their workspace-root pnpm scripts (lint / test /
// build / generate → lint-js / test-js / build-js / generated-js).
func discoverLibrary(repoPath string, cc *ChecksConfig) (*Discovered, error) {
	d := &Discovered{}
	seen := map[string]bool{}
	actions := cc.Integrations[integrationActions]
	add := func(ctx string) {
		if ctx == "" || seen[ctx] {
			return
		}
		seen[ctx] = true
		d.Checks = append(d.Checks, CheckRef{Context: ctx, IntegrationID: actions})
	}

	// Always-required checks (e.g. GitGuardian) keep their own integrations.
	for _, cd := range cc.Always {
		if cd.Context == "" || seen[cd.Context] {
			continue
		}
		seen[cd.Context] = true
		d.Checks = append(d.Checks, CheckRef{Context: cd.Context, IntegrationID: cc.Integrations[cd.Integration]})
	}

	for _, dir := range detect.GoModuleDirs(repoPath) {
		suffix := pathSuffix(dir)
		add("lint-go" + suffix)
		add("test-go" + suffix)
		if detect.HasGoGenerate(filepath.Join(repoPath, filepath.FromSlash(dir))) {
			add("generated-go" + suffix)
		}
		d.CodeQLLangs = appendUnique(d.CodeQLLangs, "go")
	}

	nodeFound := false
	for _, nr := range detect.NodeScriptRoots(repoPath) {
		nodeFound = true
		suffix := pathSuffix(nr.RelDir)
		if nr.Kinds["lint"] {
			add("lint-js" + suffix)
		}
		if nr.Kinds["test"] {
			add("test-js" + suffix)
		}
		if nr.Kinds["build"] {
			add("build-js" + suffix)
		}
		if nr.Kinds["generate"] {
			add("generated-js" + suffix)
		}
	}
	if nodeFound {
		d.CodeQLLangs = appendUnique(d.CodeQLLangs, "javascript-typescript")
	}

	for _, dir := range detect.ProtoDirs(repoPath) {
		add("lint-proto" + pathSuffix(dir))
	}

	d.CodeQLLangs = appendUnique(d.CodeQLLangs, cc.CodeQLAlways...)

	if tests, err := detect.DetectTests(repoPath); err == nil {
		d.HasTests = len(tests) > 0
	}

	sort.Slice(d.Checks, func(i, j int) bool { return d.Checks[i].Context < d.Checks[j].Context })
	return d, nil
}

// pathSuffix turns a module's repo-relative dir into a check-name segment: "" for
// the root, else "-" + the dir with slashes replaced by dashes (pkg/js/rest →
// "-pkg-js-rest").
func pathSuffix(relDir string) string {
	if relDir == "" || relDir == "." {
		return ""
	}
	return "-" + strings.ReplaceAll(relDir, "/", "-")
}

// dockerTargetName turns a detect podman target ("rest.Dockerfile",
// "standalone.rest.Dockerfile") into the CI job suffix ("rest",
// "standalone-rest"). Note: a few CI jobs are not pure derivations of the
// Dockerfile name (e.g. init -> job-init), so discovery cannot reproduce them.
// Such a context is "unmanaged" — present live but outside the map's namespace —
// and `update` preserves it rather than reconciling it away (see
// reconcileMasterChecks); a `--full` reset, by contrast, drops it.
func dockerTargetName(name string) string {
	name = strings.TrimSuffix(name, ".Dockerfile")
	return strings.ReplaceAll(name, ".", "-")
}

func appendUnique(dst []string, vs ...string) []string {
	for _, v := range vs {
		found := false
		for _, e := range dst {
			if e == v {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, v)
		}
	}
	return dst
}

func sortedKeys(m map[string]LangRule) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func sortedFeatureKeys(m map[string]FeatureRule) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
