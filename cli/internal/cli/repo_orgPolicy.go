package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

const (
	orgPolicyConfigurationName        = "a-novel managed"
	orgPolicyConfigurationDescription = "Managed by a-novel repo update."
	orgPolicyConfigurationID          = "{configuration_id}"
	codeScanningDefaultSetupDisabled  = "disabled"
	orgPolicyScopeAll                 = "all"
	orgPolicySettingEnabled           = "enabled"
	orgPolicySettingNotSet            = "not_set"
	orgPolicyStatusFailed             = "failed"
)

// orgSecurityConfiguration is the organization security-configuration state
// needed to reconcile the managed repository policy.
type orgSecurityConfiguration struct {
	ID                                     int64          `json:"id"`
	TargetType                             string         `json:"target_type"`
	Name                                   string         `json:"name"`
	Description                            string         `json:"description"`
	AdvancedSecurity                       string         `json:"advanced_security"`
	DependencyGraph                        string         `json:"dependency_graph"`
	DependencyGraphAutosubmitAction        string         `json:"dependency_graph_autosubmit_action"`
	DependencyGraphAutosubmitActionOptions map[string]any `json:"dependency_graph_autosubmit_action_options"`
	DependabotAlerts                       string         `json:"dependabot_alerts"`
	DependabotSecurityUpdates              string         `json:"dependabot_security_updates"`
	CodeScanningDefaultSetup               string         `json:"code_scanning_default_setup"`
	CodeScanningDelegatedAlertDismissal    string         `json:"code_scanning_delegated_alert_dismissal"`
	SecretScanning                         string         `json:"secret_scanning"`
	SecretScanningPushProtection           string         `json:"secret_scanning_push_protection"`
	SecretScanningDelegatedBypass          string         `json:"secret_scanning_delegated_bypass"`
	SecretScanningValidityChecks           string         `json:"secret_scanning_validity_checks"`
	SecretScanningNonProviderPatterns      string         `json:"secret_scanning_non_provider_patterns"`
	SecretScanningGenericSecrets           string         `json:"secret_scanning_generic_secrets"`
	SecretScanningDelegatedAlertDismissal  string         `json:"secret_scanning_delegated_alert_dismissal"`
	SecretScanningExtendedMetadata         string         `json:"secret_scanning_extended_metadata"`
	PrivateVulnerabilityReporting          string         `json:"private_vulnerability_reporting"`
	Enforcement                            string         `json:"enforcement"`
}

// orgDefaultSecurityConfiguration is one configuration selected for new
// repositories in an organization.
type orgDefaultSecurityConfiguration struct {
	DefaultForNewRepos string                   `json:"default_for_new_repos"`
	Configuration      orgConfigurationIdentity `json:"configuration"`
}

// orgConfigurationIdentity handles both direct and value-wrapped identities
// returned by GitHub's configuration endpoints.
type orgConfigurationIdentity struct {
	ID    int64 `json:"id"`
	Value struct {
		ID int64 `json:"id"`
	} `json:"value"`
}

// resolvedID returns the configuration id from either response shape.
func (i orgConfigurationIdentity) resolvedID() int64 {
	if i.ID != 0 {
		return i.ID
	}
	return i.Value.ID
}

// orgSecurityConfigurationRepository is one repository attachment and its
// current reconciliation state.
type orgSecurityConfigurationRepository struct {
	Status     string                   `json:"status"`
	Repository orgConfigurationIdentity `json:"repository"`
}

// orgRepository is the repository identity needed to detect attachment drift.
type orgRepository struct {
	ID int64 `json:"id"`
}

// plannedOrgPolicy holds the organization operations that establish the
// managed repository security policy before repository config is reconciled.
type plannedOrgPolicy struct {
	org             string
	configurationID int64
	plan            *repocfg.Plan
}

// planOrgPolicies builds at most one policy plan for each organization found
// among the eligible repository updates.
func planOrgPolicies(items []plannedUpdate) ([]plannedOrgPolicy, error) {
	orgSet := make(map[string]struct{}, len(items))
	for _, item := range items {
		orgSet[item.cand.org] = struct{}{}
	}
	orgs := make([]string, 0, len(orgSet))
	for org := range orgSet {
		orgs = append(orgs, org)
	}
	slices.Sort(orgs)

	plans := make([]plannedOrgPolicy, 0, len(orgs))
	for _, org := range orgs {
		plan, err := planOrgPolicy(org)
		if err != nil {
			return nil, err
		}
		if len(plan.plan.Ops) > 0 {
			plans = append(plans, plan)
		}
	}
	return plans, nil
}

