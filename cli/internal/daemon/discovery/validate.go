package discovery

import (
	"fmt"
	"regexp"
	"strings"
)

// envRefRe extracts ${VAR} and ${VAR:-default} references from compose env
// values. It duplicates the env package's pattern to keep discovery free of an
// env import.
var envRefRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-[^}]*)?\}`)

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
//   - a key declared as a literal in the same env block, a sibling constant
//
// Anything else warns. Compose still substitutes an empty string at runtime,
// and the warning surfaces the gap.
func ValidateEnvRefs(stacks []*Stack) {
	for _, st := range stacks {
		serviceNames := make([]string, 0, len(st.Services))
		for _, svc := range st.Services {
			serviceNames = append(serviceNames, svc.Name)
		}
		for _, svc := range st.Services {
			st.Errors = append(st.Errors, validateService(svc, serviceNames)...)
		}
	}
}

func validateService(svc *Service, allServices []string) []DiscoveryError {
	var errs []DiscoveryError
	// The keys declared as constants across this service's target and infra env
	// blocks, used to recognize sibling-constant references.
	declared := make(map[string]bool)
	collect := func(env map[string]string) {
		for k := range env {
			declared[k] = true
		}
	}
	for _, t := range svc.Targets {
		collect(t.Environment)
	}
	for _, in := range svc.Infra {
		collect(in.Environment)
	}

	check := func(where, key, value string) {
		for _, ref := range extractRefsLocal(value) {
			if isResolvable(ref, allServices, declared) {
				continue
			}
			errs = append(errs, DiscoveryError{
				Service: svc.Name,
				Path:    svc.ComposePath,
				Reason: fmt.Sprintf("%s declares %s=%q referencing ${%s} — daemon doesn't synthesize this var (set as constant in compose, or use a recognized form: *_PORT / *_HOST / *_URL / POSTGRES_DSN / <SERVICE>_<VAR>)",
					where, key, value, ref),
			})
		}
	}
	for _, t := range svc.Targets {
		for k, v := range t.Environment {
			check("target "+t.Name, k, v)
		}
	}
	for _, in := range svc.Infra {
		for k, v := range in.Environment {
			check("infra "+in.Name, k, v)
		}
	}
	return errs
}

// extractRefsLocal returns the deduplicated ${VAR} names referenced in raw,
// mirroring the env package's own extraction without importing it.
func extractRefsLocal(raw string) []string {
	matches := envRefRe.FindAllStringSubmatch(raw, -1)
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// isResolvable applies the env-resolution rules to decide whether the daemon
// can synthesize the reference.
func isResolvable(varName string, allServices []string, declared map[string]bool) bool {
	if declared[varName] {
		return true // sibling constant in the same service's env block
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
