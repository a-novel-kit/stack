package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

// Stack is the discovered shape of one registered git checkout. It owns the
// services found under <path>/app/service-*.
type Stack struct {
	Name     string
	Path     string
	Default  bool
	Services []*Service
	// Errors holds this stack's non-fatal discovery problems, surfaced through
	// Status and the startup logs so the daemon still starts. A stack path that
	// does not exist is returned as the function's error instead.
	Errors []DiscoveryError
}

// Service is one app/service-* directory, with everything we discovered
// about its compose file.
type Service struct {
	Name        string // e.g., "service-json-keys"
	Stack       string // owning stack name
	Path        string // absolute path to the service directory
	ComposePath string // absolute path to its compose file
	Targets     []*Target
	Infra       []*Infra
	Volumes     []*Volume
	Networks    []string // network names declared at compose top-level
	// Dependency edges, one per compose `depends_on` entry, connecting two
	// compose-service names. Resolving them to a concrete target or infra
	// happens later, at the daemon level.
	DependsOn map[string][]string // compose-service-name → [list of names it depends on]
}

// Target is a discovered Go cmd/<name>/ entry with its compose mirror.
type Target struct {
	Name        string     // e.g., "rest" (cmd directory name)
	ComposeName string     // e.g., "service-json-keys-rest" (compose service name)
	Service     string     // owning service name
	Stack       string     // owning stack name
	Profile     string     // compose profile name (same as Name by convention)
	Kind        TargetKind // OneShot | LongRunner
	CmdDir      string     // absolute path to cmd/<name>/
	Dockerfile  string     // absolute path to the build's Dockerfile (may be "")
	DependsOn   []string   // compose-service-names this target depends_on
	Ports       []string   // raw "${VAR}:N" port mappings, resolved later
	Environment map[string]string
}

// ID returns the canonical "<stack>/<service>/<target>" identifier the daemon
// uses to address this target across every API. Every layer derives it the same
// way, so the identifier stays stable end to end.
func (t *Target) ID() string {
	return t.Stack + "/" + t.Service + "/" + t.Name
}

// Infra is a compose service with no profile assignment and no matching
// cmd/<name>/ directory.
type Infra struct {
	Name           string // compose service name, e.g., "postgres-json-keys"
	Service        string // owning service name
	Stack          string // owning stack name
	Dockerfile     string // referenced Dockerfile (for build)
	HasHealthcheck bool
	DependsOn      []string
	Ports          []string
	Environment    map[string]string
	Volumes        []string // volume mounts the infra declares
}

// Volume is a top-level compose `volumes:` entry, scoped to its service.
type Volume struct {
	Name    string // bare name, e.g., "json-keys-postgres-data"
	Service string
	Stack   string
}

// TargetKind classifies a target as one-shot or long-runner.
type TargetKind int

const (
	TargetKindUnknown    TargetKind = iota
	TargetKindOneShot               // no healthcheck in compose or the Dockerfile
	TargetKindLongRunner            // healthcheck in compose or the Dockerfile
)

// String renders the kind in the shape the proto enum uses.
func (k TargetKind) String() string {
	switch k {
	case TargetKindOneShot:
		return "one-shot"
	case TargetKindLongRunner:
		return "long-runner"
	default:
		return "unknown"
	}
}

// A DiscoveryError is a non-fatal classification problem, such as a compose
// service with no matching cmd directory. They are collected per stack and
// surfaced through Status, so the daemon still starts. An unreadable stack path
// bubbles up as the function's error instead.
type DiscoveryError struct {
	Service string // service name within the stack
	Path    string // file or directory path the error points at
	Reason  string // human-readable explanation
}

func (e DiscoveryError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Service, e.Reason, e.Path)
	}
	return fmt.Sprintf("%s: %s", e.Service, e.Reason)
}

// =============================================================================
// Discovery
// =============================================================================

