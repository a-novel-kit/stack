// Package repocfg models and applies a repository's GitHub configuration —
// general settings, security, CodeQL, Pages, topics and the per-repo
// rulesets — from editable YAML templates under templates/.
//
// The desired state for a repo is composed from three inputs:
//
//   - a CLASS preset (classes/<class>.yaml) — the per-kind toggles,
//   - an ORG profile (orgs/<org>.yaml) — bypass actors, team, signing,
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
	"fmt"
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
	ClassService     Class = "service"
	ClassGoLibrary   Class = "go-library"
	ClassNodeLibrary Class = "node-library"
	ClassWorkflows   Class = "workflows"
	ClassMeta        Class = "meta"
	ClassTooling     Class = "tooling"
	ClassAssets      Class = "assets"
)

// AllClasses is the ordered set of known classes (for the UI picker and
// flag validation).
var AllClasses = []Class{
	ClassService, ClassGoLibrary, ClassNodeLibrary,
	ClassWorkflows, ClassMeta, ClassTooling, ClassAssets,
}

// BypassActor is one ruleset bypass entry. ActorID is nil for
// OrganizationAdmin (which has no id).
type BypassActor struct {
	ActorType  string `yaml:"actor_type"`
	ActorID    *int64 `yaml:"actor_id,omitempty"`
	BypassMode string `yaml:"bypass_mode,omitempty"`
}

// OrgProfile is the per-org environment data kept out of code.
type OrgProfile struct {
	Org             string        `yaml:"org"`
	TeamID          int64         `yaml:"team_id"`
	BypassAlways    []BypassActor `yaml:"bypass_always"`
	BypassBots      []BypassActor `yaml:"bypass_bots"`
	SigningRequired bool          `yaml:"signing_required"`
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
	Private     bool            `yaml:"private"`
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

// LangRule maps a detected language to checks + CodeQL/Dependabot identifiers.
type LangRule struct {
	Detect     []string   `yaml:"detect"`
	Checks     []CheckDef `yaml:"checks"`
	CodeQL     []string   `yaml:"codeql"`
	Dependabot []string   `yaml:"dependabot"`
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
