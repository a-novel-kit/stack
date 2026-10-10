// `a-novel repo` — create and configure GitHub repositories from the editable
// templates in internal/repocfg. Writes are interactive and human-only;
// `--dry-run` prints the API operations that would run and is safe anywhere.

package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
	"github.com/a-novel-kit/stack/cli/internal/ui"
)

// repoJobs is how many repositories `repo update --all` reconciles at once by
// default. GitHub's secondary rate limit allows 900 REST points a minute, a
// write costing five, and one repository's apply spends about 60 points in
// some 20 seconds, so six concurrent applies stay under it.
const repoJobs = 6

// confirm prints prompt and reads a yes/no answer; only an explicit y/yes
// returns true, so a bare Enter is a safe "no".
func confirm(cmd *cobra.Command, prompt string) bool {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Create and configure GitHub repositories (interactive, human-only)",
		Long: `Apply the standard repository configuration (settings, security,
rulesets, Pages) from the templates in cli/internal/repocfg/templates.

Writes are interactive and human-only. '--dry-run' computes the desired
state and prints the raw API operations without applying anything.`,
	}
	cmd.AddCommand(newRepoCreateCmd(), newRepoUpdateCmd())
	return cmd
}

func newRepoCreateCmd() *cobra.Command {
	var description, class, template string
	var private bool
	cmd := &cobra.Command{
		Use:   "create <org> <name>",
		Short: "Create a repository and apply its class config",
		Long: `Creates <org>/<name> (optionally from a template), then discovers its
checks and applies the class configuration — settings, security,
rulesets and Pages. Interactive (human-only); repositories are public unless --private is set.`,
		Example: `  a-novel repo create a-novel infra --class infra`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := repoEntry{Org: args[0], Name: args[1]}
			if !stdinIsTTY() {
				return errors.New("repo create is interactive (human-only); run it in a terminal")
			}
			preset, err := resolvePreset(repo, class)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			visibility, from := "public", ""
			if private {
				visibility = "private"
			}
			if template != "" {
				from = " from template " + repo.Org + "/" + template
			}
			if !confirm(cmd, fmt.Sprintf("Create %s %s%s (class %s)?", visibility, repo.FullName(), from, preset.Class)) {
				_, _ = fmt.Fprintln(out, "aborted.")
				return nil
			}

			createArgs := []string{"repo", "create", repo.FullName(), "--" + visibility}
			if description != "" {
				createArgs = append(createArgs, "--description", description)
			}
			if template != "" {
				createArgs = append(createArgs, "--template", repo.Org+"/"+template)
			}
			if ghOut, err := gh(createArgs...); err != nil {
				return fmt.Errorf("repo create: %w\n%s", err, ghOut)
			}
			_, _ = fmt.Fprintf(out, "✓ created %s\n", repo.FullName())

			// A fresh repo has no live ruleset or history yet, so its checks are
			// discovered from a throwaway clone.
			tmp, err := os.MkdirTemp("", "repo-create-")
			if err != nil {
				return err
			}
			defer func() { _ = os.RemoveAll(tmp) }()
			cloneDir := filepath.Join(tmp, repo.Name)
			if _, err := gh("repo", "clone", repo.FullName(), cloneDir); err != nil {
				return fmt.Errorf("clone created repo for check discovery: %w", err)
			}
			target, plan, err := planRepo(repo, cloneDir, class)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(out)
			renderSummary(out, target)
			_, _ = fmt.Fprintf(out, "\n▸ Applying %s config...\n", preset.Class)
			if err := applyPlan(out, target, plan); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "\n✓ %s created and configured.\n", repo.FullName())
			return nil
		},
	}
	cmd.Flags().StringVar(&class, "class", "", classFlagUsage())
	cmd.Flags().StringVar(&description, "description", "", "repository description")
	cmd.Flags().StringVar(&template, "template", "", "create from this org template repo (e.g. service-template)")
	cmd.Flags().BoolVar(&private, "private", false, "create a private repository (default public)")
	return cmd
}

// classFlagUsage derives the accepted values from repocfg.AllClasses.
func classFlagUsage() string {
	classes := make([]string, len(repocfg.AllClasses))
	for i, class := range repocfg.AllClasses {
		classes[i] = string(class)
	}
	return "class (" + strings.Join(classes, "|") + "); a repos/<org>_<repo>.yaml override wins"
}

