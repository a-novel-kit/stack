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
	answer := readAnswer(cmd.InOrStdin())
	return answer == "y" || answer == "yes"
}

// readAnswer reads one line of in, trimmed and lowercased.
func readAnswer(in io.Reader) string {
	line, _ := bufio.NewReader(in).ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line))
}

// newRepoCmd creates and configures GitHub repositories from the templates in
// internal/repocfg. Writes are interactive and human-only, while --dry-run
// prints the API operations and is safe anywhere.
func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Create and configure GitHub repositories (interactive, human-only)",
		Long: `Apply a repository's configuration, from its settings to its rulesets, as the
templates in cli/internal/repocfg/templates define it. A write asks for
confirmation in a terminal; --dry-run prints the API operations and applies
nothing.`,
	}
	cmd.AddCommand(newRepoCreateCmd(), newRepoUpdateCmd())
	return cmd
}

// repoCreateOpts holds the flags of `repo create`.
type repoCreateOpts struct {
	description, class, template string
	private                      bool
}

func newRepoCreateCmd() *cobra.Command {
	var o repoCreateOpts
	cmd := &cobra.Command{
		Use:   "create <org> <name>",
		Short: "Create a repository and apply its class config",
		Long: `Create <org>/<name>, from --template when given, then apply its class
configuration. The repository is public unless --private is set. The command
asks for confirmation in a terminal.`,
		Example: `  a-novel repo create a-novel infra --class infra`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return createRepo(cmd, repoEntry{Org: args[0], Name: args[1]}, o)
		},
	}
	cmd.Flags().StringVar(&o.class, "class", "", classFlagUsage())
	cmd.Flags().StringVar(&o.description, "description", "", "repository description")
	cmd.Flags().StringVar(&o.template, "template", "", "create from this org template repo (e.g. service-template)")
	cmd.Flags().BoolVar(&o.private, "private", false, "create a private repository (default public)")
	return cmd
}

// createRepo creates repo with gh after a confirmation in a terminal, then
// applies its class configuration.
func createRepo(cmd *cobra.Command, repo repoEntry, o repoCreateOpts) error {
	if !stdinIsTTY() {
		return errors.New("repo create is interactive (human-only); run it in a terminal")
	}
	preset, err := resolvePreset(repo, o.class)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	visibility, from := "public", ""
	if o.private {
		visibility = "private"
	}
	if o.template != "" {
		from = " from template " + repo.Org + "/" + o.template
	}
	if !confirm(cmd, fmt.Sprintf("Create %s %s%s (class %s)?", visibility, repo.FullName(), from, preset.Class)) {
		_, _ = fmt.Fprintln(out, "aborted.")
		return nil
	}

	createArgs := []string{"repo", "create", repo.FullName(), "--" + visibility}
	if o.description != "" {
		createArgs = append(createArgs, "--description", o.description)
	}
	if o.template != "" {
		createArgs = append(createArgs, "--template", repo.Org+"/"+o.template)
	}
	if ghOut, err := gh(createArgs...); err != nil {
		return fmt.Errorf("repo create: %w\n%s", err, ghOut)
	}
	_, _ = fmt.Fprintf(out, "✓ created %s\n", repo.FullName())
	return configureNewRepo(out, repo, o.class)
}

// configureNewRepo applies a freshly created repo's class config. The repo has
// no live ruleset or history yet, so its checks are discovered from a
// throwaway clone.
func configureNewRepo(out io.Writer, repo repoEntry, class string) error {
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
	_, _ = fmt.Fprintf(out, "\n▸ Applying %s config...\n", target.Class.Class)
	if err := applyPlan(out, target, plan); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "\n✓ %s created and configured.\n", repo.FullName())
	return nil
}

// classFlagUsage derives the accepted values from repocfg.AllClasses.
func classFlagUsage() string {
	classes := make([]string, len(repocfg.AllClasses))
	for i, class := range repocfg.AllClasses {
		classes[i] = string(class)
	}
	return "class (" + strings.Join(classes, "|") + "); a repos/<org>_<repo>.yaml override wins"
}

// repoUpdateOpts holds the flags of `repo update`.
type repoUpdateOpts struct {
	dryRun, jsonOut, all bool
	class, rootDir       string
	exclude              []string
	limit                int
}

