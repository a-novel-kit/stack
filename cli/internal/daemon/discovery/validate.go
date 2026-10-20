package discovery

import (
	"fmt"
	"iter"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/shared/compose"
)

// ValidateEnvRefs scans every compose env block for ${VAR} references the
// daemon cannot fill and appends a non-fatal warning to the owning stack's
// Errors. It runs at the end of discovery, so parse errors and validation
// warnings land in the one stream the daemon prints at startup.
//
// A reference resolves when it is:
//   - `*_PORT`, allocated by the daemon
//   - `*_HOST` or `*_URL`, synthesized from the same service's `*_PORT`
//   - `POSTGRES_DSN`, synthesized once POSTGRES_PORT is allocated
//   - `<SERVICE>_<VAR>`, naming a known service in this stack
//   - a key one of the service's env blocks declares, a sibling constant
//
// Anything else warns. Compose still substitutes an empty string at runtime,
// and the warning surfaces the gap.
func ValidateEnvRefs(stacks Stacks) {
	for _, st := range stacks {
		serviceNames := Stacks{st}.ServiceNames()
		for _, svc := range st.Services {
			st.Errors = append(st.Errors, validateService(svc, serviceNames)...)
		}
	}
}

// validateService warns about every reference of svc's env blocks the daemon
// cannot fill.
func validateService(svc *Service, allServices []string) []DiscoveryError {
	// Any block's keys are constants a sibling reference may name.
	declared := make(map[string]bool)
	for _, env := range envBlocks(svc) {
		for key := range env {
			declared[key] = true
		}
	}
	var errs []DiscoveryError
	for where, env := range envBlocks(svc) {
		errs = append(errs, unresolvedRefs(svc, where, env, allServices, declared)...)
	}
	return errs
}

// envBlocks yields each env block of svc, targets first, with the target or
// infra that declares it.
func envBlocks(svc *Service) iter.Seq2[string, map[string]string] {
	return func(yield func(string, map[string]string) bool) {
		for _, t := range svc.Targets {
			if !yield("target "+t.Name, t.Environment) {
				return
			}
		}
		for _, in := range svc.Infra {
			if !yield("infra "+in.Name, in.Environment) {
				return
			}
		}
	}
}

// unresolvedRefs warns about each reference in env, the env block of where,
// that the daemon cannot fill.
func unresolvedRefs(
	svc *Service, where string, env map[string]string, allServices []string, declared map[string]bool,
) []DiscoveryError {
	var errs []DiscoveryError
	for key, value := range env {
		for _, ref := range compose.Refs(value) {
			if isResolvable(ref, allServices, declared) {
				continue
			}
			reason := fmt.Sprintf("%s declares %s=%q referencing ${%s} — daemon doesn't synthesize this var "+
				"(set as constant in compose, or use a recognized form: *_PORT / *_HOST / *_URL / POSTGRES_DSN / <SERVICE>_<VAR>)",
				where, key, value, ref)
			errs = append(errs, DiscoveryError{Service: svc.Name, Path: svc.ComposePath, Reason: reason})
		}
	}
	return errs
}

// isResolvable applies the env-resolution rules to decide whether the daemon
// can synthesize the reference.
func isResolvable(varName string, allServices []string, declared map[string]bool) bool {
	if declared[varName] {
		return true // a constant of one of the service's env blocks
	}
	if strings.HasSuffix(varName, "_PORT") ||
		strings.HasSuffix(varName, "_HOST") ||
		strings.HasSuffix(varName, "_URL") ||
		varName == "POSTGRES_DSN" {
		return true
	}
	// Cross-service form: <SERVICE_UPPER>_<rest>.
	for _, svc := range allServices {
		prefix := strings.ToUpper(strings.ReplaceAll(svc, "-", "_")) + "_"
		if strings.HasPrefix(varName, prefix) {
			return true
		}
	}
	return false
}
