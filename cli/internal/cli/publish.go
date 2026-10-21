package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

func newPublishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Release helpers",
		Long:  `Helpers for the release workflow, which cuts every release in CI.`,
	}
	cmd.AddCommand(newPublishStampCmd())
	return cmd
}

// newPublishStampCmd refreshes the vX.Y.Z references inside doc and config
// files, such as READMEs, OpenAPI specs and pinned action refs, to the repo's
// package version. A repo's prepublish:doc script calls it while the release
// workflow cuts a release, so those references track the released version.
func newPublishStampCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stamp <prefix> <file-or-glob>...",
		Short: "Refresh vX.Y.Z references in doc/config files to the current package version",
		Long: `Replace each <prefix>vX.Y.Z in the files with <prefix>v<version>, where
<version> is the version in the repository root's package.json. <prefix> is a
regular expression, and a file argument may be a glob, which the command
expands. A repository's prepublish:doc script runs it between the version bump
and the release commit. Matching no reference at all fails, so a stale pattern
shows before the release.`,
		Example: `  a-novel publish stamp 'version: ' openapi.yaml
  a-novel publish stamp 'a-novel/service-json-keys/[^/]+' README.md
  a-novel publish stamp 'a-novel-kit/workflows/[^@]+@' '*/*/action.yaml'`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return stampVersion(cmd.OutOrStdout(), args[0], args[1:])
		},
	}
}

// stampVersion stamps the repository's package version into every
// <prefix>vX.Y.Z reference of the files patterns resolve to.
func stampVersion(out io.Writer, prefix string, patterns []string) error {
	root, err := gitToplevel(".")
	if err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	version, err := readPackageVersion(root)
	if err != nil {
		return err
	}
	total, fileCount, err := stampTargets(patterns, prefix, version)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "✓ stamped %d reference(s) across %d file(s) to v%s\n", total, fileCount, version)
	return nil
}

// stampTargets stamps version into every reference matching prefix across the
// files the patterns resolve to, and reports the totals.
//
// The whole set matching nothing is an error, the command-level counterpart to
// resolveStampTargets refusing an empty file set. A prepublish:doc script exists
// to keep a doc reference in step with the release. A pattern that matches zero
// references has stopped tracking what it names, after a README rewrite changed
// the reference shape or a field was renamed, and stamping nothing would hide
// that until a release ships the stale reference.
func stampTargets(patterns []string, prefix, version string) (int, int, error) {
	re, err := stampPattern(prefix)
	if err != nil {
		return 0, 0, err
	}
	files, err := resolveStampTargets(patterns)
	if err != nil {
		return 0, 0, err
	}

	total := 0

	for _, f := range files {
		count, err := stampFile(f, re, version)
		if err != nil {
			return 0, 0, err
		}

		total += count
	}

	if total == 0 {
		return 0, 0, fmt.Errorf("publish: pattern %q matched no references across %d file(s): %s",
			prefix, len(files), strings.Join(files, " "))
	}

	return total, len(files), nil
}

// readPackageVersion returns the "version" field of root/package.json, the
// version a release has just bumped to.
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

// resolveStampTargets expands each file-or-glob argument into file paths,
// relative to the working directory, in order and without duplicates. No file
// at all is an error, which catches a mistyped path in a prepublish:doc script.
func resolveStampTargets(patterns []string) ([]string, error) {
	seen := map[string]bool{}
	var files []string
	for _, p := range patterns {
		matches, err := expandStampTarget(p)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				files = append(files, m)
			}
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("publish: no files matched: %s", strings.Join(patterns, " "))
	}
	return files, nil
}

// expandStampTarget returns p itself when it names an existing path, so a file
// name holding glob metacharacters stays literal. Otherwise it returns the
// paths p matches as a glob.
func expandStampTarget(p string) ([]string, error) {
	if _, err := os.Stat(p); err == nil {
		return []string{p}, nil
	}
	matches, err := filepath.Glob(p)
	if err != nil {
		return nil, fmt.Errorf("publish: bad glob %q: %w", p, err)
	}
	return matches, nil
}

// stampPattern matches `<prefix>vX.Y.Z`, prefix being a regular expression.
func stampPattern(prefix string) (*regexp.Regexp, error) {
	re, err := regexp.Compile("(" + prefix + ")v[0-9.]+")
	if err != nil {
		return nil, fmt.Errorf("publish: invalid prefix pattern %q: %w", prefix, err)
	}
	return re, nil
}

// stampFile rewrites every match of re, a `(<prefix>)v[0-9.]+` pattern, in the
// file at path to carry version, and reports how many references it rewrote.
func stampFile(path string, re *regexp.Regexp, version string) (int, error) {
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
