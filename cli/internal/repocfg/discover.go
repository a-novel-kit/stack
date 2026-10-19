package repocfg

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// integrationActions is the checks.yaml key for the GitHub Actions app, which
// posts every check a job emits.
const integrationActions = "actions"

// CheckRef is a required status check resolved to its posting app.
type CheckRef struct {
	Context       string
	IntegrationID int64
}

// Discovered is what a repo's working tree shows: the required checks for the
// master ruleset, and the secrets and environments its workflows use.
type Discovered struct {
	Checks []CheckRef
	// Secrets are the secret names the repo's workflows reference, sorted.
	Secrets []string
	// Environments are the deployment environments its jobs name, sorted.
	Environments []string
}

// Discover computes a repo's required checks and the secrets and environments
// its workflows use.
//
// Required checks are the `always` set plus every job declared in the repo's
// .github/workflows/main.yaml, minus cc.Exclude. The job's name, or its id, is
// the check context, so no class-based list can drift from the workflow. A
// missing main.yaml contributes no jobs.
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

	if d.Secrets, d.Environments, err = workflowUses(filepath.Join(repoPath, ".github", "workflows")); err != nil {
		return nil, err
	}
	return d, nil
}

// secretRef matches a secrets-context reference such as secrets.CODECOV_TOKEN.
var secretRef = regexp.MustCompile(`\bsecrets\.([A-Za-z_][A-Za-z0-9_]*)`)

// workflowUses lists the secrets that the workflow files in dir reference and
// the environments their jobs name, each sorted and unique. A job names its
// environment either directly or as the name of an environment map.
func workflowUses(dir string) ([]string, []string, error) {
	var secrets, environments []string
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		if ext := filepath.Ext(entry.Name()); entry.IsDir() || ext != ".yaml" && ext != ".yml" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, nil, err
		}
		for _, match := range secretRef.FindAllSubmatch(raw, -1) {
			secrets = append(secrets, string(match[1]))
		}
		named, err := jobEnvironments(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		environments = append(environments, named...)
	}
	slices.Sort(secrets)
	slices.Sort(environments)
	return slices.Compact(secrets), slices.Compact(environments), nil
}

// jobEnvironments lists the environments the jobs of a workflow file name,
// directly or as the name of an environment map.
func jobEnvironments(raw []byte) ([]string, error) {
	var wf struct {
		Jobs map[string]struct {
			Environment any `yaml:"environment"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return nil, err
	}
	var environments []string
	for _, job := range wf.Jobs {
		switch environment := job.Environment.(type) {
		case string:
			environments = append(environments, environment)
		case map[string]any:
			if name, ok := environment["name"].(string); ok {
				environments = append(environments, name)
			}
		}
	}
	return environments, nil
}

// workflowJob is one job parsed from a workflow file: its check context (the
// job's `name` if set, else its id) and its `if:` guard.
type workflowJob struct {
	ID      string
	Context string
	If      string
}

// workflowJobs parses the `jobs:` of a workflow file, returning each job's
// context and `if:`, sorted by id. A missing file yields no jobs, since not
// every repo has a main.yaml.
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
		out = append(out, workflowJob{ID: id, Context: cmp.Or(j.Name, id), If: j.If})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// excludes reports whether a main.yaml job is held back from the required
// checks, because its id matches an excluded prefix or its `if:` contains an
// excluded string.
func (e ExcludeRules) excludes(j workflowJob) bool {
	return slices.ContainsFunc(e.Prefixes, func(prefix string) bool { return strings.HasPrefix(j.ID, prefix) }) ||
		slices.ContainsFunc(e.IfContains, func(s string) bool { return s != "" && strings.Contains(j.If, s) })
}
