// `a-novel publish` — cut a release locally: bump version files, commit,
// tag the release, push commit + tag. CI's release workflow fires on the
// pushed tag and does the rest (GitHub Release notes, Docker images, npm
// packages). The tag is vX.Y.Z for a root module and <subdir>/vX.Y.Z for a
// sub-directory module (see goTagPrefix).
//
// Two behaviors matter:
//
//  1. Workspace auto-detection. `pnpm version` needs different flags for a
//     pnpm workspace (--recursive) vs a single-package repo
//     (--no-git-tag-version); the command picks the right form so callers
//     don't hardcode (and drift) one — pnpm 11, for instance, rejects the
//     npm-style --workspaces flags.
//  2. A preflight before any mutation: branch == default, tree clean,
//     HEAD == origin/<default>, and a `git push --dry-run` that surfaces a
//     403 from branch protection without leaving the repo half-bumped.
//
// Who can actually publish is enforced server-side (branch protection on
// master + tag protection on v*). The preflight here is UX, not security.

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

// stdinIsTTY reports whether the CLI is attached to an interactive terminal.
// A package var (not a direct call) so tests can drive the non-interactive
// branch without a real PTY. Used to gate release-cutting (publish version):
// a release pushes to master and a protected tag, so it is human-only — an
// agent or CI run (no TTY) is refused. This is the cheap first gate; the real
// boundary is the token's GitHub rights (a non-interactive token must lack
// push-to-master / tag-create), which this CLI cannot weaken.
var stdinIsTTY = func() bool { return term.IsTerminal(os.Stdin.Fd()) }

func newPublishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Cut and push a release from the local working tree",
		Long: `Release helpers. Releases are created locally by a developer with push
rights, not by CI — the end-to-end sequence is: bump version files, commit,
tag vX.Y.Z, push commit + tag. CI's release workflow then fires on the
pushed tag.

  publish version <new-version>   the full sequence above, with preflight
  publish stamp <prefix> <file>   refresh vX.Y.Z references inside a doc file`,
	}
	cmd.AddCommand(newPublishVersionCmd())
	cmd.AddCommand(newPublishStampCmd())
	return cmd
}

func newPublishVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version <new-version>",
		Short: "Bump, commit, tag and push a new release version",
		Long: `Bumps the repo version to <new-version> (an explicit semver like 0.21.0,
or a pnpm increment keyword: patch, minor, major), commits the change with
the version as the message, tags it (v<version>, or <subdir>/v<version> when
the Go module lives in a sub-directory, as the CLI's does under cli/), and
pushes commit + tag.

The bump is workspace-aware: a repo with pnpm-workspace.yaml is bumped with
'pnpm version --recursive' (root + every member in one pass), a
single-package repo with 'pnpm version --no-git-tag-version'. Either way
this command owns the commit and the tag, never pnpm.

If the repo declares a 'prepublish:doc' pnpm script it runs between the
bump and the commit, so version references inside docs (README, openapi)
are refreshed with the new version (see 'a-novel publish stamp').

Preflight — all checks pass before anything is mutated:

  - current branch is the default branch (master)
  - working tree is clean
  - HEAD matches origin's default branch head
  - 'git push --dry-run' succeeds (surfaces branch-protection 403s early)

This command is interactive-only: it refuses to run without a terminal, so
an agent or CI job cannot cut a release. Publishing rights are enforced
server-side by branch protection on master and tag protection on v* (a
non-interactive token must lack both); the terminal check and this
preflight are UX, not the security boundary.`,
		Example: `  a-novel publish version 0.21.0
  a-novel publish version patch`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			// Release-cutting is human-only. Refuse before touching anything
			// when there's no terminal — an agent / CI run must not push to
			// master or create a release tag. (Defence in depth: the
			// non-interactive token should also lack those rights server-side.)
			if !stdinIsTTY() {
				return errors.New("publish version: refusing to run non-interactively — " +
					"a release pushes to master and a protected tag, so it must be cut by a " +
					"human from a terminal. Run it yourself; an agent/CI token should not be " +
					"able to publish")
			}
			root, err := gitToplevel(".")
			if err != nil {
				return err
			}
			def := resolveDefaultBranch(root)
			if err = publishPreflight(root, def); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "▸ Preflight OK (%s, clean, in sync with origin/%s)\n", root, def)

			if err = bumpVersion(out, root, args[0]); err != nil {
				return err
			}
			if err = runPnpm(out, root, "run", "--if-present", "prepublish:doc"); err != nil {
				return fmt.Errorf("publish: prepublish:doc: %w", err)
			}
			version, err := readPackageVersion(root)
			if err != nil {
				return err
			}
			tagPrefix, err := goTagPrefix(root)
			if err != nil {
				return err
			}
			tag := tagPrefix + "v" + version

			// runGit captures all output (replayed only on error), so no
			// --quiet flags are needed to keep success runs clean.
			steps := [][]string{
				{"add", "-A"},
				{"commit", "-m", version},
				{"tag", tag},
				{"push", "origin", def},
				{"push", "origin", "refs/tags/" + tag},
			}
			for _, step := range steps {
				if cmdOut, stepErr := runGit(root, step...); stepErr != nil {
					return fmt.Errorf("publish: git %s: %w\n%s", strings.Join(step, " "), stepErr, strings.TrimSpace(cmdOut))
				}
			}
			_, _ = fmt.Fprintf(out, "✓ released %s — pushed %s and tag %s\n", version, def, tag)
			return nil
		},
	}
}

func newPublishStampCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stamp <prefix> <file>",
		Short: "Refresh vX.Y.Z references in a doc file to the current package version",
		Long: `Replaces every occurrence of <prefix>vX.Y.Z in <file> with
<prefix>v<current-version>, where the current version is read from the repo
root's package.json. <prefix> is a regular expression.

This is what 'prepublish:doc' pnpm scripts call between the version bump and
the release commit, so module paths and spec versions inside docs always
match the released version:

  a-novel publish stamp 'version: ' openapi.yaml
  a-novel publish stamp 'a-novel/service-json-keys/[^/]+' README.md`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := gitToplevel(".")
			if err != nil {
				return err
			}
			version, err := readPackageVersion(root)
			if err != nil {
				return err
			}
			count, err := stampFile(args[1], args[0], version)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ stamped %d reference(s) in %s to v%s\n", count, args[1], version)
			return nil
		},
	}
}

// publishPreflight runs every release precondition against the repo at
// root. It mutates nothing — the point is to fail before the version bump,
// not after, so a refused release never leaves the repo half-bumped.
func publishPreflight(root, def string) error {
	branchOut, err := runGit(root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return fmt.Errorf("publish: resolve current branch: %w", err)
	}
	if branch := strings.TrimSpace(branchOut); branch != def {
		return fmt.Errorf("publish: releases are cut from %s, current branch is %s", def, branch)
	}
	statusOut, err := runGit(root, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("publish: git status: %w", err)
	}
	if strings.TrimSpace(statusOut) != "" {
		return fmt.Errorf("publish: working tree is not clean — commit or stash first:\n%s", strings.TrimSpace(statusOut))
	}
	localSHA, err := runGit(root, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("publish: rev-parse HEAD: %w", err)
	}
	remoteSHA, err := runGit(root, "rev-parse", "refs/remotes/origin/"+def)
	if err != nil {
		return fmt.Errorf("publish: rev-parse origin/%s: %w", def, err)
	}
	if strings.TrimSpace(localSHA) != strings.TrimSpace(remoteSHA) {
		return fmt.Errorf("publish: HEAD is not in sync with origin/%s — pull or push first", def)
	}
	if cmdOut, dryErr := runGit(root, "push", "--quiet", "--dry-run", "origin", def); dryErr != nil {
		return fmt.Errorf("publish: push dry-run refused (missing rights? branch protection?): %w\n%s",
			dryErr, strings.TrimSpace(cmdOut))
	}
	if _, statErr := os.Stat(filepath.Join(root, "package.json")); statErr != nil {
		return fmt.Errorf("publish: no package.json at repo root %s — nothing to version", root)
	}
	return nil
}

