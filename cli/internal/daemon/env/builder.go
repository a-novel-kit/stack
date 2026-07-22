package env

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/secrets"
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

// ForTarget builds the env block to pass to a spawned process. It creates the
// allocations, recording the target as the consumer of every `*_PORT` it
// references, directly or across services, until Allocator.Release frees them.
//
// It returns the resolved entries plus a value-free warning line for each
// missing secret, which the caller writes to the target's log so the operator
// sees what to set.
func (b *Builder) ForTarget(t *discovery.Target, allServices []string) ([]Entry, []string, error) {
	entries, err := b.buildEnv(t, allServices, true /* allocate */, t.ID())
	if err != nil {
		return nil, nil, err
	}
	// Inject the service repo's decrypted secrets as plain entries, so they ride
	// into the runner's cmd.Env. The value-free .a-novel/secrets.yaml manifest
	// at the service repo root drives them; an absent manifest is a no-op, and
	// an absent key or store reports every declared secret missing. The inspect
	// paths, ForService and ForServiceUp, skip this, so no value ever reaches a
	// log. A declared-but-unset secret becomes a warning.
	var warnings []string
	if root := serviceRoot(t); root != "" {
		res, err := injectSecrets(root)
		if err != nil {
			return nil, nil, err
		}
		for name, value := range res.Env {
			entries = append(entries, Entry{Key: name, Value: value})
		}
		warnings = res.Warnings()
	}
	return entries, warnings, nil
}

// serviceRoot returns the service repo root for a target — the directory that
// may hold .a-novel/secrets.yaml. CmdDir is `.../service-X/cmd/<name>/`, so the
// service dir is its grandparent.
func serviceRoot(t *discovery.Target) string {
	if t.CmdDir == "" {
		return ""
	}
	return filepath.Dir(filepath.Dir(t.CmdDir))
}

// injectSecrets is the seam to the secrets package, indirected through a package
// var so the env-builder tests can stub it without touching the local key store.
var injectSecrets = secrets.InjectForRepo

// ForServiceUp is the allocating variant of ForService, used when bringing
// infra up so the daemon claims the `${*_PORT}` slots infra services reference.
// consumer is the service-level synthetic ID ("<stack>/<service>-infra"), which
// lets KillInfra release those slots cleanly.
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

