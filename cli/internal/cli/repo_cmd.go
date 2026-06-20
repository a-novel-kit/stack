// `a-novel repo` — create and configure GitHub repositories from the
// editable templates in internal/repocfg. Writes are interactive
// (human-only, TTY-gated like publish); `--dry-run` prints the API
// operations that would run and is safe to use anywhere.
package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Create and configure GitHub repositories (interactive, human-only)",
		Long: `Apply the standard repository configuration (settings, security, CodeQL,
rulesets, Pages) from the templates in cli/internal/repocfg/templates.

Writes are interactive and human-only. '--dry-run' computes the desired
state and prints the raw API operations without applying anything.`,
	}
	cmd.AddCommand(newRepoUpdateCmd())
	return cmd
}

func newRepoUpdateCmd() *cobra.Command {
	var (
		dryRun bool
		class  string
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Reconcile the current repository's config to its class template",
		Long: `Run from inside a checked-out repo. Resolves the repo from its 'origin'
remote, picks the repos/<org>_<repo>.yaml override or the --class preset,
discovers required checks from the working tree, and reconciles config.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			org, repo, err := repoFromGitRemote(wd)
			if err != nil {
				return err
			}
			root, err := gitToplevel(wd)
			if err != nil {
				return err
			}

			preset, err := resolvePreset(org, repo, class)
			if err != nil {
				return err
			}
			orgProfile, err := repocfg.LoadOrg(org)
			if err != nil {
				return err
			}
			checks, err := repocfg.LoadChecks()
			if err != nil {
				return err
			}
			discovered, err := repocfg.Discover(root, checks)
			if err != nil {
				return err
			}

			branch := repoDefaultBranch(org, repo)
			plan, err := repocfg.BuildPlan(&repocfg.RepoTarget{
				Org:            org,
				Repo:           repo,
				DefaultBranch:  branch,
				Class:          preset,
				OrgProfile:     orgProfile,
				Checks:         checks,
				Discovered:     discovered,
				MasterChecks:   liveMasterChecks(org, repo),
				CodecovReports: codecovReports(org, repo, branch),
			})
			if err != nil {
				return err
			}

			if dryRun {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"# dry-run %s/%s — class %s\n# discovered checks: %s\n\n",
					org, repo, preset.Class, strings.Join(checkContexts(discovered.Checks), ", "))
				return plan.Render(cmd.OutOrStdout())
			}

			if !stdinIsTTY() {
				return errors.New("repo update is interactive (human-only); run it in a terminal, or use --dry-run")
			}
			return errors.New("apply is not implemented yet — use --dry-run to preview")
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the API operations that would run, without applying")
	cmd.Flags().StringVar(&class, "class", "", "class (service|library|workflows|meta); a repos/<org>_<repo>.yaml override wins")
	return cmd
}

// resolvePreset prefers a repos/<org>_<repo>.yaml override, else the --class flag.
func resolvePreset(org, repo, class string) (*repocfg.ClassPreset, error) {
	if p, ok, err := repocfg.LoadRepoOverride(org, repo); err != nil {
		return nil, err
	} else if ok {
		return p, nil
	}
	if class == "" {
		return nil, fmt.Errorf("no repos/%s_%s.yaml override; pass --class (service|library|workflows|meta)", org, repo)
	}
	return repocfg.LoadClass(repocfg.Class(class))
}

// repoFromGitRemote parses owner/repo from the origin remote of dir.
func repoFromGitRemote(dir string) (string, string, error) {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", "", fmt.Errorf("not in a git repo with an 'origin' remote: %w", err)
	}
	url := strings.TrimSuffix(strings.TrimSpace(string(out)), ".git")
	if i := strings.Index(url, "github.com"); i >= 0 {
		url = strings.TrimLeft(url[i+len("github.com"):], ":/")
	}
	parts := strings.Split(url, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("cannot parse owner/repo from origin url %q", strings.TrimSpace(string(out)))
	}
	return parts[len(parts)-2], parts[len(parts)-1], nil
}

// repoDefaultBranch asks GitHub for the repo's default branch; falls back to master.
func repoDefaultBranch(org, repo string) string {
	const fallback = "master"
	out, err := exec.Command("gh", "api", "repos/"+org+"/"+repo, "--jq", ".default_branch").Output()
	if err != nil {
		return fallback
	}
	if b := strings.TrimSpace(string(out)); b != "" {
		return b
	}
	return fallback
}

// liveMasterChecks returns the required status checks of the repo's live
// `master` ruleset, so update preserves them. Returns nil when there is no
// such ruleset (e.g. a brand-new repo), letting the plan fall back to the
// discovered set.
func liveMasterChecks(org, repo string) []repocfg.CheckRef {
	idOut, err := exec.Command("gh", "api", "repos/"+org+"/"+repo+"/rulesets",
		"--jq", `.[]|select(.name=="master").id`).Output()
	id := strings.TrimSpace(string(idOut))
	if err != nil || id == "" {
		return nil
	}
	out, err := exec.Command("gh", "api", "repos/"+org+"/"+repo+"/rulesets/"+id,
		"--jq", `.rules[]|select(.type=="required_status_checks").parameters.required_status_checks[]|[.context,(.integration_id//0|tostring)]|join("\t")`).Output()
	if err != nil {
		return nil
	}
	var checks []repocfg.CheckRef
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		ref := repocfg.CheckRef{Context: parts[0]}
		if len(parts) == 2 {
			if n, convErr := strconv.ParseInt(parts[1], 10, 64); convErr == nil {
				ref.IntegrationID = n
			}
		}
		checks = append(checks, ref)
	}
	return checks
}

// codecovReports reports whether Codecov posts a status check on the repo's
// default branch (gates codecov: auto).
func codecovReports(org, repo, branch string) bool {
	out, err := exec.Command("gh", "api", "repos/"+org+"/"+repo+"/commits/"+branch+"/status",
		"--jq", `[.statuses[].context]|map(select(startswith("codecov")))|length`).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != "0" && strings.TrimSpace(string(out)) != ""
}

func checkContexts(checks []repocfg.CheckRef) []string {
	cs := make([]string, len(checks))
	for i, c := range checks {
		cs[i] = c.Context
	}
	return cs
}
