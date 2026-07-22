// `a-novel publish stamp` refreshes the vX.Y.Z references inside doc and
// config files (READMEs, openapi specs, pinned action refs) so they match the
// repo's current package version. It is what a repo's prepublish:doc pnpm
// script calls during a release, so those references always track the released
// version.
//
// Releases themselves are cut in CI by the release-core GitHub Action.

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

// stdinIsTTY reports whether the CLI is attached to an interactive terminal. It
// is a package var so tests can drive the non-interactive branch without a real
// PTY. The human-only commands (repo create, repo update) gate on it: they
// prompt for confirmation, so an agent or CI run with no TTY is refused rather
// than driving them blind.
var stdinIsTTY = func() bool { return term.IsTerminal(os.Stdin.Fd()) }

func newPublishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Release helpers",
		Long: `Release helpers. Releases are cut in CI by the release-core GitHub
Action, not from a local working tree.

  publish stamp <prefix> <file>   refresh vX.Y.Z references inside a doc file`,
	}
	cmd.AddCommand(newPublishStampCmd())
	return cmd
}

func newPublishStampCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stamp <prefix> <file-or-glob>...",
		Short: "Refresh vX.Y.Z references in doc/config files to the current package version",
		Long: `Replaces every occurrence of <prefix>vX.Y.Z in each target with
<prefix>v<current-version>, where the current version is read from the repo
root's package.json. <prefix> is a regular expression.

Each target is a file path or a shell-style glob (expanded by the command,
relative to the working directory), so one invocation can stamp many files —
e.g. every composite action that pins a sibling action to a version.

This is what 'prepublish:doc' pnpm scripts call between the version bump and
the release commit, so module paths, spec versions and pinned action refs
inside docs/config always match the released version:

  a-novel publish stamp 'version: ' openapi.yaml
  a-novel publish stamp 'a-novel/service-json-keys/[^/]+' README.md
  a-novel publish stamp 'a-novel-kit/workflows/[^@]+@' '*/*/action.yaml'`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := gitToplevel(".")
			if err != nil {
				return err
			}
			version, err := readPackageVersion(root)
			if err != nil {
				return err
			}
			files, err := resolveStampTargets(args[1:])
			if err != nil {
				return err
			}
			total := 0
			for _, f := range files {
				count, err := stampFile(f, args[0], version)
				if err != nil {
					return err
				}
				total += count
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ stamped %d reference(s) across %d file(s) to v%s\n", total, len(files), version)
			return nil
		},
	}
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

// resolveStampTargets expands each file-or-glob argument into concrete file
// paths (relative to the working directory), preserving order and de-duping
// overlapping matches. An existing literal path is taken verbatim (so a real
// filename with glob metacharacters is never reinterpreted); otherwise the
// argument is globbed. It is an error for the whole set to resolve to no files
// — that catches a typo'd path or glob in a prepublish:doc script before it
// silently stamps nothing.
func resolveStampTargets(patterns []string) ([]string, error) {
	seen := map[string]bool{}
	var files []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}
	for _, p := range patterns {
		// A path that exists as-is is a literal target: take it verbatim so a
		// real filename containing glob metacharacters (e.g. weird[1].yaml) is
		// never reinterpreted as a pattern. Fall back to globbing only when the
		// literal path does not exist.
		if _, err := os.Stat(p); err == nil {
			add(p)
			continue
		}
		matches, err := filepath.Glob(p)
		if err != nil {
			return nil, fmt.Errorf("publish: bad glob %q: %w", p, err)
		}
		for _, m := range matches {
			add(m)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("publish: no files matched: %s", strings.Join(patterns, " "))
	}
	return files, nil
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