// planOrgPolicy returns the operations needed to establish the managed
// security configuration, make it the default, and attach it to every repo.
func planOrgPolicy(org string) (plannedOrgPolicy, error) {
	configs, err := loadPaginatedJSON[orgSecurityConfiguration](
		"orgs/" + org + "/code-security/configurations",
	)
	if err != nil {
		return plannedOrgPolicy{}, fmt.Errorf("read %s security configurations: %w", org, err)
	}

	var managed *orgSecurityConfiguration
	for i := range configs {
		if configs[i].TargetType != "organization" || configs[i].Name != orgPolicyConfigurationName {
			continue
		}
		if managed != nil {
			return plannedOrgPolicy{}, fmt.Errorf(
				"%s has multiple %q security configurations",
				org,
				orgPolicyConfigurationName,
			)
		}
		managed = &configs[i]
	}

	collectionPath := "orgs/" + org + "/code-security/configurations"
	desired := desiredOrgSecurityConfigurationBody()
	if managed == nil {
		configurationPath := collectionPath + "/" + orgPolicyConfigurationID
		return plannedOrgPolicy{
			org: org,
			plan: &repocfg.Plan{Ops: []repocfg.Op{
				{Method: http.MethodPost, Path: collectionPath, Body: desired},
				{
					Method: http.MethodPut,
					Path:   configurationPath + "/defaults",
					Body:   map[string]any{"default_for_new_repos": orgPolicyScopeAll},
				},
				{
					Method: http.MethodPost,
					Path:   configurationPath + "/attach",
					Body:   map[string]any{"scope": orgPolicyScopeAll},
				},
			}},
		}, nil
	}
	if managed.ID == 0 {
		return plannedOrgPolicy{}, fmt.Errorf("%s managed security configuration has no id", org)
	}

	configurationPath := fmt.Sprintf("%s/%d", collectionPath, managed.ID)
	plan := plannedOrgPolicy{
		org:             org,
		configurationID: managed.ID,
		plan:            &repocfg.Plan{},
	}
	if !orgSecurityConfigurationMatches(*managed, desired) {
		plan.plan.Ops = append(plan.plan.Ops, repocfg.Op{
			Method: http.MethodPatch,
			Path:   configurationPath,
			Body:   desired,
		})
	}
	isDefault, err := orgPolicyIsDefault(org, managed.ID)
	if err != nil {
		return plannedOrgPolicy{}, err
	}
	if !isDefault {
		plan.plan.Ops = append(plan.plan.Ops, repocfg.Op{
			Method: http.MethodPut,
			Path:   configurationPath + "/defaults",
			Body:   map[string]any{"default_for_new_repos": orgPolicyScopeAll},
		})
	}
	needsAttachment, err := orgPolicyNeedsAttachment(org, managed.ID)
	if err != nil {
		return plannedOrgPolicy{}, err
	}
	if needsAttachment {
		plan.plan.Ops = append(plan.plan.Ops, repocfg.Op{
			Method: http.MethodPost,
			Path:   configurationPath + "/attach",
			Body:   map[string]any{"scope": orgPolicyScopeAll},
		})
	}
	return plan, nil
}

// desiredOrgSecurityConfigurationBody returns the complete security policy
// applied to every repository. CodeQL default setup stays off while the other
// protections match the organizations' current GitHub-recommended settings.
func desiredOrgSecurityConfigurationBody() map[string]any {
	return map[string]any{
		"name":                               orgPolicyConfigurationName,
		keyDescription:                       orgPolicyConfigurationDescription,
		"advanced_security":                  orgPolicySettingEnabled,
		"dependency_graph":                   orgPolicySettingEnabled,
		"dependency_graph_autosubmit_action": orgPolicySettingNotSet,
		"dependency_graph_autosubmit_action_options": map[string]any{"labeled_runners": false},
		"dependabot_alerts":                          orgPolicySettingEnabled,
		"dependabot_security_updates":                orgPolicySettingNotSet,
		"code_scanning_default_setup":                codeScanningDefaultSetupDisabled,
		"code_scanning_delegated_alert_dismissal":    orgPolicySettingNotSet,
		"secret_scanning":                            orgPolicySettingEnabled,
		"secret_scanning_push_protection":            orgPolicySettingEnabled,
		"secret_scanning_delegated_bypass":           orgPolicySettingNotSet,
		"secret_scanning_validity_checks":            orgPolicySettingEnabled,
		"secret_scanning_non_provider_patterns":      orgPolicySettingEnabled,
		"secret_scanning_generic_secrets":            orgPolicySettingNotSet,
		"secret_scanning_delegated_alert_dismissal":  orgPolicySettingNotSet,
		"secret_scanning_extended_metadata":          orgPolicySettingEnabled,
		"private_vulnerability_reporting":            orgPolicySettingEnabled,
		"enforcement":                                "unenforced",
	}
}

