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
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

//go:embed templates
var templatesFS embed.FS

// Class is a repository kind; each maps to one classes/<class>.yaml preset.
type Class string

const (
	// ClassService is a backend service, a service-* repository.
	ClassService Class = "service"
	// ClassPlatform is a frontend platform, a platform-* repository.
	ClassPlatform Class = "platform"
	// ClassInfra is the infra repository, whose merges deploy production.
	ClassInfra Class = "infra"
	// ClassLibrary is a library or a tool, the class of any other name.
	ClassLibrary Class = "library"
	// ClassWorkflows is the repository of the shared CI actions.
	ClassWorkflows Class = "workflows"
	// ClassMeta is an organization's .github repository.
	ClassMeta Class = "meta"
)

// AllClasses lists the known classes, in the order the --class help shows them.
var AllClasses = []Class{ClassService, ClassPlatform, ClassInfra, ClassLibrary, ClassWorkflows, ClassMeta}

// DetectClass infers a repo's class from its name. It applies when neither a
// repos/<org>_<repo>.yaml override nor --class names one. A name no case
// matches is a library.
func DetectClass(repo string) Class {
	switch {
	case repo == "infra":
		return ClassInfra
	case repo == "workflows":
		return ClassWorkflows
	case repo == ".github":
		return ClassMeta
	case strings.HasPrefix(repo, "service-"):
		return ClassService
	case strings.HasPrefix(repo, "platform-"):
		return ClassPlatform
	default:
		return ClassLibrary
	}
}

// OrgProfile is orgs/<org>.yaml, an org's bot ids and signing policy. A
// ruleset's generic bot name, a key of Bots, resolves to its App id here.
type OrgProfile struct {
	Org             string           `yaml:"org"`
	SigningRequired bool             `yaml:"signing_required"`
	Bots            map[string]int64 `yaml:"bots"`
	// ScopedSecrets are the org secrets with selected-repository visibility. A
	// repository can read one exactly when one of its workflows names it.
	ScopedSecrets []string `yaml:"scoped_secrets"`
	// Governance holds the privileged secrets, which no org secret carries.
	Governance Governance `yaml:"governance"`
}

// Governance is an org's privileged secrets. Each repository's governance
// environment holds the ones its workflows name, copied from a Bitwarden
// Secrets Manager project that keys each secret by its GitHub name.
type Governance struct {
	BWSProject   string   `json:"bws_project"    yaml:"bws_project"`
	BWSServerURL string   `json:"bws_server_url" yaml:"bws_server_url"`
	Secrets      []string `json:"secrets"        yaml:"secrets"`
}