// DiscoverStacks parses every service in every registered stack, returning one
// Stack per input with its services populated and its problems collected in
// Errors. An inaccessible stack path returns an error and no Stack entry, while
// a stack with malformed services still comes back with them listed in Errors.
func DiscoverStacks(stk []stacks.Stack) ([]*Stack, error) {
	out := make([]*Stack, 0, len(stk))
	for _, s := range stk {
		info, err := os.Stat(s.Path)
		if err != nil {
			// A scratch stack lives in a directory the OS reclaims, so its
			// files can vanish while the $A_NOVEL_STACKS entry lives on in a
			// shell config. A vanished scratch stack is skipped, and only the
			// default stack is fatal.
			if !s.IsDefault {
				continue
			}
			return nil, fmt.Errorf("stack %s at %s: %w", s.Name, s.Path, err)
		}
		if !info.IsDir() {
			if !s.IsDefault {
				continue
			}
			return nil, fmt.Errorf("stack %s: %s is not a directory", s.Name, s.Path)
		}
		st := &Stack{Name: s.Name, Path: s.Path, Default: s.IsDefault}
		discoverStack(st)
		out = append(out, st)
	}
	// Validate compose env-var references, appending non-fatal warnings to each
	// stack's Errors so the startup log surfaces every unresolvable `${VAR}`.
	ValidateEnvRefs(out)
	return out, nil
}

// discoverStack walks <stack>/app/service-*/ and adds Service entries to st.
// Per-service errors are accumulated in st.Errors; the stack itself is
// returned even when its services are problematic, so the user can still
// inspect partial state.
func discoverStack(st *Stack) {
	appDir := filepath.Join(st.Path, "app")
	entries, err := os.ReadDir(appDir)
	if err != nil {
		// No app/ directory means no services. A freshly cloned stack looks
		// like this, so record it without failing.
		st.Errors = append(st.Errors, DiscoveryError{
			Path:   appDir,
			Reason: "no app/ directory under stack root",
		})
		return
	}
	// Skip the *-template scaffolds: they are design-time references with
	// nothing runnable behind them, and listing them in `ps` or the TUI sidebar
	// invites an accidental start. The sort keeps the output stable.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "service-") {
			continue
		}
		if strings.HasSuffix(e.Name(), "-template") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		svc, errs := discoverService(st.Name, filepath.Join(appDir, n))
		st.Errors = append(st.Errors, errs...)
		if svc != nil {
			st.Services = append(st.Services, svc)
		}
	}
}

