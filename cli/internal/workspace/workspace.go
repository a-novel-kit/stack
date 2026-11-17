package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

// The two GitHub organizations the workspace routes.
const (
	OrgAnovel    = "a-novel"
	OrgAnovelKit = "a-novel-kit"
)

// ReposFile lists the workspace repositories, read from the workspace
// root at run time, so editing it needs no rebuild.
const ReposFile = "workspace-repos.yaml"

// Repo is one GitHub repository, "<org>/<name>".
type Repo struct {
	Org  string
	Name string
}

// StackRepo is the stack repository, checked out at the workspace root.
var StackRepo = Repo{Org: OrgAnovelKit, Name: "stack"}

// FullName returns "<org>/<name>", the form flags take.
func (r Repo) FullName() string { return r.Org + "/" + r.Name }

// Listed reports whether set names r, by its full or its bare name. Repo
// names are unique across both orgs, so a bare name is unambiguous.
func (r Repo) Listed(set map[string]bool) bool { return set[r.FullName()] || set[r.Name] }

// SSHURL is the canonical clone URL.
func (r Repo) SSHURL() string { return "git@github.com:" + r.FullName() + ".git" }

// Dir is where r checks out under the workspace root: the stack at the root,
// a-novel-kit repositories under kit/, a-novel ones under app/.
func (r Repo) Dir(root string) string {
	switch {
	case r == StackRepo:
		return root
	case r.Org == OrgAnovelKit:
		return filepath.Join(root, "kit", r.Name)
	default:
		return filepath.Join(root, "app", r.Name)
	}
}

// LoadRepos reads the repositories <root>/workspace-repos.yaml lists. A
// missing file lists none. Each entry is "<org>/<repo>" in one of the two
// routed orgs; anything else is an error, so a typo never routes a clone.
func LoadRepos(root string) ([]Repo, error) {
	data, err := os.ReadFile(filepath.Join(root, ReposFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ReposFile, err)
	}
	var doc struct {
		Repos []string `yaml:"repos"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ReposFile, err)
	}
	entries := make([]Repo, 0, len(doc.Repos))
	for _, raw := range doc.Repos {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		org, name, ok := strings.Cut(raw, "/")
		if !ok || org == "" || name == "" {
			return nil, fmt.Errorf("%s: invalid repo %q (want <org>/<name>)", ReposFile, raw)
		}
		if org != OrgAnovel && org != OrgAnovelKit {
			return nil, fmt.Errorf("%s: unknown org %q in %q (want %s or %s)",
				ReposFile, org, raw, OrgAnovel, OrgAnovelKit)
		}
		entries = append(entries, Repo{Org: org, Name: name})
	}
	return entries, nil
}

// Checkout is a workspace repository present on disk.
type Checkout struct {
	Repo

	Path string
}

// Checkouts lists the workspace checkouts under root: the stack
// repository at root, resolved from its origin, then each whitelisted
// repository cloned under app/ or kit/.
func Checkouts(root string) ([]Checkout, error) {
	var out []Checkout
	self, selfErr := Origin(root)
	if selfErr == nil {
		out = append(out, Checkout{Repo: self, Path: root})
	}
	entries, err := LoadRepos(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		dir := e.Dir(root)
		if (selfErr == nil && e == self) || !fsutil.Exists(filepath.Join(dir, ".git")) {
			continue
		}
		out = append(out, Checkout{Repo: e, Path: dir})
	}
	return out, nil
}

// Root picks the workspace root: the --root flag, else the default
// stack's path, else the working directory.
func Root(override string) (string, error) {
	if override != "" {
		return AbsRoot(override)
	}
	if stack, err := stacks.Default(); err == nil {
		return stack.Path, nil
	}
	return os.Getwd()
}

// AbsRoot resolves a --root value to an absolute path. A leading ~ expands to
// the home directory, since zsh leaves it as typed in --root=~/x.
func AbsRoot(root string) (string, error) {
	return filepath.Abs(paths.ExpandHome(root))
}
