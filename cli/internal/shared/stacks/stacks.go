// Package stacks parses and validates the A_NOVEL_STACKS environment variable,
// the daemon's single source of truth for which checkouts to manage.
//
// Format: "name1:/path1,name2:/path2,..." — first entry is the default stack.
// Unset / empty: a single inferred stack named "default" at ~/git-projects/a-novel.
package stacks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultName is the stack name used when A_NOVEL_STACKS is unset.
const DefaultName = "default"

// EnvVar is the env var name read at daemon start.
const EnvVar = "A_NOVEL_STACKS"

// Stack is one registered git-checkout the daemon manages.
type Stack struct {
	Name      string
	Path      string
	IsDefault bool
}

// Parse reads $A_NOVEL_STACKS (or the provided raw string for testing) and
// returns the ordered list of stacks. The first entry is marked default.
// Returns at least one stack on success — falls back to the implicit default
// at ~/git-projects/a-novel if the env var is unset or empty.
func Parse(raw string) ([]Stack, error) {
	if raw == "" {
		return []Stack{implicitDefault()}, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]Stack, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		colon := strings.IndexByte(p, ':')
		if colon <= 0 || colon == len(p)-1 {
			return nil, fmt.Errorf("%s entry %d: expected name:path, got %q", EnvVar, i+1, p)
		}
		name, path := strings.TrimSpace(p[:colon]), strings.TrimSpace(p[colon+1:])
		if seen[name] {
			return nil, fmt.Errorf("%s entry %d: duplicate stack name %q", EnvVar, i+1, name)
		}
		seen[name] = true
		out = append(out, Stack{
			Name:      name,
			Path:      expandHome(path),
			IsDefault: len(out) == 0,
		})
	}
	if len(out) == 0 {
		return []Stack{implicitDefault()}, nil
	}
	return out, nil
}

// ParseEnv is Parse(os.Getenv(EnvVar)).
func ParseEnv() ([]Stack, error) { return Parse(os.Getenv(EnvVar)) }

// implicitDefault returns the fallback default stack when $A_NOVEL_STACKS is unset.
// Path follows the user's working-tree convention (`~/git-projects/a-novel`).
func implicitDefault() Stack {
	return Stack{
		Name:      DefaultName,
		Path:      filepath.Join(homeDir(), "git-projects", "a-novel"),
		IsDefault: true,
	}
}

// expandHome turns a leading ~ into $HOME. Conservative — only expands the
// special-case prefix; everything else is returned unchanged so accidental
// shell-expansion mistakes surface as broken paths rather than silently
// resolved ones.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(homeDir(), p[2:])
	}
	if p == "~" {
		return homeDir()
	}
	return p
}

func homeDir() string {
	if v := os.Getenv("HOME"); v != "" {
		return v
	}
	return "/"
}
