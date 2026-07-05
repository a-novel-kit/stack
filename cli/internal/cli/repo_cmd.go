// `a-novel repo` — create and configure GitHub repositories from the
// editable templates in internal/repocfg. Writes are interactive
// (human-only, TTY-gated like publish); `--dry-run` prints the API
// operations that would run and is safe to use anywhere.
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

// branchMaster is the default branch name across the org's repos.
const branchMaster = "master"

// confirm prints prompt and reads a yes/no answer; only an explicit y/yes
// returns true (so a bare Enter is a safe "no").
func confirm(cmd *cobra.Command, prompt string) bool {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Create and configure GitHub repositories (interactive, human-only)",
		Long: `Apply the standard repository configuration (settings, security, CodeQL,
rulesets, Pages) from the templates in cli/internal/repocfg/templates.

Writes are interactive and human-only. '--dry-run' computes the desired
state and prints the raw API operations without applying anything.`,
	}
	cmd.AddCommand(newRepoCreateCmd())
	cmd.AddCommand(newRepoUpdateCmd())
	return cmd
}

func newRepoCreateCmd() *cobra.Command {
	var (
		description string
		class       string
		template    string
		private     bool
	)
	cmd := &cobra.Command{
		Use:   "create <org> <name>",
		Short: "Create a repository and apply its class config",
		Long: `Creates <org>/<name> (optionally from a template), then discovers its
checks and applies the class configuration — settings, security, CodeQL,
rulesets, Pages. Interactive (human-only); run it from anywhere.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			org, name := args[0], args[1]
			if !stdinIsTTY() {
				return errors.New("repo create is interactive (human-only); run it in a terminal")
			}
			preset, err := resolvePreset(org, name, class)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			tmpl := ""
			if template != "" {
				tmpl = " from template " + org + "/" + template
			}
			if !confirm(cmd, fmt.Sprintf("Create %s %s/%s%s (class %s)?", visibility(private), org, name, tmpl, preset.Class)) {
				_, _ = fmt.Fprintln(out, "aborted.")
				return nil
			}

			createArgs := []string{"repo", "create", org + "/" + name, "--" + visibility(private)}
			if description != "" {
				createArgs = append(createArgs, "--description", description)
			}
			if template != "" {
				createArgs = append(createArgs, "--template", org+"/"+template)
			}
			if cmdOut, err := gh(createArgs...); err != nil {
				return fmt.Errorf("repo create: %w\n%s", err, cmdOut)
			}
			_, _ = fmt.Fprintf(out, "✓ created %s/%s\n", org, name)

			// Discover the new repo's checks from a throwaway clone (a fresh
			// repo has no live ruleset or coverage history yet).
			tmp, err := os.MkdirTemp("", "repo-create-")
			if err != nil {
				return err
			}
			defer func() { _ = os.RemoveAll(tmp) }()
			cloneDir := filepath.Join(tmp, name)
			checks, err := repocfg.LoadChecks()
			if err != nil {
				return err
			}
			orgProfile, err := repocfg.LoadOrg(org)
			if err != nil {
				return err
			}
			// The [Agent] App id is per-org; inject it before discovery so the
			// merge-gate required check resolves to this org's App.
			checks.ResolveBotIntegrations(orgProfile)
			discovered := &repocfg.Discovered{}
			if _, err := gh("repo", "clone", org+"/"+name, cloneDir); err == nil {
				if d, derr := repocfg.Discover(cloneDir, checks); derr == nil {
					discovered = d
				}
			}
			branch := repoDefaultBranch(org, name)
			target := &repocfg.RepoTarget{
				Org:            org,
				Repo:           name,
				DefaultBranch:  branch,
				Class:          preset,
				OrgProfile:     orgProfile,
				Checks:         checks,
				Discovered:     discovered,
				CodecovReports: codecovReports(org, name, branch),
			}
			plan, err := repocfg.BuildPlan(target)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(out)
			renderSummary(out, target)
			_, _ = fmt.Fprintf(out, "\n▸ Applying %s config...\n", preset.Class)
			if err := applyPlan(out, org, name, branch, plan); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "\n✓ %s/%s created and configured.\n", org, name)
			return nil
		},
	}
	cmd.Flags().StringVar(&class, "class", "", "class (service|library|workflows|meta); a repos/<org>_<repo>.yaml override wins")
	cmd.Flags().StringVar(&description, "description", "", "repository description")
	cmd.Flags().StringVar(&template, "template", "", "create from this org template repo (e.g. service-template)")
	cmd.Flags().BoolVar(&private, "private", false, "create a private repository (default public)")
	return cmd
}

func visibility(private bool) string {
	if private {
		return "private"
	}
	return "public"
}

func newRepoUpdateCmd() *cobra.Command {
	var (
		dryRun  bool
		jsonOut bool
		class   string
		all     bool
		exclude []string
		rootDir string
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Reconcile a repository's config to its class template",
		Long: `Run from inside a checked-out repo. Resolves the repo from its 'origin'
remote, picks the repos/<org>_<repo>.yaml override or the --class preset, and
reconciles config. The master ruleset's required checks are the jobs declared in
.github/workflows/main.yaml (minus reporting / master-only jobs) plus the
always-required set — set wholesale.

--all reconciles every pulled workspace repo in one interactive pass instead:
the stack repo plus each whitelisted checkout present under app/ or kit/ (the
same set 'core sync' manages, auto-discovered from workspace-repos.yaml). Config
is discovered from each working tree, so a repo carrying ongoing work — off its
default branch or with uncommitted changes — is skipped untouched rather than
reconciled from an in-progress checkout. --exclude drops named repos; a single
confirm gates the whole batch.`,
		Example: `  a-novel repo update                       # current repo
  a-novel repo update --all                 # every pulled workspace repo
  a-novel repo update --all --exclude=service-template
  a-novel repo update --all --dry-run       # preview the whole batch`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if all {
				return runRepoUpdateAll(cmd, rootDir, class, exclude, dryRun, jsonOut)
			}
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			target, plan, err := buildRepoTarget(wd, class)
			if err != nil {
				return err
			}
			org, repo, branch := target.Org, target.Repo, target.DefaultBranch

			if jsonOut {
				return plan.RenderJSON(cmd.OutOrStdout())
			}
			if dryRun {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"# dry-run %s/%s — class %s\n# required checks: %s\n\n",
					org, repo, target.Class.Class, strings.Join(checkContexts(target.Discovered.Checks), ", "))
				return plan.Render(cmd.OutOrStdout())
			}

			if !stdinIsTTY() {
				return errors.New("repo update is interactive (human-only); run it in a terminal, or use --dry-run")
			}

			out := cmd.OutOrStdout()
			renderSummary(out, target)
			if !confirm(cmd, fmt.Sprintf("\nApply this configuration to %s/%s?", org, repo)) {
				_, _ = fmt.Fprintln(out, "aborted.")
				return nil
			}
			_, _ = fmt.Fprintln(out, "\n▸ Applying...")
			if err := applyPlan(out, org, repo, branch, plan); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "\n✓ %s/%s reconciled.\n", org, repo)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the API operations that would run, without applying")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "with --dry-run, emit the plan(s) as JSON")
	cmd.Flags().StringVar(&class, "class", "", "class (service|library|workflows|meta); a repos/<org>_<repo>.yaml override wins")
	cmd.Flags().BoolVar(&all, "all", false, "reconcile every pulled workspace repo (stack + app/ + kit/) in one run")
	cmd.Flags().StringSliceVar(&exclude, "exclude", nil,
		"with --all, skip these repos (<org>/<name> or bare <name>); may be repeated")
	cmd.Flags().StringVar(&rootDir, "root", "",
		"with --all, the workspace root containing kit/ and app/ (defaults like `core sync`)")
	return cmd
}

