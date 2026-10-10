package env

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/secrets"
	"github.com/a-novel-kit/stack/cli/internal/shared/compose"
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

// Environ returns the daemon's own environment with entries layered over it, in
// the KEY=VALUE form exec.Cmd.Env takes. A spawned process inherits PATH, HOME,
// and GOPATH this way, and an entry wins over an inherited key.
func Environ(entries []Entry) []string {
	out := os.Environ()
	for _, e := range entries {
		out = append(out, e.Key+"="+e.Value)
	}
	return out
}

// ForTarget builds the env block to pass to a spawned process. It creates the
// allocations, recording the target as the consumer of every `*_PORT` it
// references, directly or across services, until Allocator.Release frees them.
//
// It returns the resolved entries plus a value-free warning line for each
// missing secret, which the caller writes to the target's log so the operator
// sees what to set.
func (b *Builder) ForTarget(t *discovery.Target) ([]Entry, []string, error) {
	vars, err := b.buildEnv(t, b.alloc.Services(), true /* allocate */, t.ID())
	if err != nil {
		return nil, nil, err
	}
	// Inject the service repo's decrypted secrets, so they ride into the
	// runner's cmd.Env. The value-free .a-novel/secrets.yaml manifest at the
	// service repo root drives them; an absent manifest is a no-op, and an
	// absent key or store reports every declared secret missing. ForService
	// skips this, so no value ever reaches a log. A declared-but-unset secret
	// becomes a warning.
	var warnings []string
	if t.CmdDir != "" {
		res, err := injectSecrets(t.ServiceDir())
		if err != nil {
			return nil, nil, err
		}
		maps.Copy(vars, res.Env)
		warnings = res.Warnings()
	}
	return entriesOf(vars), warnings, nil
}

// injectSecrets is the seam to the secrets package, indirected through a package
// var so the env-builder tests can stub it without touching the local key store.
var injectSecrets = secrets.InjectForRepo

// ForService builds the env block of every target and infra in svc, which
// GetEnv shows and infra-up hands to compose. Target ports are only looked up.
// With a consumer, the `${*_PORT}` slots the infra references are acquired for
// it, so compose's substitution sees real port numbers and KillInfra can release
// them. With an empty consumer the allocator stays untouched, and a var whose
// port is unallocated appears with an empty value.
func (b *Builder) ForService(svc *discovery.Service, consumer string) ([]Entry, error) {
	services := b.alloc.Services()
	// Every target in the service shares the same env block, since compose's
	// environment is per-compose-service and maps one-to-one to a target here.
	merged := make(map[string]string)
	for _, t := range svc.Targets {
		vars, err := b.buildEnv(t, services, false /* lookup-only */, "")
		if err != nil {
			return nil, err
		}
		maps.Copy(merged, vars)
	}
	// Each infra env block contributes its constants and derived vars, which is
	// where `run env` picks up the Postgres credentials.
	for _, in := range svc.Infra {
		vars, err := b.buildInfraEnv(in, services, consumer != "", consumer)
		if err != nil {
			return nil, err
		}
		for k, v := range vars {
			if _, exists := merged[k]; !exists {
				merged[k] = v
			}
		}
	}
	return entriesOf(merged), nil
}

