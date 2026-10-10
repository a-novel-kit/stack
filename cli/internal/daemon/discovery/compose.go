package discovery

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// composeFile mirrors the slice of podman-compose.yaml that discovery consumes.
// yaml.v3 ignores keys absent from the struct, so a compose file can grow with
// podman-compose without breaking the parse.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
	// Volumes holds the top-level named volumes. Only their names matter, and
	// the common `json-keys-postgres-data:` form leaves the value empty.
	Volumes map[string]struct{} `yaml:"volumes"`
}

// composeService is one entry under `services:` in the compose file.
type composeService struct {
	Profiles    []string         `yaml:"profiles"`
	Build       *composeBuild    `yaml:"build"`
	Ports       []string         `yaml:"ports"`
	DependsOn   composeDependsOn `yaml:"depends_on"`
	Environment composeEnv       `yaml:"environment"`
	// Healthcheck marks the presence of a healthcheck; only its existence
	// matters.
	Healthcheck *struct{} `yaml:"healthcheck"`
}

// composeBuild carries the dockerfile path, so classification can peek for a
// HEALTHCHECK instruction when the compose service declares none.
type composeBuild struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

// composeDependsOn normalizes both YAML forms of depends_on into the set of
// service names depended on:
//
//	depends_on: [svc1, svc2]              # short form — list of names
//	depends_on:                            # long form — map keyed by name
//	  svc1: { condition: service_healthy }
//	  svc2: { condition: service_started }
type composeDependsOn map[string]struct{}

// UnmarshalYAML handles the polymorphic short/long form.
func (d *composeDependsOn) UnmarshalYAML(value *yaml.Node) error {
	out := composeDependsOn{}
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

// composeEnv normalizes both YAML forms of `environment:` to map[string]string:
//
//	environment: { KEY: value, KEY2: "${REF}" }   # map form
//	environment: [ KEY=value, KEY2=${REF} ]       # list form
type composeEnv map[string]string

func (e *composeEnv) UnmarshalYAML(value *yaml.Node) error {
	out := composeEnv{}
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

// parseComposeFile reads and unmarshals one podman-compose.yaml. Errors
// include the path for actionable messages.
func parseComposeFile(path string) (*composeFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cf composeFile
	if err := yaml.Unmarshal(raw, &cf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &cf, nil
}