func newRepoUpdateCmd() *cobra.Command {
	var (
		dryRun, jsonOut, all bool
		class, rootDir       string
		exclude              []string
		limit                int
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Reconcile a repository's config to its class template",
		Long: `Run from inside a checked-out repo. Resolves the repo from its 'origin'
remote, picks the repos/<org>_<repo>.yaml override or the --class preset, and
reconciles config. The master ruleset's required checks are the jobs declared in
.github/workflows/main.yaml (minus reporting / master-only jobs) plus the
always-required set — set wholesale.

--all reconciles every pulled workspace repo instead: the stack repo plus each
whitelisted checkout present under app/ or kit/ (the same set 'core sync'
manages, from workspace-repos.yaml). Config is discovered from each working
tree, so a repo carrying ongoing work — off its default branch or with
uncommitted changes — is skipped untouched. --exclude drops named repos; a
single confirm gates the whole batch, which applies --jobs repos at once.`,
		Example: `  a-novel repo update                       # current repo
  a-novel repo update --all                 # every pulled workspace repo
  a-novel repo update --all --exclude=service-template
  a-novel repo update --all --dry-run       # preview the whole batch`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if all {
				return runRepoUpdateAll(cmd, rootDir, class, exclude, dryRun, jsonOut, limit)
			}
			root, err := gitToplevel(".")
			if err != nil {
				return err
			}
			repo, err := repoFromGitRemote(root)
			if err != nil {
				return err
			}
			target, plan, err := planRepo(repo, root, class)
			if err != nil {
				return err
			}
			if jsonOut {
				return plan.RenderJSON(cmd.OutOrStdout())
			}
			if dryRun {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "# dry-run %s/%s — class %s\n# required checks: %s\n%s\n\n",
					target.Org, target.Repo, target.Class.Class, strings.Join(checkContexts(target), ", "),
					pruneImpact(target, plan))
				return plan.Render(cmd.OutOrStdout())
			}
			if !stdinIsTTY() {
				return errors.New("repo update is interactive (human-only); run it in a terminal, or use --dry-run")
			}

			out := cmd.OutOrStdout()
			renderSummary(out, target)
			if !confirm(cmd, fmt.Sprintf("\nApply this configuration to %s/%s?", target.Org, target.Repo)) {
				_, _ = fmt.Fprintln(out, "aborted.")
				return nil
			}
			_, _ = fmt.Fprintln(out, "\n▸ Applying...")
			if err := applyPlan(out, target, plan); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "\n✓ %s/%s reconciled.\n", target.Org, target.Repo)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the API operations that would run, without applying")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "with --dry-run, emit the plan(s) as JSON")
	cmd.Flags().StringVar(&class, "class", "", classFlagUsage())
	cmd.Flags().BoolVar(&all, "all", false, "reconcile every pulled workspace repo (stack + app/ + kit/) in one run")
	cmd.Flags().StringSliceVar(&exclude, "exclude", nil,
		"with --all, skip these repos (<org>/<name> or bare <name>); may be repeated")
	cmd.Flags().StringVar(&rootDir, "root", "",
		"with --all, the workspace root containing kit/ and app/ (defaults like `core sync`)")
	cmd.Flags().IntVarP(&limit, "jobs", "j", repoJobs,
		"with --all, max repos applied at once (GitHub rate-limits concurrent writes)")
	return cmd
}

// planRepo discovers repo's config from its working tree at root and computes
// the plan, writing nothing. The org's [Agent] App id is injected before
// discovery, so the merge-gate check resolves to that org's App. Discovery
// reads the working tree, so a bulk caller must first confirm the checkout is
// clean and on its default branch.
func planRepo(repo repoEntry, root, class string) (*repocfg.RepoTarget, *repocfg.Plan, error) {
	preset, err := resolvePreset(repo, class)
	if err != nil {
		return nil, nil, err
	}
	orgProfile, err := repocfg.LoadOrg(repo.Org)
	if err != nil {
		return nil, nil, err
	}
	checks, err := repocfg.LoadChecks()
	if err != nil {
		return nil, nil, err
	}
	checks.ResolveBotIntegrations(orgProfile)
	discovered, err := repocfg.Discover(root, checks)
	if err != nil {
		return nil, nil, err
	}
	target := &repocfg.RepoTarget{
		Org:           repo.Org,
		Repo:          repo.Name,
		DefaultBranch: repoDefaultBranch(repo),
		Class:         preset,
		OrgProfile:    orgProfile,
		Checks:        checks,
		Discovered:    discovered,
	}
	plan, err := repocfg.BuildPlan(target)
	if err != nil {
		return nil, nil, err
	}
	return target, plan, nil
}