// bumpVersion runs the workspace-aware `pnpm version` form. pnpm always
// skips its own git commit/tag in recursive mode, and --no-git-tag-version
// forces the same in single-package mode — the publish command owns the
// commit/tag either way.
func bumpVersion(out io.Writer, root, newVersion string) error {
	mode := "--no-git-tag-version"
	if isPnpmWorkspace(root) {
		mode = "--recursive"
	}
	args := []string{"version", newVersion, mode}
	if err := runPnpm(out, root, args...); err != nil {
		return fmt.Errorf("publish: pnpm %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// isPnpmWorkspace reports whether the repo at root is a pnpm workspace —
// the presence of pnpm-workspace.yaml is pnpm's own definition of one.
func isPnpmWorkspace(root string) bool {
	_, err := os.Stat(filepath.Join(root, "pnpm-workspace.yaml"))
	return err == nil
}

// readPackageVersion returns the "version" field of root/package.json —
// the post-bump source of truth for the commit message and tag name.
func readPackageVersion(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return "", fmt.Errorf("publish: read package.json: %w", err)
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err = json.Unmarshal(raw, &pkg); err != nil {
		return "", fmt.Errorf("publish: parse package.json: %w", err)
	}
	if pkg.Version == "" {
		return "", fmt.Errorf("publish: package.json at %s has no version field", root)
	}
	return pkg.Version, nil
}

// goModFile is the Go module manifest's fixed file name.
const goModFile = "go.mod"

// goTagPrefix returns the prefix Go requires on this repo's version tags.
// A module at the repo root is released with plain "vX.Y.Z" tags (prefix
// ""); a module in a sub-directory is released with tags prefixed by its
// path, because that is how Go resolves a sub-directory module — e.g. the
// a-novel CLI lives in cli/, so module github.com/a-novel-kit/stack/cli is
// released as cli/vX.Y.Z and `go install …/cli/cmd/a-novel@vX.Y.Z` only
// resolves against that prefixed tag.
//
// Detection keys on the single *tracked* go.mod, so gitignored sibling
// checkouts (app/, kit/) that carry their own go.mod do not count, and a
// root go.mod wins over any nested one. No tracked go.mod (a JS-only repo)
// yields "". Multiple sub-directory modules are refused — there is no single
// release tag to derive.
func goTagPrefix(root string) (string, error) {
	out, err := runGit(root, "ls-files", "--", "*go.mod")
	if err != nil {
		return "", fmt.Errorf("publish: list tracked go.mod files: %w", err)
	}
	var subdirs []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		// git emits slash-separated paths; the pathspec also matches
		// lookalikes (e.g. cargo.mod), so require an exact base name.
		if line == "" || path.Base(line) != goModFile {
			continue
		}
		dir := path.Dir(line)
		if dir == "." {
			return "", nil // module at the repo root → plain vX.Y.Z
		}
		subdirs = append(subdirs, dir)
	}
	switch len(subdirs) {
	case 0:
		return "", nil // no Go module → plain vX.Y.Z
	case 1:
		return subdirs[0] + "/", nil // sub-directory module → <dir>/vX.Y.Z
	default:
		return "", fmt.Errorf("publish: %d Go modules tracked (%s) — can't derive a single release tag; tag manually",
			len(subdirs), strings.Join(subdirs, ", "))
	}
}

// stampFile rewrites every `<prefix>vX.Y.Z` occurrence in the file at path
// to carry version, and reports how many references were rewritten. prefix
// is a regular expression; the version-number tail it stamps over is always
// `v[0-9.]+`.
func stampFile(path, prefix, version string) (int, error) {
	re, err := regexp.Compile("(" + prefix + ")v[0-9.]+")
	if err != nil {
		return 0, fmt.Errorf("publish: invalid prefix pattern %q: %w", prefix, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("publish: stat %s: %w", path, err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("publish: read %s: %w", path, err)
	}
	matches := re.FindAll(content, -1)
	if len(matches) == 0 {
		return 0, nil
	}
	stamped := re.ReplaceAll(content, []byte("${1}v"+version))
	if err = os.WriteFile(path, stamped, info.Mode().Perm()); err != nil {
		return 0, fmt.Errorf("publish: write %s: %w", path, err)
	}
	return len(matches), nil
}

// runPnpm runs `pnpm <args...>` from dir, streaming output to out. Used
// for the version bump and the optional prepublish:doc script — both want
// their progress visible, unlike the git plumbing around them.
func runPnpm(out io.Writer, dir string, args ...string) error {
	c := exec.Command("pnpm", args...)
	c.Dir = dir
	c.Stdout = out
	c.Stderr = out
	return c.Run()
}

// gitToplevel resolves the repo root containing dir, so publish commands
// can run from anywhere inside the repo (pnpm scripts run them from the
// root; humans may not).
func gitToplevel(dir string) (string, error) {
	out, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("publish: not inside a git repository (%s): %w", dir, err)
	}
	return strings.TrimSpace(out), nil
}