// buildRepoTarget resolves the repository that owns the checkout at dir (dir may
// be any path inside it), discovers its config from the working tree, and
// returns the reconcile target plus the computed plan. It performs no writes —
// callers gate the apply on --dry-run / a confirm. The org's [Agent] App id is
// injected before discovery so the merge-gate required check resolves to this
// org's App. Because discovery reads the working tree, callers reconciling in
// bulk must first confirm the checkout is on its default branch and clean (see
// runRepoUpdateAll) so the plan never reflects in-progress local state.
func buildRepoTarget(dir, class string) (*repocfg.RepoTarget, *repocfg.Plan, error) {
	org, repo, err := repoFromGitRemote(dir)
	if err != nil {
		return nil, nil, err
	}
	root, err := gitToplevel(dir)
	if err != nil {
		return nil, nil, err
	}
	preset, err := resolvePreset(org, repo, class)
	if err != nil {
		return nil, nil, err
	}
	orgProfile, err := repocfg.LoadOrg(org)
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

	branch := repoDefaultBranch(org, repo)
	target := &repocfg.RepoTarget{
		Org:            org,
		Repo:           repo,
		DefaultBranch:  branch,
		Class:          preset,
		OrgProfile:     orgProfile,
		Checks:         checks,
		Discovered:     discovered,
		CodecovReports: codecovReports(org, repo, branch),
	}
	plan, err := repocfg.BuildPlan(target)
	if err != nil {
		return nil, nil, err
	}
	return target, plan, nil
}

// resolvePreset prefers a repos/<org>_<repo>.yaml override, then the --class
// flag, then auto-discovery from the repo name (repocfg.DetectClass). It always
// resolves to a class — no interactive prompt — and the chosen class is shown in
// the summary before the apply confirm.
func resolvePreset(org, repo, class string) (*repocfg.ClassPreset, error) {
	if p, ok, err := repocfg.LoadRepoOverride(org, repo); err != nil {
		return nil, err
	} else if ok {
		return p, nil
	}
	if class == "" {
		class = string(repocfg.DetectClass(repo))
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
	out, err := exec.Command("gh", "api", "repos/"+org+"/"+repo, "--jq", ".default_branch").Output()
	if err != nil {
		return branchMaster
	}
	if b := strings.TrimSpace(string(out)); b != "" {
		return b
	}
	return branchMaster
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
