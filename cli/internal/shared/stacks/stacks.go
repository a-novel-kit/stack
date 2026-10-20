package stacks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

// DefaultName is the stack name used when A_NOVEL_STACKS is unset.
const DefaultName = "default"

// EnvVar names the environment variable that registers the stacks.
const EnvVar = "A_NOVEL_STACKS"

// Stack is one registered git-checkout the daemon manages.
type Stack struct {
	Name      string
	Path      string
	IsDefault bool
}

// Parse parses a $A_NOVEL_STACKS value into the ordered list of stacks, the
// first one marked default. An empty value yields the implicit default stack
// at ~/git-projects/a-novel, so a successful parse returns at least one stack.
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
		name, path, ok := strings.Cut(p, ":")
		if !ok || name == "" || path == "" {
			return nil, fmt.Errorf("%s entry %d: expected name:path, got %q", EnvVar, i+1, p)
		}
		name, path = strings.TrimSpace(name), strings.TrimSpace(path)
		if seen[name] {
			return nil, fmt.Errorf("%s entry %d: duplicate stack name %q", EnvVar, i+1, name)
		}
		seen[name] = true
		out = append(out, Stack{
			Name:      name,
			Path:      paths.ExpandHome(path),
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
		Path:      filepath.Join(paths.Home(), "git-projects", "a-novel"),
		IsDefault: true,
	}
}

// Default is the default stack, the first entry of $A_NOVEL_STACKS.
func Default() (Stack, error) {
	stk, err := ParseEnv()
	if err != nil {
		return Stack{}, err
	}
	return stk[0], nil
}