// discoverService parses one app/service-<name>/ directory: walks cmd/ for
// target names, parses builds/podman-compose.yaml, cross-checks the two,
// classifies targets by healthcheck presence (compose first, Dockerfile
// fallback), and returns the populated Service.
func discoverService(stack, dir string) (*Service, []DiscoveryError) {
	name := filepath.Base(dir)
	composePath := filepath.Join(dir, "builds", "podman-compose.yaml")
	cf, err := parseComposeFile(composePath)
	if err != nil {
		// A missing or malformed compose file leaves nothing to classify, so
		// the service drops out of the list with an error explaining why.
		return nil, []DiscoveryError{{Service: name, Path: composePath, Reason: err.Error()}}
	}
	svc := &Service{
		Name:        name,
		Stack:       stack,
		Path:        dir,
		ComposePath: composePath,
		DependsOn:   make(map[string][]string),
	}

	// Walk cmd/ to find the Go targets the service exposes.
	cmdEntries, _ := os.ReadDir(filepath.Join(dir, "cmd"))
	cmdNames := make(map[string]string) // name → absolute path to cmd/<name>/
	for _, e := range cmdEntries {
		if !e.IsDir() {
			continue
		}
		// Require cmd/<name>/main.go, so a stray empty directory never counts
		// as a target.
		mainPath := filepath.Join(dir, "cmd", e.Name(), "main.go")
		if _, err := os.Stat(mainPath); err == nil {
			cmdNames[e.Name()] = filepath.Join(dir, "cmd", e.Name())
		}
	}

	var errs []DiscoveryError

	// Classify each compose service.
	for csName, cs := range cf.Services {
		// Resolve the dockerfile path for healthcheck inspection.
		dockerfileAbs := ""
		if cs.Build != nil && cs.Build.Dockerfile != "" {
			// Compose resolves `context:` against the compose file's directory,
			// and the Dockerfile field against that context.
			ctxDir := filepath.Dir(composePath)
			if cs.Build.Context != "" {
				ctxDir = filepath.Join(ctxDir, cs.Build.Context)
			}
			dockerfileAbs = filepath.Join(ctxDir, cs.Build.Dockerfile)
			dockerfileAbs = filepath.Clean(dockerfileAbs)
		}

		// Collect dependency list.
		var depList []string
		for depName := range cs.DependsOn {
			depList = append(depList, depName)
		}
		sort.Strings(depList)
		svc.DependsOn[csName] = depList

		// Profile-tagged → target candidate.
		if len(cs.Profiles) > 0 {
			// The first profile is canonical: by convention a target
			// declares a single profile whose name matches its cmd dir.
			profile := cs.Profiles[0]
			cmdDir, hasCmd := cmdNames[profile]
			if !hasCmd {
				errs = append(errs, DiscoveryError{
					Service: name,
					Path:    composePath,
					Reason:  fmt.Sprintf("compose service %q has profiles:[%q] but no matching cmd/%s/", csName, profile, profile),
				})
				continue
			}
			// Classify by healthcheck presence (compose first, Dockerfile fallback).
			kind := TargetKindOneShot
			if cs.Healthcheck != nil {
				kind = TargetKindLongRunner
			} else if dockerfileAbs != "" && dockerfileHasHealthcheck(dockerfileAbs) {
				kind = TargetKindLongRunner
			}
			svc.Targets = append(svc.Targets, &Target{
				Name:        profile,
				ComposeName: csName,
				Service:     name,
				Stack:       stack,
				Profile:     profile,
				Kind:        kind,
				CmdDir:      cmdDir,
				Dockerfile:  dockerfileAbs,
				DependsOn:   depList,
				Ports:       cs.Ports,
				Environment: map[string]string(cs.Environment),
			})
			// Mark this cmd as matched, leaving only orphans behind for the
			// check below.
			delete(cmdNames, profile)
			continue
		}

		// No profile means infrastructure. A profile-less compose service with
		// a matching cmd/<csName>/ is an unprofiled target, which is an error;
		// infra names like "postgres-json-keys" have no cmd dir and pass
		// naturally.
		if _, hasCmd := cmdNames[csName]; hasCmd {
			errs = append(errs, DiscoveryError{
				Service: name,
				Path:    composePath,
				Reason:  fmt.Sprintf("compose service %q has no profile but a matching cmd/%s/ exists — declare profiles:[%q] to make it a target, or rename the cmd dir", csName, csName, csName),
			})
			delete(cmdNames, csName)
			continue
		}
		hasHC := cs.Healthcheck != nil
		if !hasHC && dockerfileAbs != "" {
			hasHC = dockerfileHasHealthcheck(dockerfileAbs)
		}
		svc.Infra = append(svc.Infra, &Infra{
			Name:           csName,
			Service:        name,
			Stack:          stack,
			Dockerfile:     dockerfileAbs,
			HasHealthcheck: hasHC,
			DependsOn:      depList,
			Ports:          cs.Ports,
			Environment:    map[string]string(cs.Environment),
			Volumes:        cs.Volumes,
		})
	}

	// A cmd/ entry left unmatched is an orphan: without a compose mirror it
	// cannot run.
	for unmatchedCmd := range cmdNames {
		errs = append(errs, DiscoveryError{
			Service: name,
			Path:    filepath.Join(dir, "cmd", unmatchedCmd),
			Reason:  fmt.Sprintf("cmd/%s/ has no matching compose service with profiles:[%q]", unmatchedCmd, unmatchedCmd),
		})
	}

	// Volumes and networks declared at top-level.
	for vName := range cf.Volumes {
		svc.Volumes = append(svc.Volumes, &Volume{
			Name:    vName,
			Service: name,
			Stack:   stack,
		})
	}
	for nName := range cf.Networks {
		svc.Networks = append(svc.Networks, nName)
	}

	// Stable sort for deterministic output.
	sort.Slice(svc.Targets, func(i, j int) bool { return svc.Targets[i].Name < svc.Targets[j].Name })
	sort.Slice(svc.Infra, func(i, j int) bool { return svc.Infra[i].Name < svc.Infra[j].Name })
	sort.Slice(svc.Volumes, func(i, j int) bool { return svc.Volumes[i].Name < svc.Volumes[j].Name })
	sort.Strings(svc.Networks)

	return svc, errs
}
