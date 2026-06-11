package discovery

import (
	"fmt"
	"regexp"
	"strings"
)

// envRefRe extracts ${VAR} (and ${VAR:-default}) references from
// compose env values. Mirrors env package's refRe — duplicated here to
// keep discovery free of an env-package import.
var envRefRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-[^}]*)?\}`)

// ValidateEnvRefs scans every compose env block in `disc` for ${VAR}
// references the daemon doesn't know how to fill, and appends a
// non-fatal warning to the stack's Errors list. Called at end of
// discovery so every parse error and validation warning land in one
// stream the daemon prints at startup.
//
// What's "resolvable":
//   - `*_PORT`            → allocated by the daemon
//   - `*_HOST`, `*_URL`   → synthesized from `*_PORT` (same service)
//   - `POSTGRES_DSN`      → synthesized when POSTGRES_PORT allocated
//   - `<SERVICE>_<VAR>`   → cross-service reference, if <SERVICE>
//     is a known service in this stack
//   - Any key declared as a literal (no `${...}` references) in the
//     SAME env block — that's a sibling constant
//
// Anything else gets a warning: "compose service X references ${VAR}
// but the daemon can't fill it". The compose will still substitute
// with an empty string at runtime; this warning surfaces the gap.
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
	// Build the set of keys declared as constants in this service's
	// env blocks (across all targets + infra). Used to recognize
	// sibling-constant references.
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

// extractRefsLocal returns the deduplicated list of ${VAR} names
// referenced in raw. Mirrors env.extractRefs without the dependency.
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
	// Cross-service form: <SERVICE_UPPER>_<rest>. Match longest-first.
	for _, svc := range allServices {
		prefix := strings.ToUpper(strings.ReplaceAll(svc, "-", "_")) + "_"
		if strings.HasPrefix(varName, prefix) {
			return true
		}
	}
	return false
}