func newRepoUpdateCmd() *cobra.Command {
	var o repoUpdateOpts
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Reconcile a repository's config to its class template",
		Long: `Reconcile the configuration of the repository you stand in, found from its
origin remote, with its class template or its repos/<org>_<repo>.yaml override.
The master ruleset requires every job of .github/workflows/main.yaml but the
reporting and master-only ones, plus the checks every repository requires.

--all reconciles every checkout of the workspace, after one confirmation. The
configuration is read from each working tree, so a checkout off its default
branch or with uncommitted changes is skipped.`,
		Example: `  a-novel repo update                       # current repo
  a-novel repo update --all                 # every pulled workspace repo
  a-novel repo update --all --exclude=service-template
  a-novel repo update --all --dry-run       # preview the whole batch`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.all {
				return reconcileAll(cmd, o)
			}
			return reconcileRepo(cmd, o)
		},
	}
	cmd.Flags().BoolVar(&o.dryRun, "dry-run", false, "print the API operations that would run, without applying")
	cmd.Flags().BoolVar(&o.jsonOut, "json", false, "print the plan(s) as JSON instead of applying (implies --dry-run)")
	cmd.Flags().StringVar(&o.class, "class", "", classFlagUsage())
	cmd.Flags().BoolVar(&o.all, "all", false, "reconcile every pulled workspace repo (stack + app/ + kit/) in one run")
	cmd.Flags().StringSliceVar(&o.exclude, "exclude", nil,
		"with --all, skip these repos (<org>/<name> or bare <name>); may be repeated")
	cmd.Flags().StringVar(&o.rootDir, "root", "",
		"with --all, the workspace root containing kit/ and app/ (defaults like `core sync`)")
	cmd.Flags().IntVarP(&o.limit, "jobs", "j", repoJobs,
		"with --all, max repos applied at once (GitHub rate-limits concurrent writes)")
	return cmd
}

// reconcileRepo reconciles the repository the working directory belongs to.
// --json and --dry-run print its plan; otherwise it applies the plan after a
// confirmation in a terminal.
func reconcileRepo(cmd *cobra.Command, o repoUpdateOpts) error {
	root, err := gitToplevel(".")
	if err != nil {
		return err
	}
	repo, err := repoFromGitRemote(root)
	if err != nil {
		return err
	}
	target, plan, err := planRepo(repo, root, o.class)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if o.jsonOut {
		return plan.RenderJSON(out)
	}
	if o.dryRun {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "# dry-run %s/%s — class %s\n# required checks: %s\n%s\n\n",
			target.Org, target.Repo, target.Class.Class, strings.Join(checkContexts(target), ", "),
			pruneImpact(target, plan))
		return plan.Render(out)
	}
	if !stdinIsTTY() {
		return errors.New("repo update is interactive (human-only); run it in a terminal, or use --dry-run")
	}

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

// toggle is a named on/off setting of a class preset.
type toggle struct {
	name string
	on   bool
}

// enabledNames lists the names of the toggles that are on, in order.
func enabledNames(toggles ...toggle) []string {
	var names []string
	for _, t := range toggles {
		if t.on {
			names = append(names, t.name)
		}
	}
	return names
}

// rulesetNames lists the rulesets the class applies.
func rulesetNames(c *repocfg.ClassPreset) []string {
	return enabledNames(
		toggle{"master", c.Rulesets.Master},
		toggle{"require-approval", c.Rulesets.RequireApproval},
		toggle{"tags", c.Rulesets.Tags},
	)
}

// onOff renders a setting's label green when on and muted with a ✗ when off.
func onOff(label string, on bool) string {
	if on {
		return ui.OK.Render(label)
	}
	return ui.Muted.Render(label + "✗")
}

// renderSummary prints a grouped overview of the desired config, the readable
// alternative to the raw API ops, shown before the apply confirm.
func renderSummary(w io.Writer, t *repocfg.RepoTarget) {
	c := t.Class
	line := func(label string, values ...string) {
		_, _ = fmt.Fprintf(w, "  %s  %s\n", ui.Gold.Render(fmt.Sprintf("%-10s", label)), strings.Join(values, " "))
	}

	_, _ = fmt.Fprintf(w, "%s — class %s\n", ui.Brand.Render(t.Org+"/"+t.Repo), c.Class)
	line("Features", onOff("issues", c.Features.Issues), onOff("projects", c.Features.Projects),
		onOff("discussions", c.Features.Discussions), onOff("wiki", c.Features.Wiki))
	merge := enabledNames(toggle{"squash", c.Merge.Squash}, toggle{"merge", c.Merge.MergeCommit}, toggle{"rebase", c.Merge.Rebase})
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
