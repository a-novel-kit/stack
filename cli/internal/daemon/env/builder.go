package env

import (
	"fmt"
	"sort"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
)

// Builder assembles env blocks for services / targets. It reads compose
// values from the discovery snapshot and resolves references against the
// shared Allocator.
type Builder struct {
	alloc *Allocator
}

// NewBuilder wraps an Allocator. The Allocator must have SetServices
// called before the first ForTarget invocation.
func NewBuilder(alloc *Allocator) *Builder {
	return &Builder{alloc: alloc}
}

// Entry is one resolved env variable.
type Entry struct {
	Key   string
	Value string
}

// ForTarget builds the env block to pass to a spawned process for the
// given target. Allocations are CREATED here — the target is recorded as
// the consumer for every `*_PORT` it references (directly or via
// cross-service), and Allocator.Release(targetID) frees them.
//
// The return shape is []string in "KEY=VALUE" form, ready for
// exec.Cmd.Env. Use BuildResult.ToMap if you want the same content as a
// map (e.g., for JSON output via GetEnv).
func (b *Builder) ForTarget(t *discovery.Target, allServices []string) ([]Entry, error) {
	return b.buildEnv(t, allServices, true /* allocate */, t.ID())
}

// ForServiceUp is the allocate-on-demand variant of ForService — used
// when bringing infra up so the daemon claims `${*_PORT}` slots
// referenced by infra services (e.g., postgres-X's `${POSTGRES_PORT}`).
// Consumer is the service-level synthetic ID ("<stack>/<service>-infra")
// so KillInfra can Release the slots cleanly.
func (b *Builder) ForServiceUp(svc *discovery.Service, allServices []string, consumer string) ([]Entry, error) {
	merged := make(map[string]string)
	for _, t := range svc.Targets {
		entries, err := b.buildEnv(t, allServices, false, "")
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			merged[e.Key] = e.Value
		}
	}
	for _, in := range svc.Infra {
		entries, err := b.buildInfraEnv(in, svc, allServices, true /* allocate */, consumer)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if _, exists := merged[e.Key]; !exists {
				merged[e.Key] = e.Value
			}
		}
	}
	out := make([]Entry, 0, len(merged))
	for k, v := range merged {
		out = append(out, Entry{Key: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// ForService is the read-only variant. Used by the GetEnv RPC so users
// can inspect the env without side-effecting the allocator. Vars whose
// allocations haven't been acquired yet appear with empty string values.
func (b *Builder) ForService(svc *discovery.Service, allServices []string) ([]Entry, error) {
	// We build the env one target at a time and merge — every target in
	// the service shares the same env block (compose's environment is
	// per-service-in-compose, which maps 1:1 to per-target here).
	merged := make(map[string]string)
	for _, t := range svc.Targets {
		entries, err := b.buildEnv(t, allServices, false /* lookup-only */, "")
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			merged[e.Key] = e.Value
		}
	}
	// Also include each infra service's env block (constants and any
	// derived vars they reference). For our local-dev scope those are
	// the postgres credentials we want to see in `run env`.
	for _, in := range svc.Infra {
		entries, err := b.buildInfraEnv(in, svc, allServices, false, "")
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if _, exists := merged[e.Key]; !exists {
				merged[e.Key] = e.Value
			}
		}
	}
	out := make([]Entry, 0, len(merged))
	for k, v := range merged {
		out = append(out, Entry{Key: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// buildEnv is the shared core for ForTarget/ForService. When `allocate`
// is true, Acquire is called for every `*_PORT` reference encountered,
// with `consumer` as the target ID. When false, Lookup is used (empty
// values for unallocated slots).
func (b *Builder) buildEnv(t *discovery.Target, allServices []string, allocate bool, consumer string) ([]Entry, error) {
	owner := t.Service
	// Two-pass: first resolve all referenced vars into the substitution
	// context, then substitute compose values against it.
	//
	// The ports: block of compose is folded in so its ${VAR} references
	// allocate alongside the environment block's. Compose's ports: like
	// "${POSTGRES_PORT}:5432" is the daemon's only signal that
	// POSTGRES_PORT should be allocated — without this fold, services
	// that don't also reference the port in their environment would
	// never see an allocation.
	combined := mergePortRefs(t.Environment, t.Ports)
	ctx, err := b.resolveContext(combined, owner, allServices, allocate, consumer)
	if err != nil {
		return nil, err
	}
	// Substitute every compose value, plus emit any synthesized
	// derivedFor entries we built into ctx.
	out := make(map[string]string, len(ctx)+len(t.Environment))
	for k, v := range ctx {
		out[k] = v
	}
	for k, raw := range t.Environment {
		out[k] = substitute(raw, ctx)
	}
	// Synthetic __port_N keys leak in via mergePortRefs only to
	// participate in reference collection — drop them from the user
	// view.
	for k := range out {
		if strings.HasPrefix(k, "__port_") {
			delete(out, k)
		}
	}
	// Pull in service-level allocations the target's compose doesn't
	// directly reference. The canonical case: a one-shot like
	// `migrations` whose compose only declares POSTGRES_DSN (a literal)
	// — but the SERVICE has POSTGRES_PORT allocated (for postgres-X
	// infra). We expose every allocated `*_PORT` owned by this service
	// + the derived HOST/URL so the synthesis below can build a
	// correct DSN for go-exec mode (localhost:<port> instead of the
	// compose's in-container hostname).
	for _, slot := range b.alloc.Snapshot() {
		if slot.Owner != owner {
			continue
		}
		if _, present := out[slot.LocalVar]; present {
			continue
		}
		for k, v := range derivedFor(slot.LocalVar, slot.Port) {
			if _, exists := out[k]; !exists {
				out[k] = v
			}
		}
	}

	// POSTGRES_DSN synthesis (the one named special case). When this service
	// has an allocated POSTGRES_PORT, the daemon owns POSTGRES_DSN and
	// overwrites any value the compose file declared (which is typically
	// the in-container `postgres-<svc>:5432` form, wrong for go-exec).
	if portStr, ok := out["POSTGRES_PORT"]; ok && portStr != "" {
		user := nonEmptyOr(out["POSTGRES_USER"], "postgres")
		pass := nonEmptyOr(out["POSTGRES_PASSWORD"], "postgres")
		db := nonEmptyOr(out["POSTGRES_DB"], "postgres")
		out["POSTGRES_DSN"] = "postgres://" + user + ":" + pass + "@" + hostLocalhost + ":" + portStr + "/" + db + "?sslmode=disable"
	}

	// Operator un-prefix: this target's OWN service prefix is stripped
	// for its own process env. So service-json-keys-grpc gets
	// both `GRPC_PORT=44447` (from its local view) AND, for symmetry,
	// `SERVICE_JSON_KEYS_GRPC_PORT=44447`.
	ownerPrefix := ServicePrefix(owner) + "_"
	for k, v := range out {
		// If we already have a prefixed view of one of our own ports,
		// expose the un-prefixed form too.
		base, isPrefixed := stripPrefix(k, ownerPrefix)
		if isPrefixed {
			if _, exists := out[base]; !exists {
				out[base] = v
			}
		}
	}
	// And add the prefixed form for our own ports, so cross-service
	// consumers see the same shape (handy when running `a-novel run env`
	// without --all-stacks — you still see the prefixed key). Skip keys
	// that already carry a known service prefix; otherwise re-prefixing
	// produces the double-prefix bug
	// (SERVICE_X_SERVICE_X_SERVICE_Y_VAR).
	for k, v := range out {
		if !isAllocatedKind(k) && !isSynthesizedKind(k) {
			continue
		}
		if owner2, _ := resolveOwner(k, allServices); owner2 != "" {
			continue // already prefixed
		}
		prefixed := ownerPrefix + k
		if _, exists := out[prefixed]; !exists {
			out[prefixed] = v
		}
	}
	entries := make([]Entry, 0, len(out))
	for k, v := range out {
		entries = append(entries, Entry{Key: k, Value: v})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, nil
}

// buildInfraEnv is the same as buildEnv but for an infra service (which
// has no profile and no cmd-target counterpart). It's used by
// ForService so `a-novel run env` shows the database credentials too.
func (b *Builder) buildInfraEnv(in *discovery.Infra, svc *discovery.Service, allServices []string, allocate bool, consumer string) ([]Entry, error) {
	combined := mergePortRefs(in.Environment, in.Ports)
	ctx, err := b.resolveContext(combined, svc.Name, allServices, allocate, consumer)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(in.Environment))
	for k, raw := range in.Environment {
		entries = append(entries, Entry{Key: k, Value: substitute(raw, ctx)})
	}
	// Also include the synthesized derived vars (HOST/URL) for any port
	// allocations resolved during context building.
	for k, v := range ctx {
		if isSynthesizedKind(k) {
			entries = append(entries, Entry{Key: k, Value: v})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, nil
}

// resolveContext walks every ${VAR} reference in `env` and builds the
// substitution context map. Allocations happen here (or lookups, in
// read-only mode). Constants from env that have no references are added
// as-is so they're available for substitutions elsewhere.
func (b *Builder) resolveContext(env map[string]string, owner string, allServices []string, allocate bool, consumer string) (map[string]string, error) {
	ctx := make(map[string]string)
	// First, seed with constants (entries that contain no ${VAR}
	// references). These are the inline-hardcoded values
	// rule 4.
	for k, v := range env {
		if len(extractRefs(v)) == 0 {
			ctx[k] = v
		}
	}
	// Then resolve every referenced VAR.
	for _, v := range env {
		for _, ref := range extractRefs(v) {
			if _, already := ctx[ref]; already {
				continue
			}
			val, err := b.resolveOne(ref, owner, allServices, allocate, consumer, ctx)
			if err != nil {
				return nil, err
			}
			ctx[ref] = val
		}
	}
	return ctx, nil
}

// resolveOne resolves a single VAR name against the running context.
// The classification is:
//
//   - `<prefix>_<localVar>` where <prefix> matches a registered service
//     prefix → cross-service reference; resolve via the allocator with
//     the owner = that service.
//   - `<localVar>` where localVar is a `*_PORT` → allocate against
//     `owner`.
//   - `<localVar>` ending in `_HOST` or `_URL` → derived; synthesize
//     after the matching `*_PORT` is resolved.
//   - everything else → constant referenced from the same env block
//     (already in ctx, or returns empty as compose would).
func (b *Builder) resolveOne(varName, owner string, allServices []string, allocate bool, consumer string, ctx map[string]string) (string, error) {
	resOwner, localVar := resolveOwner(varName, allServices)
	if resOwner == "" {
		resOwner = owner
	}
	// *_PORT → allocate / lookup.
	if isAllocatedKind(localVar) {
		var port int
		var err error
		if allocate {
			port, err = b.alloc.Acquire(resOwner, localVar, consumer)
		} else {
			p, ok := b.alloc.Lookup(resOwner, localVar)
			if !ok {
				return "", nil
			}
			port = p
		}
		if err != nil {
			return "", err
		}
		// Splat the derived (HOST/URL) into ctx so a later substitution
		// of a value like "http://${HOST}:${PORT}/..." resolves cleanly.
		for k, v := range derivedFor(localVar, port) {
			// If the owner is the same as the current target's, the
			// derived vars are local; otherwise re-prefix with the
			// owner's service prefix so the consumer can reach them.
			if resOwner == owner {
				if _, exists := ctx[k]; !exists {
					ctx[k] = v
				}
			} else {
				prefixed := ServicePrefix(resOwner) + "_" + k
				if _, exists := ctx[prefixed]; !exists {
					ctx[prefixed] = v
				}
			}
		}
		return itoa(port), nil
	}
	// *_HOST → synthesized.
	if isHostKind(localVar) {
		return hostLocalhost, nil
	}
	// *_URL → synthesized from the matching _PORT (must have been
	// resolved already, else we can't compose the URL).
	if isURLKind(localVar) {
		base := stripURLSuffix(localVar)
		if portStr, ok := ctx[base+"_PORT"]; ok && portStr != "" {
			// Best-effort URL synthesis from the already-derived port.
			port := atoi(portStr)
			return urlFor(base, port), nil
		}
		return "", nil
	}
	// Anything else: it's expected to be a constant from this env block
	// (already in ctx) OR an externally-injected var (POSTGRES_DSN etc.
	// declared as a value with references — resolved by substitute() in
	// the second pass). If not yet present, leave for the second pass.
	return ctx[varName], nil
}

// stripPrefix returns (suffix, true) if s starts with prefix, else ("", false).
func stripPrefix(s, prefix string) (string, bool) {
	if len(s) > len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return "", false
}

func isSynthesizedKind(k string) bool {
	return isHostKind(k) || isURLKind(k)
}

func isHostKind(k string) bool {
	return len(k) > len("_HOST") && k[len(k)-len("_HOST"):] == "_HOST"
}

func isURLKind(k string) bool {
	return len(k) > len("_URL") && k[len(k)-len("_URL"):] == "_URL"
}
func stripURLSuffix(k string) string { return k[:len(k)-len("_URL")] }

// atoi is the inverse of itoa, for the rare derived-URL recomposition.
func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// nonEmptyOr returns v if non-empty, otherwise fallback. Used to keep
// POSTGRES_DSN well-formed when the compose hasn't (yet) inlined the
// standard credentials, supplying "postgres" for USER/PASSWORD/DB when the
// compose file leaves them unset.
func nonEmptyOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// mergePortRefs returns a copy of env with synthetic entries appended
// for each raw compose ports: mapping. The synthetic keys are unique
// (`__port_N`) and the values are the raw mapping strings — passed
// through resolveContext purely so extractRefs picks up the embedded
// `${VAR}` references. Synthetic keys don't appear in the final
// output; they exist only to participate in the reference-collection
// pass.
func mergePortRefs(env map[string]string, ports []string) map[string]string {
	out := make(map[string]string, len(env)+len(ports))
	for k, v := range env {
		out[k] = v
	}
	for i, p := range ports {
		out[fmt.Sprintf("__port_%d", i)] = p
	}
	return out
}
