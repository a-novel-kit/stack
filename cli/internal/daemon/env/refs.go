// Package env owns the daemon's environment-variable handling: port
// allocation (with refcounting), value synthesis (HOST / URL for allocated
// ports), cross-service propagation, and operator un-prefix.
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
// Allocator.Release when the process terminates. The server's GetEnv RPC
// calls Builder.ForService for read-only inspection.
package env

import (
	"regexp"
	"strings"
)

// hostLocalhost is the hostname synthesized for every *_HOST derivation.
const hostLocalhost = "localhost"

// refRe matches a ${VAR} reference in a compose environment value, in both the
// bare ${VAR} and the ${VAR:-default} form. The default is matched but dropped,
// never applied.
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

// substitute resolves every ${VAR} in raw against ctx. An unknown reference
// resolves to the empty string, matching compose's behavior, so a missing var
// never survives as a literal ${VAR} that breaks at run time.
func substitute(raw string, ctx map[string]string) string {
	return refRe.ReplaceAllStringFunc(raw, func(match string) string {
		// The captured VAR is the first group of "${VAR}" or
		// "${VAR:-default}".
		m := refRe.FindStringSubmatch(match)
		if len(m) < 2 {
			return ""
		}
		return ctx[m[1]]
	})
}

// ServicePrefix is the uppercase, underscore-separated form of a service name
// used in cross-service env references: `service-json-keys` becomes
// `SERVICE_JSON_KEYS`, which other services prepend when consuming its vars, as
// in `${SERVICE_JSON_KEYS_GRPC_PORT}`.
func ServicePrefix(serviceName string) string {
	return strings.ToUpper(strings.ReplaceAll(serviceName, "-", "_"))
}

// resolveOwner classifies a variable name against the registered service names.
// A varName carrying a known service prefix yields (ownerServiceName,
// localVarName); anything else yields ("", varName), which the caller treats as
// local to its own service.
//
// When two service names share a prefix, such as `service-template` and
// `service-template-extra`, the longer match wins, so the caller must pass
// allServices sorted longest-first.
func resolveOwner(varName string, allServices []string) (string, string) {
	for _, svc := range allServices {
		prefix := ServicePrefix(svc) + "_"
		if strings.HasPrefix(varName, prefix) {
			return svc, strings.TrimPrefix(varName, prefix)
		}
	}
	return "", varName
}

// isAllocatedKind reports whether localVar is one the daemon allocates, which
// today means `*_PORT`.
func isAllocatedKind(localVar string) bool {
	return strings.HasSuffix(localVar, "_PORT")
}

// derivedFor produces the synthesized vars that accompany an allocated
// `<X>_PORT`. The returned keys use the local, un-prefixed form; callers
// re-prefix them for cross-service exposure.
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

// urlFor renders the URL string for a "<base>_PORT" allocation. gRPC gets the
// schemeless `localhost:port` form that grpc-go clients take as-is; everything
// else gets `http://`.
func urlFor(base string, port int) string {
	switch base {
	case "GRPC":
		return hostLocalhost + ":" + itoa(port)
	default:
		return "http://" + hostLocalhost + ":" + itoa(port)
	}
}

// itoa formats a non-negative int as a decimal string.
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