// ForService is the read-only variant, backing the GetEnv RPC so a user can
// inspect the env without side-effecting the allocator. A var whose allocation
// has not been acquired appears with an empty value.
func (b *Builder) ForService(svc *discovery.Service, allServices []string) ([]Entry, error) {
	// Every target in the service shares the same env block, since compose's
	// environment is per-compose-service and maps one-to-one to a target here.
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
	// Each infra env block contributes its constants and derived vars, which is
	// where `run env` picks up the Postgres credentials.
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

// buildEnv is the shared core of ForTarget and ForService. With allocate set,
// every `*_PORT` reference is acquired for consumer; otherwise it is looked up,
// and an unallocated slot resolves empty.
func (b *Builder) buildEnv(t *discovery.Target, allServices []string, allocate bool, consumer string) ([]Entry, error) {
	owner := t.Service
	// Two passes: resolve every referenced var into the substitution context,
	// then substitute the compose values against it.
	//
	// The compose `ports:` block folds in so its ${VAR} references allocate
	// alongside the environment block's. A mapping like "${POSTGRES_PORT}:5432"
	// is often the daemon's only signal to allocate POSTGRES_PORT, since a
	// service need never name it in its environment.
	combined := mergePortRefs(t.Environment, t.Ports)
	ctx, err := b.resolveContext(combined, owner, allServices, allocate, consumer)
	if err != nil {
		return nil, err
	}
	// Substitute every compose value, and emit the derivedFor entries built
	// into ctx.
	out := make(map[string]string, len(ctx)+len(t.Environment))
	for k, v := range ctx {
		out[k] = v
	}
	for k, raw := range t.Environment {
		out[k] = substitute(raw, ctx)
	}
	// The synthetic __port_N keys exist only for reference collection, so drop
	// them from the user view.
	for k := range out {
		if strings.HasPrefix(k, "__port_") {
			delete(out, k)
		}
	}
	// Pull in service-level allocations the target's compose never references.
	// A one-shot like `migrations` declares POSTGRES_DSN as a literal while the
	// service itself holds the POSTGRES_PORT allocation for its postgres infra.
	// Exposing every `*_PORT` this service owns, with its derived HOST and URL,
	// lets the synthesis below build a DSN pointing at localhost for go-exec
	// mode, in place of the compose file's in-container hostname.
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

	// With an allocated POSTGRES_PORT the daemon owns POSTGRES_DSN and
	// overwrites whatever the compose file declared, that being the
	// in-container `postgres-<svc>:5432` form, wrong for go-exec.
	if portStr, ok := out["POSTGRES_PORT"]; ok && portStr != "" {
		user := nonEmptyOr(out["POSTGRES_USER"], "postgres")
		pass := nonEmptyOr(out["POSTGRES_PASSWORD"], "postgres")
		db := nonEmptyOr(out["POSTGRES_DB"], "postgres")
		out["POSTGRES_DSN"] = "postgres://" + user + ":" + pass + "@" + hostLocalhost + ":" + portStr + "/" + db + "?sslmode=disable"
	}

	// Strip the target's own service prefix for its process env, so
	// service-json-keys-grpc sees both `GRPC_PORT=44447` and, for symmetry,
	// `SERVICE_JSON_KEYS_GRPC_PORT=44447`.
	ownerPrefix := ServicePrefix(owner) + "_"
	for k, v := range out {
		// A prefixed view of one of our own ports also gets the un-prefixed
		// form.
		base, isPrefixed := stripPrefix(k, ownerPrefix)
		if isPrefixed {
			if _, exists := out[base]; !exists {
				out[base] = v
			}
		}
	}
	// Add the prefixed form of our own ports so cross-service consumers see the
	// same shape. A key that already carries a known service prefix is skipped,
	// so every key ends up with exactly one prefix.
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

// buildInfraEnv is buildEnv for an infra service, which has no profile and no
// cmd-target counterpart. ForService uses it so `a-novel run env` shows the
// database credentials too.
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
	// Include the synthesized HOST and URL vars for every port allocation
	// resolved while building the context.
	for k, v := range ctx {
		if isSynthesizedKind(k) {
			entries = append(entries, Entry{Key: k, Value: v})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, nil
}

// resolveContext walks every ${VAR} reference in env and builds the
// substitution context map, allocating along the way or, in read-only mode,
// looking up. A constant carrying no reference is added as-is, so later
// substitutions can resolve against it.
func (b *Builder) resolveContext(env map[string]string, owner string, allServices []string, allocate bool, consumer string) (map[string]string, error) {
	ctx := make(map[string]string)
	// Seed with the constants, the entries holding no ${VAR} reference.
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

// resolveOne resolves a single VAR name against the running context:
//
//   - `<prefix>_<localVar>` whose prefix matches a registered service is a
//     cross-service reference, resolved through the allocator against that
//     service.
//   - a `*_PORT` localVar allocates against `owner`.
//   - a localVar ending in `_HOST` or `_URL` is synthesized once the matching
//     `*_PORT` resolves.
//   - anything else is a constant from the same env block, already in ctx or
//     empty — the value compose gives an unset variable.
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
		// Splat the derived HOST and URL into ctx, so a later substitution of
		// a value like "http://${HOST}:${PORT}/..." resolves cleanly.
		for k, v := range derivedFor(localVar, port) {
			// The derived vars are local when the owner is the current
			// target's; otherwise they carry the owner's service prefix so
			// the consumer can reach them.
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
	// *_URL → synthesized from the matching _PORT, which must already be
	// resolved for the URL to compose.
	if isURLKind(localVar) {
		base := stripURLSuffix(localVar)
		if portStr, ok := ctx[base+"_PORT"]; ok && portStr != "" {
			port := atoi(portStr)
			return urlFor(base, port), nil
		}
		return "", nil
	}
	// Anything else is a constant from this env block, already in ctx, or a
	// value carrying references that substitute() resolves in the second pass.
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

// nonEmptyOr returns v when it is non-empty, otherwise fallback. It keeps
// POSTGRES_DSN well-formed when the compose file leaves the standard
// credentials unset.
func nonEmptyOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// mergePortRefs returns a copy of env with one synthetic `__port_N` entry per
// raw compose ports: mapping. The synthetic keys carry those mapping strings
// through resolveContext so extractRefs picks up their embedded `${VAR}`
// references; they never reach the final output.
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