// orgSecurityConfigurationMatches reports whether every managed policy field
// already matches the desired configuration.
func orgSecurityConfigurationMatches(config orgSecurityConfiguration, desired map[string]any) bool {
	raw, err := json.Marshal(config)
	if err != nil {
		return false
	}
	var current map[string]any
	if err := json.Unmarshal(raw, &current); err != nil {
		return false
	}
	for key, want := range desired {
		if !reflect.DeepEqual(current[key], want) {
			return false
		}
	}
	return true
}

// orgPolicyIsDefault reports whether the managed configuration applies to all
// newly-created repositories.
func orgPolicyIsDefault(org string, configurationID int64) (bool, error) {
	out, err := gh("api", "orgs/"+org+"/code-security/configurations/defaults")
	if err != nil {
		return false, fmt.Errorf("read %s default security configurations: %w", org, err)
	}
	var defaults []orgDefaultSecurityConfiguration
	if err := json.Unmarshal([]byte(out), &defaults); err != nil {
		return false, fmt.Errorf("decode %s default security configurations: %w", org, err)
	}
	for _, item := range defaults {
		if item.Configuration.resolvedID() == configurationID && item.DefaultForNewRepos == orgPolicyScopeAll {
			return true, nil
		}
	}
	return false, nil
}

// orgPolicyNeedsAttachment reports whether any organization repository has no
// association with the managed configuration. GitHub reports unsupported
// repos as failed; that terminal association must not retrigger an all-repo
// write.
func orgPolicyNeedsAttachment(org string, configurationID int64) (bool, error) {
	configurationPath := fmt.Sprintf(
		"orgs/%s/code-security/configurations/%d/repositories?status=all&per_page=100",
		org,
		configurationID,
	)
	attachments, err := loadPaginatedJSON[orgSecurityConfigurationRepository](configurationPath)
	if err != nil {
		return false, fmt.Errorf("read %s security configuration repositories: %w", org, err)
	}
	repositories, err := loadPaginatedJSON[orgRepository]("orgs/" + org + "/repos?type=all&per_page=100")
	if err != nil {
		return false, fmt.Errorf("read %s repositories: %w", org, err)
	}

	managedIDs := make(map[int64]struct{}, len(attachments))
	for _, attachment := range attachments {
		switch attachment.Status {
		case "attached", "attaching", "enforced", orgPolicyStatusFailed, "updating":
			managedIDs[attachment.Repository.resolvedID()] = struct{}{}
		}
	}
	for _, repo := range repositories {
		if _, ok := managedIDs[repo.ID]; !ok {
			return true, nil
		}
	}
	return false, nil
}

// loadPaginatedJSON reads every page from path and flattens the JSON arrays.
func loadPaginatedJSON[T any](path string) ([]T, error) {
	out, err := gh("api", path, "--paginate", "--slurp")
	if err != nil {
		return nil, err
	}
	var pages [][]T
	if err := json.Unmarshal([]byte(out), &pages); err != nil {
		return nil, fmt.Errorf("decode paginated response: %w", err)
	}
	var items []T
	for _, page := range pages {
		items = append(items, page...)
	}
	return items, nil
}

// applyOrgPolicy applies a managed organization policy plan. A creation's
// returned id is threaded into the default and attachment operations.
func applyOrgPolicy(plan plannedOrgPolicy) error {
	configurationID := plan.configurationID
	collectionPath := "orgs/" + plan.org + "/code-security/configurations"
	for _, op := range plan.plan.Ops {
		if configurationID == 0 && op.Method == http.MethodPost && op.Path == collectionPath {
			out, err := ghJSONOut(op.Method, op.Path, op.Body)
			if err != nil {
				return fmt.Errorf("create %s organization policy: %w", plan.org, err)
			}
			var created orgSecurityConfiguration
			if err := json.Unmarshal([]byte(out), &created); err != nil {
				return fmt.Errorf("decode %s organization policy creation: %w", plan.org, err)
			}
			if created.ID == 0 {
				return fmt.Errorf("create %s organization policy returned no id", plan.org)
			}
			configurationID = created.ID
			continue
		}

		path := op.Path
		if strings.Contains(path, orgPolicyConfigurationID) {
			if configurationID == 0 {
				return fmt.Errorf("apply %s organization policy without a configuration id", plan.org)
			}
			path = strings.ReplaceAll(path, orgPolicyConfigurationID, strconv.FormatInt(configurationID, 10))
		}
		if err := ghJSON(op.Method, path, op.Body); err != nil {
			return fmt.Errorf("apply %s organization policy: %w", plan.org, err)
		}
	}
	return nil
}
