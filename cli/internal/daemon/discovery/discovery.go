package discovery

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/shared/compose"
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
	ComposePath string // absolute path to its compose file
	Targets     []*Target
	Infra       []*Infra
	Volumes     []*Volume
}

// Target is a discovered Go cmd/<name>/ entry with its compose mirror. Name is
// also the compose profile that gates the mirror.
type Target struct {
	Name        string     // e.g., "rest" (cmd directory name)
	ComposeName string     // e.g., "service-json-keys-rest" (compose service name)
	Service     string     // owning service name
	Stack       string     // owning stack name
	Kind        TargetKind // OneShot | LongRunner
	CmdDir      string     // absolute path to cmd/<name>/
	DependsOn   []string   // compose-service-names this target depends_on
	Ports       []string   // raw "${VAR}:N" port mappings, resolved later
	Environment map[string]string
}

// ID returns the canonical "<stack>/<service>/<target>" identifier the daemon
// uses to address this target across every API.
func (t *Target) ID() string {
	return t.Stack + "/" + t.Service + "/" + t.Name
}

// ServiceDir returns the service repo root the target runs from, which holds
// its go.mod: the grandparent of cmd/<name>/.
func (t *Target) ServiceDir() string {
	return filepath.Dir(filepath.Dir(t.CmdDir))
}

