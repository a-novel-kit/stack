package repocfg

import (
	"fmt"
	"os"
	"path/filepath"
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
// CodeQL languages and Dependabot ecosystems to configure, and whether the
// repo has tests (which gates the codecov ruleset).
type Discovered struct {
	Checks         []CheckRef
	CodeQLLangs    []string
	DependabotEcos []string
	HasTests       bool
}

// Discover walks repoPath and applies the checks.yaml map to produce the
// required-check set + CodeQL/Dependabot identifiers. Detection is by file
// presence plus the detect package for docker build targets.
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
		if anyExists(repoPath, lr.Detect) {
			addDefs(lr.Checks)
			d.CodeQLLangs = appendUnique(d.CodeQLLangs, lr.CodeQL...)
			d.DependabotEcos = appendUnique(d.DependabotEcos, lr.Dependabot...)
		}
	}

	for _, name := range sortedFeatureKeys(cc.Features) {
		fr := cc.Features[name]
		if anyExists(repoPath, fr.Detect) {
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
// Dockerfile name (e.g. init -> job-init), which is why `update` reconciles
// against the live ruleset rather than trusting discovery alone.
func dockerTargetName(name string) string {
	name = strings.TrimSuffix(name, ".Dockerfile")
	return strings.ReplaceAll(name, ".", "-")
}

func anyExists(root string, names []string) bool {
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(n))); err == nil {
			return true
		}
	}
	return false
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
