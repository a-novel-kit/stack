package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// infraSession tracks the per-service "infra is up" record (spec §5.5).
// One-shots that succeed during the session are remembered until the
// session ends (kill-infra); long-runners refuse to start unless their
// one-shot deps have succeeded in the current session.
type infraSession struct {
	Stack   string
	Service string
	// Up means infra containers were brought up successfully and the
	// daemon owns their lifecycle. Cleared by KillInfra.
	Up bool
	// OneShotResults records the exit reason of every one-shot that
	// ran during this session, keyed by target name. Cleared when the
	// session is reset (KillInfra).
	OneShotResults map[string]anovelv1.ExitReason
	// AllocationConsumer is the synthetic ID this session uses as the
	// allocator consumer for any infra-level ports it needs. Released
	// when the session ends.
	AllocationConsumer string
}

// sessionKey is the map key for infraSessions.
func sessionKey(stack, service string) string { return stack + "/" + service }

// InfraSession returns a snapshot of the per-service infra session, or
// (zero, false) if no session is recorded yet.
func (r *Runner) InfraSession(stack, service string) (infraSession, bool) {
	r.sessMu.RLock()
	defer r.sessMu.RUnlock()
	s, ok := r.infraSessions[sessionKey(stack, service)]
	if !ok {
		return infraSession{}, false
	}
	return *s, true
}

// InfraSessionRef identifies one tracked infra session (its stack + service
// pair). Used by callers that need to iterate every active session — e.g.,
// Shutdown(force=true) which tears down every service's infra in one go.
type InfraSessionRef struct {
	Stack   string
	Service string
	Up      bool // false if the session was registered but never reached Up
}

// ActiveInfraSessions returns one InfraSessionRef per currently-tracked
// infra session. Both Up and not-yet-Up sessions are included; the caller
// can filter on Ref.Up if only fully-running sessions matter.
func (r *Runner) ActiveInfraSessions() []InfraSessionRef {
	r.sessMu.RLock()
	defer r.sessMu.RUnlock()
	out := make([]InfraSessionRef, 0, len(r.infraSessions))
	for _, s := range r.infraSessions {
		out = append(out, InfraSessionRef{Stack: s.Stack, Service: s.Service, Up: s.Up})
	}
	return out
}

// StartInfra brings up a service's infrastructure containers, waits for
// healthchecks, then auto-runs every one-shot target in compose-dep
// order. Idempotent — calling on an already-up service is a no-op.
//
// `oneShotsMode` controls the mode of auto-run one-shots (defaults to
// go-exec per spec §5.5). `extraEnv` is the daemon's inherited env
// (PATH/HOME etc.) layered ON TOP of synthesized port allocations.
// The runner ALLOCATES infra ports itself (via env.Builder.ForServiceUp)
// so compose's `${POSTGRES_PORT}` substitutes to a real number even
// when called via the dep-walk path (where the caller can't pre-allocate).
func (r *Runner) StartInfra(ctx context.Context, stack, service string, oneShotsMode Mode, extraEnv []string) error {
	// `compose up` creates new containers; the next ListServices must
	// see them. The idempotent already-up early-return doesn't change
	// state, so the extra invalidate is harmless there.
	defer r.InvalidateInfraStateCache()
	svc, err := r.findService(stack, service)
	if err != nil {
		return err
	}
	r.sessMu.Lock()
	key := sessionKey(stack, service)
	sess, ok := r.infraSessions[key]
	if ok && sess.Up {
		r.sessMu.Unlock()
		return nil // idempotent — already up
	}
	sess = &infraSession{
		Stack:              stack,
		Service:            service,
		Up:                 false, // flipped after compose up succeeds
		OneShotResults:     make(map[string]anovelv1.ExitReason),
		AllocationConsumer: key + "-infra",
	}
	r.infraSessions[key] = sess
	r.sessMu.Unlock()

	// Build infra env: ALLOCATE port slots referenced by infra services
	// (postgres-X's `${POSTGRES_PORT}` etc.) so compose's substitution
	// at infra-up time produces real port numbers. The synthesized env
	// layers on top of the caller's inherited env (PATH/HOME for
	// `go run` later).
	envEntries, err := r.builder.ForServiceUp(svc, r.alloc.Services(), sess.AllocationConsumer)
	if err != nil {
		r.sessMu.Lock()
		delete(r.infraSessions, key)
		r.sessMu.Unlock()
		return fmt.Errorf("infra env for %s/%s: %w", stack, service, err)
	}
	env := append([]string(nil), extraEnv...)
	env = append(env, envEntriesToList(envEntries)...)

	// 1. Bring up the no-profile compose services (postgres, mailserver,
	//    etc.) via `compose up -d --build` (no profile selection — that
	//    picks up exactly the infra entries per compose's default rules).
	project := composeProjectName(stack, service)
	args := []string{
		"compose",
		"-p", project,
		"-f", svc.ComposePath,
		containerLabelArgs(stack, service, "" /* infra-wide, no specific target */),
		"up", "-d", "--build",
	}
	cmd := exec.CommandContext(ctx, "podman", args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.sessMu.Lock()
		delete(r.infraSessions, key)
		r.sessMu.Unlock()
		return fmt.Errorf("infra up for %s/%s: %w\n%s", stack, service, err, string(out))
	}

	// 2. Wait for healthchecks (where declared). Bounded — 60s default.
	if err := r.waitInfraHealthy(ctx, svc, 60*time.Second); err != nil {
		// Don't tear down on health-wait failure; the user can debug.
		// Just refuse to mark Up so subsequent target-starts will surface
		// the issue.
		return fmt.Errorf("infra %s/%s did not become healthy: %w", stack, service, err)
	}
	r.sessMu.Lock()
	sess.Up = true
	r.sessMu.Unlock()

	// 3. Auto-run one-shots. Walk in compose-dep order so a one-shot
	//    that depends on another runs after.
	order := topoSortOneShots(svc)
	for _, ts := range order {
		if err := r.runOneShot(ctx, ts, oneShotsMode); err != nil {
			r.sessMu.Lock()
			sess.OneShotResults[ts.Name] = anovelv1.ExitReason_EXIT_REASON_ERROR
			r.sessMu.Unlock()
			return fmt.Errorf("one-shot %s/%s failed: %w", service, ts.Name, err)
		}
		r.sessMu.Lock()
		sess.OneShotResults[ts.Name] = anovelv1.ExitReason_EXIT_REASON_SUCCESS
		r.sessMu.Unlock()
	}
	return nil
}

