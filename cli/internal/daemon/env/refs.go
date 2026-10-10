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
// The runner calls Builder.ForTarget at process spawn time, Builder.ForService
// to claim infra ports at infra-up, and Allocator.Release when the process
// terminates. The server's GetEnv RPC calls Builder.ForService for read-only
// inspection.
package env

import (
	"strconv"
	"strings"
)

// hostLocalhost is the hostname synthesized for every *_HOST derivation.
const hostLocalhost = "localhost"

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
// `service-template-extra`, the longer match wins, so services must come
// longest-first, as Allocator.Services returns them.
func resolveOwner(varName string, services []string) (string, string) {
	for _, svc := range services {
		if localVar, ok := strings.CutPrefix(varName, ServicePrefix(svc)+"_"); ok {
			return svc, localVar
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
	return map[string]string{
		localPortVar:   strconv.Itoa(port),
		base + "_HOST": hostLocalhost,
		base + "_URL":  urlFor(base, port),
	}
}

// urlFor renders the URL string for a "<base>_PORT" allocation. gRPC gets the
// schemeless `localhost:port` form that grpc-go clients take as-is; everything
// else gets `http://`.
func urlFor(base string, port int) string {
	hostPort := hostLocalhost + ":" + strconv.Itoa(port)
	if base == "GRPC" {
		return hostPort
	}
	return "http://" + hostPort
}
