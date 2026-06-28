package repocfg

import (
	"fmt"
	"sort"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/detect"
)

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

// Discover walks repoPath and applies the checks.yaml map to produce the
// required-check set + CodeQL languages. A language/feature signal is matched
// by detect.ExistsUnder — a bounded, gitignore-aware walk — so a module that
// lives in a sub-directory (e.g. stack's Go module under cli/) is detected, not
// just one at the repo root. Docker build targets come from the detect package.
func Discover(repoPath string, cc *ChecksConfig) (*Discovered, error) {
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

	sort.Slice(d.Checks, func(i, j int) bool { return d.Checks[i].Context < d.Checks[j].Context })
	return d, nil
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