// Features mirrors the repo "Features" toggles.
type Features struct {
	Issues      bool `yaml:"issues"`
	Wiki        bool `yaml:"wiki"`
	Projects    bool `yaml:"projects"`
	Discussions bool `yaml:"discussions"`
	// PullRequests is who may open pull requests: "all" or "collaborators_only".
	PullRequests string `yaml:"pull_requests"`
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

// SecurityToggles mirrors the repository security settings repocfg manages.
// DependabotAlerts has an endpoint of its own, and a nil value leaves the
// live setting as it is.
type SecurityToggles struct {
	SecretScanning   bool  `yaml:"secret_scanning"`
	PushProtection   bool  `yaml:"push_protection"`
	Dependabot       bool  `yaml:"dependabot"`
	DependabotAlerts *bool `yaml:"dependabot_alerts,omitempty"`
}

// ClassRulesets says which named rulesets the class applies.
type ClassRulesets struct {
	Master          bool `yaml:"master"`
	RequireApproval bool `yaml:"require_approval"`
	Tags            bool `yaml:"tags"`
	// AdminsOnly drops the bots from every bypass list, for a class whose
	// default branch no automation writes to directly.
	AdminsOnly bool `yaml:"admins_only"`
}

// ClassPreset is one classes/<class>.yaml file.
type ClassPreset struct {
	Class       Class           `yaml:"class"`
	Features    Features        `yaml:"features"`
	Merge       Merge           `yaml:"merge"`
	Security    SecurityToggles `yaml:"security"`
	Pages       bool            `yaml:"pages"`
	CodeQuality bool            `yaml:"code_quality"`
	CleanImages bool            `yaml:"clean_images"`
	Rulesets    ClassRulesets   `yaml:"rulesets"`
}

// CheckDef is one required status check (its integration key resolves to
// an app id via ChecksConfig.Integrations).
type CheckDef struct {
	Context     string `yaml:"context"`
	Integration string `yaml:"integration"`
}

// ExcludeRules narrows the main.yaml jobs that become required checks.
type ExcludeRules struct {
	// Prefixes excludes a job whose id starts with one, such as a reporting or
	// post-merge job.
	Prefixes []string `yaml:"prefixes"`
	// IfContains excludes a job whose `if:` holds one, such as a master-only
	// job, which never runs on a pull request and so cannot gate one.
	IfContains []string `yaml:"if_contains"`
}

// ChecksConfig is checks.yaml. Required checks are exactly the jobs declared in
// a repo's .github/workflows/main.yaml, minus Exclude, plus the Always set.
type ChecksConfig struct {
	Integrations map[string]int64 `yaml:"integrations"`
	Always       []CheckDef       `yaml:"always"`
	Exclude      ExcludeRules     `yaml:"exclude"`
}

// LoadClass reads and parses classes/<class>.yaml.
func LoadClass(c Class) (*ClassPreset, error) {
	return loadTemplate[ClassPreset](path.Join("classes", string(c)+".yaml"), fmt.Sprintf("class %q", c))
}

// LoadOrg reads and parses orgs/<org>.yaml.
func LoadOrg(org string) (*OrgProfile, error) {
	return loadTemplate[OrgProfile](path.Join("orgs", org+".yaml"), fmt.Sprintf("org %q", org))
}

// LoadChecks reads and parses checks.yaml.
func LoadChecks() (*ChecksConfig, error) {
	return loadTemplate[ChecksConfig]("checks.yaml", "checks.yaml")
}

// ResolveBotIntegrations fills the integration ids of the per-org bots. Each
// org runs its own Agent App with a distinct id, and both orgs load the same
// checks.yaml. Call it with the target repo's org profile before discovery, so
// a check that App posts, such as merge-gate, is required against it.
func (c *ChecksConfig) ResolveBotIntegrations(org *OrgProfile) {
	if c.Integrations == nil {
		c.Integrations = map[string]int64{}
	}
	if id := org.Bots["agent"]; id != 0 {
		c.Integrations["agent"] = id
	}
}

// LabelDef is one entry in the canonical label set: name, hex color (no
// leading '#', lower-case, as GitHub stores it) and description.
type LabelDef struct {
	Name        string `yaml:"name"`
	Color       string `yaml:"color"`
	Description string `yaml:"description"`
}

// LabelsConfig is governance/labels.yaml, the label set every repo carries.
// A label it names in neither list is left untouched.
type LabelsConfig struct {
	// Ensure labels are created when absent and updated when their color or
	// description drifts.
	Ensure []LabelDef `yaml:"ensure"`
	// Retire labels are deleted.
	Retire []string `yaml:"retire"`
}

// LoadLabels reads and parses governance/labels.yaml.
func LoadLabels() (*LabelsConfig, error) {
	return loadTemplate[LabelsConfig](path.Join("governance", "labels.yaml"), "labels.yaml")
}

// TeamTier is one contributor tier in teams.yaml: a team with no repository
// access, whose area teams hold Permission (a repository role such as push,
// maintain or admin) on their class's repositories.
type TeamTier struct {
	Name       string `yaml:"name"`
	Permission string `yaml:"permission"`
}

// TeamsConfig is teams.yaml: the contributor tiers every org carries.
type TeamsConfig struct {
	Tiers []TeamTier `yaml:"tiers"`
}

// LoadTeams reads and parses teams.yaml.
func LoadTeams() (*TeamsConfig, error) {
	return loadTemplate[TeamsConfig]("teams.yaml", "teams.yaml")
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
// pull_request, commit_message_pattern) are kept as maps so the template owns
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
	CommitMessagePattern map[string]any     `yaml:"commit_message_pattern"`
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
	return loadTemplate[RulesetSpec](path.Join("rulesets", name+".yaml"), fmt.Sprintf("ruleset %q", name))
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

// loadTemplate parses templates/<rel> into a T, naming the template what in
// an error.
func loadTemplate[T any](rel, what string) (*T, error) {
	var out T
	if err := loadTemplateYAML(rel, &out); err != nil {
		return nil, fmt.Errorf("load %s: %w", what, err)
	}
	return &out, nil
}

// loadTemplateYAML parses templates/<rel> into out, refusing unknown fields so
// a typo in a template fails loudly.
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

// ReadTemplate returns the bytes of templates/<rel>. An on-disk copy wins, so an
// edit needs no rebuild. The on-disk root is $REPO_CONFIG_DIR, else the default
// stack's cli/internal/repocfg/templates. A file missing or unreadable there
// falls back to the embedded copy.
func ReadTemplate(rel string) ([]byte, error) {
	if dir := onDiskTemplatesDir(); dir != "" {
		if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
			return b, nil
		}
	}
	return templatesFS.ReadFile(path.Join("templates", rel))
}

// onDiskTemplatesDir is the on-disk templates root, or "" when none resolves.
func onDiskTemplatesDir() string {
	if d := os.Getenv("REPO_CONFIG_DIR"); d != "" {
		return d
	}
	stack, err := stacks.Default()
	if err != nil {
		return ""
	}
	return filepath.Join(stack.Path, "cli", "internal", "repocfg", "templates")
}
