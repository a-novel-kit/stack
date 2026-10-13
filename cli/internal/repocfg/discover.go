package repocfg

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// integrationActions is the checks.yaml key for the GitHub Actions app — the
// poster of every job-emitted check.
const integrationActions = "actions"

// CheckRef is a required status check resolved to its posting app.
type CheckRef struct {
	Context       string
	IntegrationID int64
}

// Discovered is what probing a repo tells us: the required checks for the
// master ruleset, and the secrets its workflows read.
type Discovered struct {
	Checks []CheckRef
	// Secrets are the secret names the repo's workflows reference, sorted.
	Secrets []string
}

// Discover computes a repo's required checks and the secrets its workflows read.
//
// Required checks are the `always` set plus every job declared in the repo's
// .github/workflows/main.yaml, minus cc.Exclude — the job id is the check
// context, so there is no class- or file-based derivation to drift. A missing
// main.yaml (e.g. a docs/meta repo) simply contributes no jobs.
func Discover(repoPath string, cc *ChecksConfig) (*Discovered, error) {
	d := &Discovered{}
	seen := map[string]bool{}
	add := func(ctx string, integ int64) {
		if ctx == "" || seen[ctx] {
			return
		}
		seen[ctx] = true
		d.Checks = append(d.Checks, CheckRef{Context: ctx, IntegrationID: integ})
	}

	for _, cd := range cc.Always {
		add(cd.Context, cc.Integrations[cd.Integration])
	}

	jobs, err := workflowJobs(filepath.Join(repoPath, ".github", "workflows", "main.yaml"))
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		if cc.Exclude.excludes(j) {
			continue
		}
		add(j.Context, cc.Integrations[integrationActions])
	}

	sort.Slice(d.Checks, func(i, j int) bool { return d.Checks[i].Context < d.Checks[j].Context })

	if d.Secrets, err = workflowSecrets(filepath.Join(repoPath, ".github", "workflows")); err != nil {
		return nil, err
	}
	return d, nil
}

// secretRef matches a secrets-context reference such as secrets.CODECOV_TOKEN.
var secretRef = regexp.MustCompile(`\bsecrets\.([A-Za-z_][A-Za-z0-9_]*)`)

// workflowSecrets lists the secret names that the workflow files in dir
// reference, sorted and unique. A repo without workflows references none.
func workflowSecrets(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if ext := filepath.Ext(entry.Name()); entry.IsDir() || ext != ".yaml" && ext != ".yml" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		for _, match := range secretRef.FindAllSubmatch(raw, -1) {
			names = append(names, string(match[1]))
		}
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

// workflowJob is one job parsed from a workflow file: its check context (the
// job's `name` if set, else its id) and its `if:` guard.
type workflowJob struct {
	ID      string
	Context string
	If      string
}

// workflowJobs parses the `jobs:` of a workflow file, returning each job's
// context and `if:`. A missing file yields no jobs (not an error) — not every
// repo has a main.yaml. Jobs are returned sorted by id for determinism.
func workflowJobs(path string) ([]workflowJob, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var wf struct {
		Jobs map[string]struct {
			Name string `yaml:"name"`
			If   string `yaml:"if"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return nil, err
	}
	out := make([]workflowJob, 0, len(wf.Jobs))
	for id, j := range wf.Jobs {
		ctx := id
		if j.Name != "" {
			ctx = j.Name
		}
		out = append(out, workflowJob{ID: id, Context: ctx, If: j.If})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// excludes reports whether a main.yaml job is held back from the required
// checks, because its id matches an excluded prefix or its `if:` contains an
// excluded string.
func (e ExcludeRules) excludes(j workflowJob) bool {
	for _, p := range e.Prefixes {
		if strings.HasPrefix(j.ID, p) {
			return true
		}
	}
	for _, s := range e.IfContains {
		if s != "" && strings.Contains(j.If, s) {
			return true
		}
	}
	return false
}
