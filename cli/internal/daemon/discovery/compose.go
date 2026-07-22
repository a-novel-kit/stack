package discovery

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// composeFile mirrors the slice of podman-compose.yaml that discovery consumes.
// yaml.v3 ignores keys absent from the struct, so a compose file can grow with
// podman-compose without breaking the parse.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
	Volumes  map[string]composeVolume  `yaml:"volumes"`
	Networks map[string]composeNetwork `yaml:"networks"`
}

// composeService is one entry under `services:` in the compose file.
type composeService struct {
	Profiles    []string         `yaml:"profiles"`
	Build       *composeBuild    `yaml:"build"`
	Ports       []string         `yaml:"ports"`
	DependsOn   composeDependsOn `yaml:"depends_on"`
	Environment composeEnv       `yaml:"environment"`
	Healthcheck *composeHC       `yaml:"healthcheck"`
	Networks    []string         `yaml:"networks"`
	Volumes     []string         `yaml:"volumes"`
}

// composeBuild carries the dockerfile path, so classification can peek for a
// HEALTHCHECK instruction when the compose service declares none.
type composeBuild struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

// composeHC marks the presence of a healthcheck; only its existence matters.
type composeHC struct {
	Test     []string `yaml:"test"`
	Interval string   `yaml:"interval"`
	Timeout  string   `yaml:"timeout"`
	Retries  int      `yaml:"retries"`
}

// composeVolume is the named volume's value under top-level `volumes:`. It may
// be empty, as in the common `json-keys-postgres-data:` form.
type composeVolume struct {
	External bool `yaml:"external"`
}

// composeNetwork is the named network's value under top-level `networks:`.
type composeNetwork struct {
	External bool `yaml:"external"`
}

// composeDependsOn normalizes both YAML forms of depends_on into one map:
//
//	depends_on: [svc1, svc2]              # short form — list of names
//	depends_on:                            # long form — map keyed by name
//	  svc1: { condition: service_healthy }
//	  svc2: { condition: service_started }
//
// The long form carries the condition; the short form yields an empty entry.
type composeDependsOn map[string]composeDependsOnEntry

type composeDependsOnEntry struct {
	Condition string `yaml:"condition"`
}

// UnmarshalYAML handles the polymorphic short/long form.
func (d *composeDependsOn) UnmarshalYAML(value *yaml.Node) error {
	if d == nil {
		return nil
	}
	out := composeDependsOn{}
	switch value.Kind {
	case yaml.SequenceNode:
		// Short form: list of service names.
		for _, n := range value.Content {
			out[n.Value] = composeDependsOnEntry{}
		}
	case yaml.MappingNode:
		// Long form: map of name → entry.
		var raw map[string]composeDependsOnEntry
		if err := value.Decode(&raw); err != nil {
			return fmt.Errorf("decode depends_on (long form): %w", err)
		}
		for k, v := range raw {
			out[k] = v
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
	if e == nil {
		return nil
	}
	out := composeEnv{}
	switch value.Kind {
	case yaml.MappingNode:
		var raw map[string]string
		if err := value.Decode(&raw); err != nil {
			return fmt.Errorf("decode environment (map form): %w", err)
		}
		for k, v := range raw {
			out[k] = v
		}
	case yaml.SequenceNode:
		var list []string
		if err := value.Decode(&list); err != nil {
			return fmt.Errorf("decode environment (list form): %w", err)
		}
		for _, kv := range list {
			// KEY=value — split on first '='.
			for i := range len(kv) {
				if kv[i] == '=' {
					out[kv[:i]] = kv[i+1:]
					break
				}
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
