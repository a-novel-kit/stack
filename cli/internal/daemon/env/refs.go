// Package env owns the daemon's environment-variable handling: port
// allocation (with refcounting), value synthesis (HOST / URL for allocated
// ports), cross-service propagation, and operator un-prefix —.
//
// The package exposes two surfaces:
//
//	Allocator — picks free host ports, tracks refcounts, frees on release.
//	Builder   — assembles the full env block for one service, including
//	            constants from compose, allocated values, derived values,
//	            and cross-service references resolved against other
//	            services' allocations.
//
// The runner calls Builder.ForTarget at process spawn time and
// Allocator.ReleaseForTarget when the process terminates. Server's
// GetEnv RPC calls Builder.ForService for read-only inspection.
package env

import (
	"regexp"
	"strings"
)

// hostLocalhost is the canonical synthesized hostname for *_HOST
// derivations. Hoisted so a future "rewrite for remote daemon" can
// flip the value in one place without grepping the package.
const hostLocalhost = "localhost"

// refRe matches a ${VAR} reference in a compose environment value. Two
// variants supported: bare ${VAR} and ${VAR:-default}. Default values
// aren't honored yet — they're rare in our compose files and adding them
// is a one-line follow-up.
var refRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-[^}]*)?\}`)

// extractRefs returns the deduplicated list of ${VAR} names referenced in
// raw — the right-hand side of one compose environment entry.
func extractRefs(raw string) []string {
	matches := refRe.FindAllStringSubmatch(raw, -1)
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

// substitute resolves every ${VAR} in raw against ctx. Unknown references
// resolve to empty string (matching compose's behavior, so a missing var
// surfaces as an empty value instead of a literal ${VAR} that breaks at
// run time).
func substitute(raw string, ctx map[string]string) string {
	return refRe.ReplaceAllStringFunc(raw, func(match string) string {
		// match looks like "${VAR}" or "${VAR:-default}"; the captured
		// VAR is the first match group.
		m := refRe.FindStringSubmatch(match)
		if len(m) < 2 {
			return ""
		}
		return ctx[m[1]]
	})
}

// ServicePrefix is the uppercase, underscore-separated form of a service
// name used in cross-service env references. The convention
// turns `service-json-keys` into `SERVICE_JSON_KEYS`; the resulting
// prefix is what other services prepend when consuming this service's
// vars: `${SERVICE_JSON_KEYS_GRPC_PORT}`.
func ServicePrefix(serviceName string) string {
	return strings.ToUpper(strings.ReplaceAll(serviceName, "-", "_"))
}

// resolveOwner classifies a variable name against the set of registered
// service names. If varName carries a known service prefix, returns
// (ownerServiceName, localVarName); otherwise returns ("", varName) and
// the caller treats it as local to its own service.
//
// Order of evaluation matters: when two service names share a prefix
// (e.g., `service-template` and `service-template-extra`), the longer
// match wins. allServices must be sorted longest-first by the caller.
func resolveOwner(varName string, allServices []string) (string, string) {
	for _, svc := range allServices {
		prefix := ServicePrefix(svc) + "_"
		if strings.HasPrefix(varName, prefix) {
			return svc, strings.TrimPrefix(varName, prefix)
		}
	}
	return "", varName
}

// isAllocatedKind reports whether localVar is one we allocate (currently
// only `*_PORT`). Future polish: pluggable allocators.
func isAllocatedKind(localVar string) bool {
	return strings.HasSuffix(localVar, "_PORT")
}

// derivedFor produces the synthesized vars that always accompany an
// allocated `<X>_PORT`. The keys returned use the local
// (un-prefixed) form; callers re-prefix for cross-service exposure.
func derivedFor(localPortVar string, port int) map[string]string {
	base := strings.TrimSuffix(localPortVar, "_PORT")
	host := hostLocalhost
	url := urlFor(base, port)
	return map[string]string{
		localPortVar:   itoa(port),
		base + "_HOST": host,
		base + "_URL":  url,
	}
}

// urlFor renders the URL string for a "<base>_PORT" allocation. gRPC has
// a deliberately schemeless form (`localhost:port` — `grpc-go` clients
// take it as-is); everything else gets `http://`.
func urlFor(base string, port int) string {
	switch base {
	case "GRPC":
		return hostLocalhost + ":" + itoa(port)
	default:
		return "http://" + hostLocalhost + ":" + itoa(port)
	}
}

// itoa is a thin wrapper to avoid importing strconv just for one call
// site — and to keep this file's surface focused on the substitution
// rules.
func itoa(n int) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	return string(buf[i:])
}