// buildEnv is the shared core of ForTarget and ForService. With allocate set,
// every `*_PORT` reference is acquired for consumer; otherwise it is looked up,
// and an unallocated slot resolves empty. services is the allocator's
// longest-first service list.
func (b *Builder) buildEnv(t *discovery.Target, services []string, allocate bool, consumer string) (map[string]string, error) {
	owner := t.Service
	// Two passes: resolve every referenced var into the substitution context,
	// then substitute the compose values against it.
	//
	// The compose `ports:` block folds in so its ${VAR} references allocate
	// alongside the environment block's. A mapping like "${POSTGRES_PORT}:5432"
	// is often the daemon's only signal to allocate POSTGRES_PORT, since a
	// service need never name it in its environment.
	ctx, err := b.resolveContext(mergePortRefs(t.Environment, t.Ports), owner, services, allocate, consumer)
	if err != nil {
		return nil, err
	}
	// Substitute every compose value, and emit the derivedFor entries built
	// into ctx.
	out := maps.Clone(ctx)
	for k, raw := range t.Environment {
		out[k] = compose.Substitute(raw, ctx)
	}
	// The synthetic __port_N keys exist only for reference collection, so drop
	// them from the user view.
	maps.DeleteFunc(out, func(k, _ string) bool { return strings.HasPrefix(k, "__port_") })
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
	// in-container `postgres-<svc>:5432` form, wrong for go-exec. Unset
	// credentials fall back to postgres, so the DSN stays well-formed.
	if portStr := out["POSTGRES_PORT"]; portStr != "" {
		user := cmp.Or(out["POSTGRES_USER"], "postgres")
		pass := cmp.Or(out["POSTGRES_PASSWORD"], "postgres")
		db := cmp.Or(out["POSTGRES_DB"], "postgres")
		out["POSTGRES_DSN"] = "postgres://" + user + ":" + pass + "@" + hostLocalhost + ":" + portStr + "/" + db + "?sslmode=disable"
	}

	// Strip the target's own service prefix for its process env, so
	// service-json-keys-grpc sees both `GRPC_PORT=44447` and, for symmetry,
	// `SERVICE_JSON_KEYS_GRPC_PORT=44447`.
	ownerPrefix := ServicePrefix(owner) + "_"
	for k, v := range out {
		// A prefixed view of one of our own ports also gets the un-prefixed
		// form.
		if base, isPrefixed := strings.CutPrefix(k, ownerPrefix); isPrefixed && base != "" {
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
		if keyOwner, _ := resolveOwner(k, services); keyOwner != "" {
			continue // already prefixed
		}
		prefixed := ownerPrefix + k
		if _, exists := out[prefixed]; !exists {
			out[prefixed] = v
		}
	}
	return out, nil
}

// buildInfraEnv is buildEnv for an infra service, which has no profile and no
// cmd-target counterpart. ForService uses it so `a-novel run env` shows the
// database credentials too.
func (b *Builder) buildInfraEnv(in *discovery.Infra, services []string, allocate bool, consumer string) (map[string]string, error) {
	ctx, err := b.resolveContext(mergePortRefs(in.Environment, in.Ports), in.Service, services, allocate, consumer)
	if err != nil {
		return nil, err
	}
	// Include the synthesized HOST and URL vars for every port allocation
	// resolved while building the context, under the substituted compose values.
	out := make(map[string]string, len(in.Environment))
	for k, v := range ctx {
		if isSynthesizedKind(k) {
			out[k] = v
		}
	}
	for k, raw := range in.Environment {
		out[k] = compose.Substitute(raw, ctx)
	}
	return out, nil
}

// resolveContext walks every ${VAR} reference in env and builds the
// substitution context map, allocating along the way or, in read-only mode,
// looking up. A constant carrying no reference is added as-is, so later
// substitutions can resolve against it.
func (b *Builder) resolveContext(env map[string]string, owner string, services []string, allocate bool, consumer string) (map[string]string, error) {
	ctx := make(map[string]string)
	// Seed with the constants, the entries holding no ${VAR} reference.
	for k, v := range env {
		if len(compose.Refs(v)) == 0 {
			ctx[k] = v
		}
	}
	// Then resolve every referenced VAR.
	for _, v := range env {
		for _, ref := range compose.Refs(v) {
			if _, already := ctx[ref]; already {
				continue
			}
			val, err := b.resolveOne(ref, owner, services, allocate, consumer, ctx)
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
func (b *Builder) resolveOne(varName, owner string, services []string, allocate bool, consumer string, ctx map[string]string) (string, error) {
	resOwner, localVar := resolveOwner(varName, services)
	if resOwner == "" {
		resOwner = owner
	}
	switch {
	case isAllocatedKind(localVar):
		var port int
		if allocate {
			p, err := b.alloc.Acquire(resOwner, localVar, consumer)
			if err != nil {
				return "", err
			}
			port = p
		} else if p, ok := b.alloc.Lookup(resOwner, localVar); ok {
			port = p
		} else {
			return "", nil
		}
		// Splat the derived HOST and URL into ctx, so a later substitution of
		// a value like "http://${HOST}:${PORT}/..." resolves cleanly. The
		// derived vars are local when the owner is the current target's;
		// otherwise they carry the owner's service prefix so the consumer can
		// reach them.
		prefix := ""
		if resOwner != owner {
			prefix = ServicePrefix(resOwner) + "_"
		}
		for k, v := range derivedFor(localVar, port) {
			if _, exists := ctx[prefix+k]; !exists {
				ctx[prefix+k] = v
			}
		}
		return strconv.Itoa(port), nil
	case isHostKind(localVar):
		return hostLocalhost, nil
	case isURLKind(localVar):
		// The URL composes from the matching _PORT, which must already be
		// resolved.
		base := strings.TrimSuffix(localVar, "_URL")
		if portStr := ctx[base+"_PORT"]; portStr != "" {
			port, _ := strconv.Atoi(portStr)
			return urlFor(base, port), nil
		}
		return "", nil
	default:
		// A constant from this env block, already in ctx, or a value carrying
		// references that compose.Substitute resolves in the second pass.
		return ctx[varName], nil
	}
}

func isSynthesizedKind(k string) bool { return isHostKind(k) || isURLKind(k) }

func isHostKind(k string) bool { return k != "_HOST" && strings.HasSuffix(k, "_HOST") }

func isURLKind(k string) bool { return k != "_URL" && strings.HasSuffix(k, "_URL") }

// mergePortRefs returns a copy of env with one synthetic `__port_N` entry per
// raw compose ports: mapping. The synthetic keys carry those mapping strings
// through resolveContext so compose.Refs picks up their embedded `${VAR}`
// references; they never reach the final output.
func mergePortRefs(env map[string]string, ports []string) map[string]string {
	out := make(map[string]string, len(env)+len(ports))
	maps.Copy(out, env)
	for i, p := range ports {
		out[fmt.Sprintf("__port_%d", i)] = p
	}
	return out
}

// entriesOf returns vars as entries sorted by key, so output is deterministic.
func entriesOf(vars map[string]string) []Entry {
	entries := make([]Entry, 0, len(vars))
	for _, k := range slices.Sorted(maps.Keys(vars)) {
		entries = append(entries, Entry{Key: k, Value: vars[k]})
	}
	return entries
}
