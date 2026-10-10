// Package compose reads the podman-compose files service repos ship: the
// slice of the format the CLI consumes, and the ${VAR} references it fills.
package compose

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// File mirrors the slice of a podman-compose file the CLI consumes. yaml.v3
// ignores keys absent from the struct, so a file can grow with podman-compose
// without breaking the parse.
type File struct {
	Services map[string]Service `yaml:"services"`
	// Volumes holds the top-level named volumes. Only their names matter, and
	// the common `postgres-data:` form leaves the value empty.
	Volumes map[string]struct{} `yaml:"volumes"`
}

// Service is one entry under `services:`.
type Service struct {
	Profiles    []string    `yaml:"profiles"`
	Build       *Build      `yaml:"build"`
	Ports       []string    `yaml:"ports"`
	DependsOn   DependsOn   `yaml:"depends_on"`
	Environment Environment `yaml:"environment"`
	// Healthcheck marks the presence of a healthcheck; only its existence
	// matters.
	Healthcheck *struct{} `yaml:"healthcheck"`
}

// Build carries the Dockerfile path, so a service declaring no healthcheck can
// be classified from its image's HEALTHCHECK instruction.
type Build struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

// DependsOn is the set of services a service depends on, from either YAML form:
//
//	depends_on: [svc1, svc2]              # short form — list of names
//	depends_on:                            # long form — map keyed by name
//	  svc1: { condition: service_healthy }
type DependsOn map[string]struct{}

// UnmarshalYAML accepts the short and the long form.
func (d *DependsOn) UnmarshalYAML(value *yaml.Node) error {
	out := DependsOn{}
	switch value.Kind {
	case yaml.SequenceNode:
		for _, n := range value.Content {
			out[n.Value] = struct{}{}
		}
	case yaml.MappingNode:
		if err := value.Decode((*map[string]struct{})(&out)); err != nil {
			return fmt.Errorf("decode depends_on (long form): %w", err)
		}
	default:
		return fmt.Errorf("depends_on must be a list or a map, got %v", value.Kind)
	}
	*d = out
	return nil
}

// Environment is a service's environment, from either YAML form:
//
//	environment: { KEY: value, KEY2: "${REF}" }   # map form
//	environment: [ KEY=value, KEY2=${REF} ]       # list form
type Environment map[string]string

// UnmarshalYAML accepts the map and the list form.
func (e *Environment) UnmarshalYAML(value *yaml.Node) error {
	out := Environment{}
	switch value.Kind {
	case yaml.MappingNode:
		if err := value.Decode((*map[string]string)(&out)); err != nil {
			return fmt.Errorf("decode environment (map form): %w", err)
		}
	case yaml.SequenceNode:
		var list []string
		if err := value.Decode(&list); err != nil {
			return fmt.Errorf("decode environment (list form): %w", err)
		}
		for _, kv := range list {
			if key, val, ok := strings.Cut(kv, "="); ok {
				out[key] = val
			}
		}
	default:
		return fmt.Errorf("environment must be a list or a map, got %v", value.Kind)
	}
	*e = out
	return nil
}

// ReadFile reads and parses the compose file at path.
func ReadFile(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	f, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return f, nil
}

// Parse parses a compose document.
func Parse(raw []byte) (*File, error) {
	var f File
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// refPattern matches a ${VAR} or ${VAR:-default} reference, capturing VAR. The
// default is matched but never applied.
var refPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-[^}]*)?\}`)

// Refs returns the distinct variables s references, in first-seen order.
func Refs(s string) []string {
	var out []string
	for _, m := range refPattern.FindAllStringSubmatch(s, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// Substitute resolves every reference in s against vars. An unknown one
// resolves to the empty string, as compose does, so a missing variable never
// survives as a literal ${VAR}.
func Substitute(s string, vars map[string]string) string {
	return refPattern.ReplaceAllStringFunc(s, func(ref string) string {
		return vars[refPattern.FindStringSubmatch(ref)[1]]
	})
}

// hostPortPattern matches a "${VAR}:5432" port mapping, whose host side the CLI
// allocates.
var hostPortPattern = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-[^}]*)?\}:(\d+)$`)

// HostPort splits a "${VAR}:5432" port mapping into the variable naming its
// host port and the container port. A literal mapping such as "5432:5432" has
// no variable and reports false.
func HostPort(mapping string) (string, string, bool) {
	m := hostPortPattern.FindStringSubmatch(strings.TrimSpace(mapping))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}