// Infra is a compose service with no profile assignment and no matching
// cmd/<name>/ directory.
type Infra struct {
	Name        string // compose service name, e.g., "postgres-json-keys"
	Service     string // owning service name
	Stack       string // owning stack name
	DependsOn   []string
	Ports       []string
	Environment map[string]string
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
// Lookups
// =============================================================================

// Stacks is the discovery snapshot of every registered stack, in registration
// order, so the default stack comes first. Its methods are the one way the
// daemon resolves names to discovered entities.
type Stacks []*Stack

// Stack returns the stack named name, or the default stack when name is empty.
// It returns nil when no stack matches.
func (x Stacks) Stack(name string) *Stack {
	for _, st := range x {
		if st.Name == name || name == "" && st.Default {
			return st
		}
	}
	return nil
}

// Service returns the named service of the named stack, an empty stack name
// meaning the default one. It returns nil when either name is unknown.
func (x Stacks) Service(stack, name string) *Service {
	st := x.Stack(stack)
	if st == nil {
		return nil
	}
	return find(st.Services, func(svc *Service) bool { return svc.Name == name })
}

// Target returns the target whose ID is id, with its owning service. Both are
// nil when no discovered target carries that ID.
func (x Stacks) Target(id string) (*Target, *Service) {
	for _, st := range x {
		for _, svc := range st.Services {
			if t := find(svc.Targets, func(t *Target) bool { return t.ID() == id }); t != nil {
				return t, svc
			}
		}
	}
	return nil, nil
}

// ServiceNames returns the name of every service across the stacks.
func (x Stacks) ServiceNames() []string {
	var names []string
	for _, st := range x {
		for _, svc := range st.Services {
			names = append(names, svc.Name)
		}
	}
	return names
}

// FindInfra returns the infra the service declares under the compose service
// name, or nil.
func (s *Service) FindInfra(name string) *Infra {
	return find(s.Infra, func(in *Infra) bool { return in.Name == name })
}

// FindTargetByComposeName returns the target whose compose mirror is named
// composeName, the form depends_on entries use, or nil.
func (s *Service) FindTargetByComposeName(composeName string) *Target {
	return find(s.Targets, func(t *Target) bool { return t.ComposeName == composeName })
}

// find returns the first element of list that satisfies match, or nil.
func find[T any](list []*T, match func(*T) bool) *T {
	if i := slices.IndexFunc(list, match); i >= 0 {
		return list[i]
	}
	return nil
}

// =============================================================================
// Discovery
// =============================================================================

// DiscoverStacks parses every service in every registered stack, returning one
// Stack per input with its services populated and its problems collected in
// Errors. An inaccessible stack path returns an error and no Stack entry, while
// a stack with malformed services still comes back with them listed in Errors.
func DiscoverStacks(stk []stacks.Stack) (Stacks, error) {
	out := make(Stacks, 0, len(stk))
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
	// invites an accidental start. os.ReadDir sorts by name, which keeps the
	// output stable.
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "service-") || strings.HasSuffix(e.Name(), "-template") {
			continue
		}
		svc, errs := discoverService(st.Name, filepath.Join(appDir, e.Name()))
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
	cf, err := compose.ReadFile(composePath)
	if err != nil {
		// A missing or malformed compose file leaves nothing to classify, so
		// the service drops out of the list with an error explaining why.
		return nil, []DiscoveryError{{Service: name, Path: composePath, Reason: err.Error()}}
	}
	svc := &Service{Name: name, Stack: stack, ComposePath: composePath}

	// Walk cmd/ to find the Go targets the service exposes.
	cmdEntries, _ := os.ReadDir(filepath.Join(dir, "cmd"))
	cmdNames := make(map[string]string) // name → absolute path to cmd/<name>/
	for _, e := range cmdEntries {
		if !e.IsDir() {
			continue
		}
		// Require cmd/<name>/main.go, so a stray empty directory never counts
		// as a target.
		cmdDir := filepath.Join(dir, "cmd", e.Name())
		if _, err := os.Stat(filepath.Join(cmdDir, "main.go")); err == nil {
			cmdNames[e.Name()] = cmdDir
		}
	}

	var errs []DiscoveryError

	// Classify each compose service.
	for csName, cs := range cf.Services {
		deps := slices.Sorted(maps.Keys(cs.DependsOn))

		// No profile means infrastructure. A profile-less compose service with
		// a matching cmd/<csName>/ is an unprofiled target, which is an error;
		// infra names like "postgres-json-keys" have no cmd dir and pass
		// naturally.
		if len(cs.Profiles) == 0 {
			if _, hasCmd := cmdNames[csName]; hasCmd {
				errs = append(errs, DiscoveryError{
					Service: name,
					Path:    composePath,
					Reason:  fmt.Sprintf("compose service %q has no profile but a matching cmd/%s/ exists — declare profiles:[%q] to make it a target, or rename the cmd dir", csName, csName, csName),
				})
				delete(cmdNames, csName)
				continue
			}
			svc.Infra = append(svc.Infra, &Infra{
				Name:        csName,
				Service:     name,
				Stack:       stack,
				DependsOn:   deps,
				Ports:       cs.Ports,
				Environment: cs.Environment,
			})
			continue
		}

		// The first profile is canonical: by convention a target declares a
		// single profile whose name matches its cmd dir.
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
		// Classify by healthcheck presence, compose first. Compose resolves
		// the build context against the compose file's directory, and the
		// Dockerfile against that context.
		kind := TargetKindOneShot
		if cs.Healthcheck != nil || cs.Build != nil && cs.Build.Dockerfile != "" &&
			dockerfileHasHealthcheck(filepath.Join(filepath.Dir(composePath), cs.Build.Context, cs.Build.Dockerfile)) {
			kind = TargetKindLongRunner
		}
		svc.Targets = append(svc.Targets, &Target{
			Name:        profile,
			ComposeName: csName,
			Service:     name,
			Stack:       stack,
			Kind:        kind,
			CmdDir:      cmdDir,
			DependsOn:   deps,
			Ports:       cs.Ports,
			Environment: cs.Environment,
		})
		// Mark this cmd as matched, leaving only orphans behind for the check
		// below.
		delete(cmdNames, profile)
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

	for _, vName := range slices.Sorted(maps.Keys(cf.Volumes)) {
		svc.Volumes = append(svc.Volumes, &Volume{Name: vName, Service: name, Stack: stack})
	}

	// Stable sort for deterministic output.
	slices.SortFunc(svc.Targets, func(a, b *Target) int { return cmp.Compare(a.Name, b.Name) })
	slices.SortFunc(svc.Infra, func(a, b *Infra) int { return cmp.Compare(a.Name, b.Name) })

	return svc, errs
}