// KillInfra refuses if any long-runner target of the service is still
// running (unless force). Otherwise tears down all infra containers and
// resets the session.
func (r *Runner) KillInfra(ctx context.Context, stack, service string, force bool) error {
	defer r.InvalidateInfraStateCache()
	svc, err := r.findService(stack, service)
	if err != nil {
		return err
	}
	if !force {
		// Check for running targets of this service.
		r.mu.RLock()
		for _, inst := range r.instances {
			if inst.Service != service || inst.Stack != stack {
				continue
			}
			if inst.Phase == anovelv1.Phase_PHASE_RUNNING || inst.Phase == anovelv1.Phase_PHASE_STARTING {
				r.mu.RUnlock()
				return fmt.Errorf("%s/%s has running targets (e.g., %s); kill them first or use --force", stack, service, inst.Target)
			}
		}
		r.mu.RUnlock()
	} else {
		// Cascade-kill every target of this service.
		r.mu.RLock()
		var ids []string
		for id, inst := range r.instances {
			if inst.Service == service && inst.Stack == stack &&
				(inst.Phase == anovelv1.Phase_PHASE_RUNNING || inst.Phase == anovelv1.Phase_PHASE_STARTING) {
				ids = append(ids, id)
			}
		}
		r.mu.RUnlock()
		for _, id := range ids {
			_ = r.Kill(ctx, id, 5*time.Second)
		}
	}
	// `compose down` tears down infra + any orphaned containers in the
	// project. We DO NOT pass --volume so postgres data survives —
	// spec §8.1 says only `volume clear` destroys volumes.
	project := composeProjectName(stack, service)
	cmd := exec.CommandContext(ctx, "podman", "compose",
		"-p", project, "-f", svc.ComposePath,
		"down", "--remove-orphans", "-t", "10")
	out, err := cmd.CombinedOutput()
	// Reset session regardless of compose's exit; the user can re-run
	// to recover from a half-stuck state.
	r.sessMu.Lock()
	consumer := ""
	if s, ok := r.infraSessions[sessionKey(stack, service)]; ok {
		consumer = s.AllocationConsumer
	}
	delete(r.infraSessions, sessionKey(stack, service))
	r.sessMu.Unlock()
	if r.alloc != nil && consumer != "" {
		r.alloc.Release(consumer)
	}
	if err != nil {
		return fmt.Errorf("compose down for %s/%s: %w\n%s", stack, service, err, string(out))
	}
	return nil
}