// resolvePreset prefers a repos/<org>_<repo>.yaml override, then the --class
// flag, then the class the repo name implies. The summary shows the result
// before the apply confirm.
func resolvePreset(repo repoEntry, class string) (*repocfg.ClassPreset, error) {
	if p, ok, err := repocfg.LoadRepoOverride(repo.Org, repo.Name); err != nil || ok {
		return p, err
	}
	if class == "" {
		class = string(repocfg.DetectClass(repo.Name))
	}
	return repocfg.LoadClass(repocfg.Class(class))
}

// repoDefaultBranch asks GitHub for the repo's default branch, falling back to
// master.
func repoDefaultBranch(repo repoEntry) string {
	out, err := gh("api", "repos/"+repo.FullName(), "--jq", ".default_branch")
	if b := strings.TrimSpace(out); err == nil && b != "" {
		return b
	}
	return branchMaster
}

// checkContexts lists the required checks the master ruleset will carry.
func checkContexts(t *repocfg.RepoTarget) []string {
	out := make([]string, len(t.Discovered.Checks))
	for i, c := range t.Discovered.Checks {
		out[i] = c.Context
	}
	return out
}

// rulesetNames lists the rulesets the class applies.
func rulesetNames(c *repocfg.ClassPreset) []string {
	var out []string
	for _, rs := range []struct {
		name string
		on   bool
	}{
		{"master", c.Rulesets.Master},
		{"require-approval", c.Rulesets.RequireApproval},
		{"tags", c.Rulesets.Tags},
	} {
		if rs.on {
			out = append(out, rs.name)
		}
	}
	return out
}

// renderSummary prints a grouped overview of the desired config, the readable
// alternative to the raw API ops, shown before the apply confirm.
func renderSummary(w io.Writer, t *repocfg.RepoTarget) {
	c := t.Class
	line := func(label string, values ...string) {
		_, _ = fmt.Fprintf(w, "  %s  %s\n", ui.Gold.Render(fmt.Sprintf("%-10s", label)), strings.Join(values, " "))
	}
	onOff := func(label string, on bool) string {
		if on {
			return ui.OK.Render(label)
		}
		return ui.Muted.Render(label + "✗")
	}

	_, _ = fmt.Fprintf(w, "%s — class %s\n", ui.Brand.Render(t.Org+"/"+t.Repo), c.Class)
	line("Features", onOff("issues", c.Features.Issues), onOff("projects", c.Features.Projects),
		onOff("discussions", c.Features.Discussions), onOff("wiki", c.Features.Wiki))
	var merge []string
	for _, m := range []struct {
		name string
		on   bool
	}{{"squash", c.Merge.Squash}, {"merge", c.Merge.MergeCommit}, {"rebase", c.Merge.Rebase}} {
		if m.on {
			merge = append(merge, m.name)
		}
	}
	mergeLine := strings.Join(merge, "+")
	if c.Merge.AutoMerge {
		mergeLine += ", auto-merge"
	}
	if c.Merge.SignoffRequired {
		mergeLine += ", signoff"
	}
	line("Merge", mergeLine)
	security := []string{
		onOff("secret-scanning", c.Security.SecretScanning),
		onOff("push-protection", c.Security.PushProtection),
		onOff("dependabot-updates", c.Security.Dependabot),
	}
	if c.Security.DependabotAlerts != nil {
		security = append(security, onOff("dependabot-alerts", *c.Security.DependabotAlerts))
	}
	line("Security", security...)
	pages := ui.Muted.Render("disabled")
	if c.Pages {
		pages = ui.OK.Render("enabled")
	}
	line("Pages", pages)
	line("Rulesets", strings.Join(rulesetNames(c), ", "))
	if checks := checkContexts(t); len(checks) > 0 {
		line("Checks", fmt.Sprintf("%s (%d)", strings.Join(checks, ", "), len(checks)))
	}
}
