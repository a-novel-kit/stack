// Package repocfg models and applies a repository's GitHub configuration —
// general settings, security, CodeQL, Pages and the per-repo rulesets —
// from editable YAML templates under templates/.
//
// The desired state for a repo is composed from three inputs:
//
//   - a CLASS preset (classes/<class>.yaml) — the per-kind toggles,
//   - an ORG profile (orgs/<org>.yaml) — bypass actors and signing,
//   - semantically DISCOVERED checks (checks.yaml + the detect package),
//
// then overridden by CLI flags / the interactive form. The templates are
// plain YAML so they can be edited without touching Go; an on-disk copy
// (under the stack checkout, or $REPO_CONFIG_DIR) wins over the embedded
// copy so edits don't need a rebuild.
package repocfg

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

//go:embed templates
var templatesFS embed.FS

// Class is a repository kind; each maps to one classes/<class>.yaml preset.
type Class string

const (
	ClassService   Class = "service"
	ClassLibrary   Class = "library"
	ClassWorkflows Class = "workflows"
	ClassMeta      Class = "meta"
)

// AllClasses is the ordered set of known classes (for the UI picker and
// flag validation).
var AllClasses = []Class{ClassService, ClassLibrary, ClassWorkflows, ClassMeta}

// OrgProfile is orgs/<org>.yaml — per-org bot ids and signing policy. A
// ruleset's generic bot name (bots key) resolves to the app id here.
type OrgProfile struct {
	Org             string           `yaml:"org"`
	SigningRequired bool             `yaml:"signing_required"`
	Bots            map[string]int64 `yaml:"bots"`
}

// Features mirrors the repo "Features" toggles.
type Features struct {
	Issues      bool `yaml:"issues"`
	Wiki        bool `yaml:"wiki"`
	Projects    bool `yaml:"projects"`
	Discussions bool `yaml:"discussions"`
}

// Merge mirrors the merge-button + PR settings.
type Merge struct {
	Squash              bool `yaml:"squash"`
	MergeCommit         bool `yaml:"merge_commit"`
	Rebase              bool `yaml:"rebase"`
	AutoMerge           bool `yaml:"auto_merge"`
	DeleteBranchOnMerge bool `yaml:"delete_branch_on_merge"`
	AllowUpdateBranch   bool `yaml:"allow_update_branch"`
	SignoffRequired     bool `yaml:"signoff_required"`
}

// SecurityToggles mirrors the security_and_analysis block we manage.
type SecurityToggles struct {
	SecretScanning bool `yaml:"secret_scanning"`
	PushProtection bool `yaml:"push_protection"`
	Dependabot     bool `yaml:"dependabot"`
}

// CodeQLPreset configures CodeQL advanced setup. Languages are discovered,
// not declared here.
type CodeQLPreset struct {
	Enabled    bool   `yaml:"enabled"`
	QuerySuite string `yaml:"query_suite"`
}

// ClassRulesets says which named rulesets the class applies. codecov is
// driven separately by ClassPreset.Codecov.
type ClassRulesets struct {
	Master          bool `yaml:"master"`
	RequireApproval bool `yaml:"require_approval"`
	Tags            bool `yaml:"tags"`
}

// CodecovMode is auto | enabled | disabled (auto = on when the repo has
// tests). Kept a string, not a bool, to avoid the yaml off/on pitfall.
type CodecovMode string

const (
	CodecovAuto     CodecovMode = "auto"
	CodecovEnabled  CodecovMode = "enabled"
	CodecovDisabled CodecovMode = "disabled"
)

// ClassPreset is one classes/<class>.yaml file.
type ClassPreset struct {
	Class       Class           `yaml:"class"`
	Features    Features        `yaml:"features"`
	Merge       Merge           `yaml:"merge"`
	Security    SecurityToggles `yaml:"security"`
	CodeQL      CodeQLPreset    `yaml:"codeql"`
	Pages       bool            `yaml:"pages"`
	Codecov     CodecovMode     `yaml:"codecov"`
	CodeQuality bool            `yaml:"code_quality"`
	Rulesets    ClassRulesets   `yaml:"rulesets"`
}

// CheckDef is one required status check (its integration key resolves to
// an app id via ChecksConfig.Integrations).
type CheckDef struct {
	Context     string `yaml:"context"`
	Integration string `yaml:"integration"`
}

// LangRule maps a detected language to its checks + CodeQL languages.
type LangRule struct {
	Detect []string   `yaml:"detect"`
	Checks []CheckDef `yaml:"checks"`
	CodeQL []string   `yaml:"codeql"`
}

// FeatureRule maps a sub-feature (e.g. pkg/js) to extra checks.
type FeatureRule struct {
	Detect []string   `yaml:"detect"`
	Checks []CheckDef `yaml:"checks"`
}

// DockerRule formats a build-<target> check per detected docker target.
type DockerRule struct {
	ContextFormat string `yaml:"context_format"`
	Integration   string `yaml:"integration"`
}

// CodecovRule controls the codecov ruleset's auto-enable + its checks.
type CodecovRule struct {
	EnableWhenTests bool       `yaml:"enable_when_tests"`
	Checks          []CheckDef `yaml:"checks"`
}

