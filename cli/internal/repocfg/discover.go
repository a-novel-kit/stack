package repocfg

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
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
// master ruleset, the secrets and environments its workflows use, and the
// major version it releases.
type Discovered struct {
	Checks []CheckRef
	// Secrets are the secret names the repo's workflows reference, sorted.
	Secrets []string
	// Environments are the deployment environments its jobs name, sorted.
	Environments []string
	// Major is the major of the version in the root package.json, which every
	// release bumps. A repo without a version reads as major 0.
	Major int
}

// Discover computes a repo's required checks, the secrets and environments
// its workflows use, and its major version.
//
// Required checks are the `always` set plus every job declared in the repo's
// .github/workflows/main.yaml, minus cc.Exclude. The job's name, or its id, is
// the check context, so no class-based list can drift from the workflow. A
// missing main.yaml contributes no jobs.
func Discover(repoPath string, cc *ChecksConfig) (*Discovered, error) {
	jobs, err := workflowJobs(filepath.Join(repoPath, ".github", "workflows", "main.yaml"))
	if err != nil {
		return nil, err
	}

	var checks []CheckRef
	for _, cd := range cc.Always {
		checks = append(checks, CheckRef{Context: cd.Context, IntegrationID: cc.Integrations[cd.Integration]})
	}
	for _, j := range jobs {
		if !cc.Exclude.excludes(j) {
			checks = append(checks, CheckRef{Context: j.Context, IntegrationID: cc.Integrations[integrationActions]})
		}
	}

	d := &Discovered{Checks: uniqueChecks(checks)}
	if d.Secrets, d.Environments, err = workflowUses(filepath.Join(repoPath, ".github", "workflows")); err != nil {
		return nil, err
	}
	if d.Major, err = packageMajor(filepath.Join(repoPath, "package.json")); err != nil {
		return nil, err
	}
	return d, nil
}

// packageMajor reads the major of the version in the package.json at path. A
// missing file or version reads as 0, and a version that is not semver is an
// error.
func packageMajor(path string) (int, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}

	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(content, &pkg); err != nil {
		return 0, fmt.Errorf("parse %s: %w", path, err)
	}
	if pkg.Version == "" {
		return 0, nil
	}

	major := semver.Major("v" + pkg.Version)
	if major == "" {
		return 0, fmt.Errorf("%s: version %q is not semver", path, pkg.Version)
	}
	return strconv.Atoi(strings.TrimPrefix(major, "v"))
}

// uniqueChecks sorts checks by context and keeps the first check of each
// context, so an always check wins over a main.yaml job of the same name. A
// check without a context is dropped.
func uniqueChecks(checks []CheckRef) []CheckRef {
	checks = slices.DeleteFunc(checks, func(c CheckRef) bool { return c.Context == "" })
	slices.SortStableFunc(checks, func(a, b CheckRef) int { return strings.Compare(a.Context, b.Context) })
	return slices.CompactFunc(checks, func(a, b CheckRef) bool { return a.Context == b.Context })
}

// secretRef matches a secrets-context reference such as secrets.CODECOV_TOKEN.
var secretRef = regexp.MustCompile(`\bsecrets\.([A-Za-z_][A-Za-z0-9_]*)`)

// workflowUses lists the secrets that the workflow files in dir reference and
// the environments their jobs name, each sorted and unique. A secret counts
// only when a YAML scalar references it, so a comment naming one adds nothing.
// A job names its environment either directly or as the name of an environment
// map.
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
		if ext := filepath.Ext(entry.Name()); entry.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		if secrets, environments, err = workflowFileUses(dir, entry.Name(), secrets, environments); err != nil {
			return nil, nil, err
		}
	}
	slices.Sort(secrets)
	slices.Sort(environments)
	return slices.Compact(secrets), slices.Compact(environments), nil
}

// workflowFileUses appends to secrets and environments what the workflow file
// dir/name references.
func workflowFileUses(dir, name string, secrets, environments []string) ([]string, []string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	named, err := jobEnvironments(&doc)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	return secretRefs(secrets, &doc), append(environments, named...), nil
}

// secretRefs appends to secrets the name of every secret the scalars under
// node reference.
func secretRefs(secrets []string, node *yaml.Node) []string {
	if node.Kind == yaml.ScalarNode {
		for _, match := range secretRef.FindAllStringSubmatch(node.Value, -1) {
			secrets = append(secrets, match[1])
		}
	}
	for _, child := range node.Content {
		secrets = secretRefs(secrets, child)
	}
	return secrets
}

// jobEnvironments lists the environments the jobs of a workflow document name,
// directly or as the name of an environment map.
func jobEnvironments(doc *yaml.Node) ([]string, error) {
	var wf struct {
		Jobs map[string]struct {
			Environment any `yaml:"environment"`
		} `yaml:"jobs"`
	}
	if err := doc.Decode(&wf); err != nil {
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
	slices.SortFunc(out, func(a, b workflowJob) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// excludes reports whether a main.yaml job is held back from the required
// checks, because its id matches an excluded prefix or its `if:` contains an
// excluded string.
func (e ExcludeRules) excludes(j workflowJob) bool {
	return slices.ContainsFunc(e.Prefixes, func(prefix string) bool { return strings.HasPrefix(j.ID, prefix) }) ||
		slices.ContainsFunc(e.IfContains, func(s string) bool { return s != "" && strings.Contains(j.If, s) })
}
