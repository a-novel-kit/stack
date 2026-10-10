package cli

// The workspace: the stack checkout at its root, plus the a-novel and
// a-novel-kit repositories workspace-repos.yaml lists, cloned under app/ and
// kit/. `core sync` populates it; `repo update --all`, `core stacks prune` and
// `--sandbox` read it.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

// The two GitHub organizations the workspace routes.
const (
	orgAnovel    = "a-novel"
	orgAnovelKit = "a-novel-kit"
)

// repoWhitelistFile lists the workspace repositories, read from the workspace
// root at run time, so editing it needs no rebuild.
const repoWhitelistFile = "workspace-repos.yaml"

// repoEntry is one GitHub repository, "<org>/<name>".
type repoEntry struct {
	Org  string
	Name string
}

// stackRepo is the stack repository, checked out at the workspace root.
var stackRepo = repoEntry{Org: orgAnovelKit, Name: "stack"}

// FullName returns "<org>/<name>", the form flags take.
func (r repoEntry) FullName() string { return r.Org + "/" + r.Name }

// listed reports whether set names r, by its full or its bare name. Repo
// names are unique across both orgs, so a bare name is unambiguous.
func (r repoEntry) listed(set map[string]bool) bool { return set[r.FullName()] || set[r.Name] }

// SSHURL is the canonical clone URL.
func (r repoEntry) SSHURL() string { return "git@github.com:" + r.FullName() + ".git" }

// Dir is where r checks out under the workspace root: the stack at the root,
// a-novel-kit repositories under kit/, a-novel ones under app/.
func (r repoEntry) Dir(root string) string {
	switch {
	case r == stackRepo:
		return root
	case r.Org == orgAnovelKit:
		return filepath.Join(root, "kit", r.Name)
	default:
		return filepath.Join(root, "app", r.Name)
	}
}

// loadRepoWhitelist reads the repositories <root>/workspace-repos.yaml lists. A
// missing file lists none. Each entry is "<org>/<repo>" in one of the two
// routed orgs; anything else is an error, so a typo never routes a clone.
func loadRepoWhitelist(root string) ([]repoEntry, error) {
	data, err := os.ReadFile(filepath.Join(root, repoWhitelistFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", repoWhitelistFile, err)
	}
	var doc struct {
		Repos []string `yaml:"repos"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", repoWhitelistFile, err)
	}
	entries := make([]repoEntry, 0, len(doc.Repos))
	for _, raw := range doc.Repos {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		org, name, ok := strings.Cut(raw, "/")
		if !ok || org == "" || name == "" {
			return nil, fmt.Errorf("%s: invalid repo %q (want <org>/<name>)", repoWhitelistFile, raw)
		}
		if org != orgAnovel && org != orgAnovelKit {
			return nil, fmt.Errorf("%s: unknown org %q in %q (want %s or %s)",
				repoWhitelistFile, org, raw, orgAnovel, orgAnovelKit)
		}
		entries = append(entries, repoEntry{Org: org, Name: name})
	}
	return entries, nil
}

// checkout is a workspace repository present on disk.
type checkout struct {
	repoEntry

	dir string
}

// workspaceCheckouts lists the checkouts under root: the stack repository at
// root, resolved from its origin, then each whitelisted repository actually
// cloned. A whitelisted repository not cloned yet has nothing local to offer.
func workspaceCheckouts(root string) ([]checkout, error) {
	var out []checkout
	self, selfErr := repoFromGitRemote(root)
	if selfErr == nil {
		out = append(out, checkout{repoEntry: self, dir: root})
	}
	entries, err := loadRepoWhitelist(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		dir := e.Dir(root)
		if (selfErr == nil && e == self) || !exists(filepath.Join(dir, ".git")) {
			continue
		}
		out = append(out, checkout{repoEntry: e, dir: dir})
	}
	return out, nil
}

// workspaceRoot picks the workspace root: the --root flag, else the default
// stack's path, else the working directory.
func workspaceRoot(override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	if root, err := stacks.DefaultPath(); err == nil {
		return root, nil
	}
	return os.Getwd()
}

// normaliseFilter turns repeatable, comma-splittable flag values into a set.
func normaliseFilter(values []string) map[string]bool {
	out := map[string]bool{}
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out[part] = true
			}
		}
	}
	return out
}