// ChecksConfig is checks.yaml — the semantic discovery map.
type ChecksConfig struct {
	Integrations map[string]int64       `yaml:"integrations"`
	Always       []CheckDef             `yaml:"always"`
	Languages    map[string]LangRule    `yaml:"languages"`
	Features     map[string]FeatureRule `yaml:"features"`
	Docker       DockerRule             `yaml:"docker"`
	Codecov      CodecovRule            `yaml:"codecov"`
}

// LoadClass reads and parses classes/<class>.yaml.
func LoadClass(c Class) (*ClassPreset, error) {
	var p ClassPreset
	if err := loadTemplateYAML(path.Join("classes", string(c)+".yaml"), &p); err != nil {
		return nil, fmt.Errorf("load class %q: %w", c, err)
	}
	return &p, nil
}

// LoadOrg reads and parses orgs/<org>.yaml.
func LoadOrg(org string) (*OrgProfile, error) {
	var o OrgProfile
	if err := loadTemplateYAML(path.Join("orgs", org+".yaml"), &o); err != nil {
		return nil, fmt.Errorf("load org %q: %w", org, err)
	}
	return &o, nil
}

// LoadChecks reads and parses checks.yaml.
func LoadChecks() (*ChecksConfig, error) {
	var c ChecksConfig
	if err := loadTemplateYAML("checks.yaml", &c); err != nil {
		return nil, fmt.Errorf("load checks.yaml: %w", err)
	}
	return &c, nil
}

// RulesetSpec is one rulesets/<name>.yaml base. The required-status-checks
// list and concrete bypass actors are injected by the builder, not stored
// here.
type RulesetSpec struct {
	Name        string            `yaml:"name"`
	Target      string            `yaml:"target"`
	Enforcement string            `yaml:"enforcement"`
	Conditions  RulesetConditions `yaml:"conditions"`
	Bypass      []string          `yaml:"bypass"`
	Rules       RulesetRules      `yaml:"rules"`
}

// RulesetConditions is the ref-name match for a branch ruleset.
type RulesetConditions struct {
	RefName struct {
		Include []string `yaml:"include"`
		Exclude []string `yaml:"exclude"`
	} `yaml:"ref_name"`
}

// RulesetRules holds every rule a template may set; absent ones stay nil/
// false. Param blobs that pass straight through to the API (merge_queue,
// pull_request, copilot_code_review) are kept as maps so the template owns
// their shape.
type RulesetRules struct {
	Creation             bool               `yaml:"creation"`
	Update               bool               `yaml:"update"`
	Deletion             bool               `yaml:"deletion"`
	NonFastForward       bool               `yaml:"non_fast_forward"`
	RequiredSignatures   bool               `yaml:"required_signatures"`
	RequiredStatusChecks *RSCParams         `yaml:"required_status_checks"`
	MergeQueue           map[string]any     `yaml:"merge_queue"`
	CodeQuality          *CodeQualityParams `yaml:"code_quality"`
	PullRequest          map[string]any     `yaml:"pull_request"`
	CopilotCodeReview    map[string]any     `yaml:"copilot_code_review"`
}

// RSCParams is the static part of a required_status_checks rule; the checks
// list is injected from discovery.
type RSCParams struct {
	Strict               bool `yaml:"strict"`
	DoNotEnforceOnCreate bool `yaml:"do_not_enforce_on_create"`
}

// CodeQualityParams is the code_quality rule's parameters.
type CodeQualityParams struct {
	Severity string `yaml:"severity"`
}

// LoadRuleset reads rulesets/<name>.yaml.
func LoadRuleset(name string) (*RulesetSpec, error) {
	var r RulesetSpec
	if err := loadTemplateYAML(path.Join("rulesets", name+".yaml"), &r); err != nil {
		return nil, fmt.Errorf("load ruleset %q: %w", name, err)
	}
	return &r, nil
}

// LoadRepoOverride reads repos/<org>_<repo>.yaml if it exists. The bool is
// false (nil error) when the repo has no override and should use its class.
func LoadRepoOverride(org, repo string) (*ClassPreset, bool, error) {
	var p ClassPreset
	if err := loadTemplateYAML(path.Join("repos", org+"_"+repo+".yaml"), &p); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load repo override %s/%s: %w", org, repo, err)
	}
	return &p, true, nil
}

func loadTemplateYAML(rel string, out any) error {
	raw, err := ReadTemplate(rel)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("parse %s: %w", rel, err)
	}
	return nil
}

// ReadTemplate returns the bytes of templates/<rel>, preferring an on-disk
// copy so edits don't need a rebuild, falling back to the embedded copy.
// On-disk precedence: $REPO_CONFIG_DIR, else <stack>/cli/internal/repocfg/templates.
func ReadTemplate(rel string) ([]byte, error) {
	if dir := onDiskTemplatesDir(); dir != "" {
		if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
			return b, nil
		}
	}
	return templatesFS.ReadFile(path.Join("templates", rel))
}

func onDiskTemplatesDir() string {
	if d := os.Getenv("REPO_CONFIG_DIR"); d != "" {
		return d
	}
	stk, err := stacks.ParseEnv()
	if err != nil || len(stk) == 0 {
		return ""
	}
	return filepath.Join(stk[0].Path, "cli", "internal", "repocfg", "templates")
}