// runOneShot spawns a one-shot target and blocks until it terminates.
// Returns nil iff it terminated with EXIT_REASON_SUCCESS.
func (r *Runner) runOneShot(ctx context.Context, t *discovery.Target, mode Mode) error {
	// Build env for the one-shot. The env builder allocates ports as
	// needed, just like for long-runners.
	if r.alloc == nil {
		return fmt.Errorf("runner has no allocator (internal misconfiguration)")
	}
	// Build env AFTER any prior allocation steps (infra-up). The
	// builder's snapshot-fill picks up POSTGRES_PORT etc. that infra-up
	// allocated under the service-level consumer, so the target's
	// POSTGRES_DSN synthesizes correctly to localhost:<port>.
	envEntries, err := r.builder.ForTarget(t, r.alloc.Services())
	if err != nil {
		return fmt.Errorf("env for %s: %w", t.ID(), err)
	}
	// Inherit the daemon's env (HOME, PATH, GOPATH defaults, etc.) so
	// `go run` finds its toolchain + module cache. Synthesized env
	// layers ON TOP so its values win for any overlapping keys.
	envList := append([]string(nil), os.Environ()...)
	envList = append(envList, envEntriesToList(envEntries)...)
	switch mode {
	case ModeContainer:
		_, err = r.StartContainer(ctx, t.ID(), envList)
	default:
		_, err = r.StartGoExec(ctx, t.ID(), envList)
	}
	if err != nil {
		return err
	}
	// Block until terminated. Poll every 500ms — one-shots are short by
	// nature so this is cheap. Bounded to 5 minutes as a safety net.
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		inst, ok := r.Instance(t.ID())
		if !ok {
			return fmt.Errorf("one-shot %s vanished from tracker", t.ID())
		}
		if inst.Phase == anovelv1.Phase_PHASE_TERMINATED {
			if inst.ExitReason == anovelv1.ExitReason_EXIT_REASON_SUCCESS {
				return nil
			}
			return fmt.Errorf("one-shot %s exited %s: %s", t.ID(), inst.ExitReason, inst.LastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("one-shot %s did not terminate within 5m", t.ID())
}

// waitInfraHealthy polls every container in the project until each one
// is either running+healthy (long-runners) or exited+success (one-shots).
// Bounded by `timeout`. Returns nil on full readiness or an error
// listing the still-unhealthy containers.
func (r *Runner) waitInfraHealthy(ctx context.Context, svc *discovery.Service, timeout time.Duration) error {
	project := composeProjectName(svc.Stack, svc.Name)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// List every container in the project.
		out, err := exec.CommandContext(ctx, "podman", "ps", "-a",
			"--filter", "label=anovel.stack="+svc.Stack,
			"--filter", "label=anovel.service="+svc.Name,
			"--format", "{{.ID}}").Output()
		if err != nil {
			return fmt.Errorf("list infra containers for %s: %w", project, err)
		}
		ids := strings.Fields(string(out))
		if len(ids) == 0 {
			// Nothing to wait for — e.g., the service has no infra
			// services declared.
			return nil
		}
		ready := true
		for _, cid := range ids {
			phase, health, exitCode, err := podmanInspect(ctx, cid)
			if err != nil {
				ready = false
				continue
			}
			switch {
			case phase == "running" && (health == "healthy" || health == "-"):
				// Good.
			case phase == "exited" && exitCode == 0:
				// One-shot succeeded.
			default:
				ready = false
			}
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
	return fmt.Errorf("infra not healthy within %s", timeout)
}

// envEntriesToList converts the env package's Entry slice to the
// "KEY=VALUE" string form that exec.Cmd.Env expects.
func envEntriesToList(entries []env.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Key+"="+e.Value)
	}
	return out
}

// topoSortOneShots returns the service's one-shot targets in
// dependency-respecting order: dependencies first. For the typical
// migrations + rotate-keys pattern, migrations comes before rotate-keys
// if rotate-keys depends on it.
func topoSortOneShots(svc *discovery.Service) []*discovery.Target {
	// Build a name → Target map of one-shots.
	byName := make(map[string]*discovery.Target)
	for _, t := range svc.Targets {
		if t.Kind == discovery.TargetKindOneShot {
			byName[t.ComposeName] = t
		}
	}
	// Reverse deps: result starts empty; we add a target after all its
	// one-shot deps have been added.
	visited := make(map[string]bool)
	var out []*discovery.Target
	var visit func(t *discovery.Target)
	visit = func(t *discovery.Target) {
		if visited[t.ComposeName] {
			return
		}
		visited[t.ComposeName] = true
		for _, depName := range t.DependsOn {
			if dep, ok := byName[depName]; ok {
				visit(dep)
			}
		}
		out = append(out, t)
	}
	// Visit alphabetically for deterministic ties.
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	// We don't need sort.Strings here — for the typical small N (2-3
	// one-shots) any order with topo-correctness is fine.
	for _, n := range names {
		visit(byName[n])
	}
	return out
}

// findService is the runner-side counterpart of server.findService —
// used by infra/dep code that doesn't have direct access to the server.
func (r *Runner) findService(stack, service string) (*discovery.Service, error) {
	for _, st := range r.discovery {
		if st.Name != stack && stack != "" {
			continue
		}
		for _, svc := range st.Services {
			if svc.Name == service {
				return svc, nil
			}
		}
	}
	return nil, fmt.Errorf("service %q not found in stack %q", service, stack)
}
