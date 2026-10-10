package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// infraSession tracks the per-service "infra is up" record.
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
}

// newInfraSession returns an empty, not-yet-Up session for the service.
func newInfraSession(stack, service string) *infraSession {
	return &infraSession{Stack: stack, Service: service, OneShotResults: make(map[string]anovelv1.ExitReason)}
}

// sessionKey is the map key for infraSessions.
func sessionKey(stack, service string) string { return stack + "/" + service }

// infraConsumer is the allocator consumer holding the ports a service's infra
// references, released when KillInfra tears the infra down.
func infraConsumer(stack, service string) string { return sessionKey(stack, service) + "-infra" }

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
}

// ActiveInfraSessions returns one InfraSessionRef per currently-tracked
// infra session, whether or not it reached Up.
func (r *Runner) ActiveInfraSessions() []InfraSessionRef {
	r.sessMu.RLock()
	defer r.sessMu.RUnlock()
	out := make([]InfraSessionRef, 0, len(r.infraSessions))
	for _, s := range r.infraSessions {
		out = append(out, InfraSessionRef{Stack: s.Stack, Service: s.Service})
	}
	return out
}

// StartInfra brings up a service's infrastructure containers, waits for
// healthchecks, then auto-runs every one-shot target in compose-dep order. It
// is idempotent: an already-up service is a no-op.
//
// oneShotsMode selects the mode for the auto-run one-shots, defaulting to
// go-exec. The runner allocates the infra ports itself through
// env.Builder.ForService, so compose's `${POSTGRES_PORT}` substitutes to a real
// number on every path, the dependency walk included.
func (r *Runner) StartInfra(ctx context.Context, stack, service string, oneShotsMode anovelv1.Mode) error {
	// `compose up` creates containers the next ListServices must see. The
	// idempotent early return changes no state, so invalidating there is
	// harmless.
	defer r.InvalidateInfraStateCache()
	svc, err := r.findService(stack, service)
	if err != nil {
		return err
	}
	key := sessionKey(stack, service)
	r.sessMu.Lock()
	if sess, ok := r.infraSessions[key]; ok && sess.Up {
		r.sessMu.Unlock()
		return nil // idempotent — already up
	}
	sess := newInfraSession(stack, service)
	r.infraSessions[key] = sess
	r.sessMu.Unlock()
	dropSession := func() {
		r.sessMu.Lock()
		delete(r.infraSessions, key)
		r.sessMu.Unlock()
	}

	// Allocate the port slots infra services reference, so compose's
	// substitution at infra-up time produces real port numbers.
	envEntries, err := r.builder.ForService(svc, infraConsumer(stack, service))
	if err != nil {
		dropSession()
		return fmt.Errorf("infra env for %s/%s: %w", stack, service, err)
	}

	// 1. Bring up the profile-less compose services, which compose's default
	//    rules resolve to exactly the infra entries.
	if err := podman(ctx, env.Environ(envEntries), "compose",
		"-p", composeProjectName(stack, service),
		"-f", svc.ComposePath,
		containerLabelArgs(stack, service),
		"up", "-d", "--build",
	); err != nil {
		dropSession()
		return fmt.Errorf("infra up for %s/%s: %w", stack, service, err)
	}

	// 2. Wait for healthchecks (where declared). Bounded — 60s default.
	if err := r.waitInfraHealthy(ctx, svc, 60*time.Second); err != nil {
		// A health-wait failure leaves the containers up for debugging, and
		// the session stays un-Up so later target starts surface the issue.
		return fmt.Errorf("infra %s/%s did not become healthy: %w", stack, service, err)
	}
	r.sessMu.Lock()
	sess.Up = true
	r.sessMu.Unlock()

	// 3. Auto-run one-shots. Walk in compose-dep order so a one-shot
	//    that depends on another runs after.
	for _, ts := range topoSortOneShots(svc) {
		err := r.runOneShot(ctx, ts, oneShotsMode)
		result := anovelv1.ExitReason_EXIT_REASON_SUCCESS
		if err != nil {
			result = anovelv1.ExitReason_EXIT_REASON_ERROR
		}
		r.sessMu.Lock()
		sess.OneShotResults[ts.Name] = result
		r.sessMu.Unlock()
		if err != nil {
			return fmt.Errorf("one-shot %s/%s failed: %w", service, ts.Name, err)
		}
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
	// Collect the service's live targets: they refuse the teardown, or with
	// force get cascade-killed first.
	r.mu.RLock()
	var live []*Instance
	for _, inst := range r.instances {
		if inst.Service == service && inst.Stack == stack && inst.Live() {
			live = append(live, inst)
		}
	}
	if len(live) > 0 && !force {
		target := live[0].Target
		r.mu.RUnlock()
		return fmt.Errorf("%s/%s has running targets (e.g., %s); kill them first or use --force", stack, service, target)
	}
	r.mu.RUnlock()
	for _, inst := range live {
		_ = r.Kill(ctx, inst.ID, 5*time.Second)
	}
	// `compose down` tears down the project's infra and any orphaned
	// containers. Without --volume the postgres data survives; only
	// `volume clear` destroys volumes.
	err = podman(ctx, nil, "compose",
		"-p", composeProjectName(stack, service), "-f", svc.ComposePath,
		"down", "--remove-orphans", "-t", "10")
	// Reset session regardless of compose's exit; the user can re-run
	// to recover from a half-stuck state.
	r.sessMu.Lock()
	delete(r.infraSessions, sessionKey(stack, service))
	r.sessMu.Unlock()
	if r.alloc != nil {
		r.alloc.Release(infraConsumer(stack, service))
	}
	if err != nil {
		return fmt.Errorf("compose down for %s/%s: %w", stack, service, err)
	}
	return nil
}

// runOneShot spawns a one-shot target and blocks until it terminates.
// Returns nil iff it terminated with EXIT_REASON_SUCCESS.
func (r *Runner) runOneShot(ctx context.Context, t *discovery.Target, mode anovelv1.Mode) error {
	if _, err := r.launch(ctx, t, mode); err != nil {
		return err
	}
	// Block until terminated, polling every 500ms, with a 5-minute bound as a
	// safety net.
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

// infraContainerState is one container's inspected state, or the fact that its
// inspect failed.
type infraContainerState struct {
	phase      string
	health     string
	exitCode   int
	inspectErr bool
}

// infraHealthy reports whether a service's infra is ready to depend on.
//
// declaredInfra is len(svc.Infra). A service that declares no infra is ready
// with nothing to wait for. One that declares infra but whose containers have
// not been resolved yet is not ready: an empty container query is a state to
// wait through, since the label query sees nothing while Postgres still runs
// initdb.
func infraHealthy(declaredInfra int, states []infraContainerState) bool {
	if declaredInfra == 0 {
		return true
	}

	if len(states) == 0 {
		return false
	}

	for _, s := range states {
		if s.inspectErr {
			return false
		}

		running := s.phase == pmPhaseRunning && (s.health == pmHealthHealthy || s.health == "-")
		oneShotDone := s.phase == pmPhaseExited && s.exitCode == 0

		if !running && !oneShotDone {
			return false
		}
	}

	return true
}

// resolveInfraContainerIDs returns the container IDs of a service's infra,
// preferring the adoption labels and falling back to the compose naming
// convention, the same fallback startContainer uses. Without it, versions that
// swallow the label flag make the label query come back empty, and the wait
// mistakes that for "no infra".
func resolveInfraContainerIDs(ctx context.Context, svc *discovery.Service) ([]string, error) {
	ids, err := podmanIDs(ctx, labelFilters(svc.Stack, svc.Name)...)
	if err != nil {
		return nil, fmt.Errorf("list infra containers for %s: %w", composeProjectName(svc.Stack, svc.Name), err)
	}
	if len(ids) > 0 {
		return ids, nil
	}

	// The label filter found nothing. Resolve each declared infra by name before
	// concluding there is nothing running.
	for _, in := range svc.Infra {
		if cid := containerByName(ctx, composeProjectName(svc.Stack, svc.Name), in.Name); cid != "" {
			ids = append(ids, cid)
		}
	}

	return ids, nil
}

// waitInfraHealthy polls every container in the project until each one is
// either running and healthy (long-runners) or exited with success
// (one-shots). It gives up after timeout with an error.
func (r *Runner) waitInfraHealthy(ctx context.Context, svc *discovery.Service, timeout time.Duration) error {
	// A service that declares no infra has nothing to wait for, and this is the
	// only place that is known for certain rather than inferred from an empty
	// container query.
	if len(svc.Infra) == 0 {
		return nil
	}

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		ids, err := resolveInfraContainerIDs(ctx, svc)
		if err != nil {
			return err
		}

		states := make([]infraContainerState, 0, len(ids))

		for _, cid := range ids {
			phase, health, exitCode, inspectErr := podmanInspect(ctx, cid)
			states = append(states, infraContainerState{
				phase:      phase,
				health:     health,
				exitCode:   exitCode,
				inspectErr: inspectErr != nil,
			})
		}

		if infraHealthy(len(svc.Infra), states) {
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

// topoSortOneShots returns the service's one-shot targets with dependencies
// first, so migrations runs before a rotate-keys that depends on it.
// Independent one-shots keep discovery's name order.
func topoSortOneShots(svc *discovery.Service) []*discovery.Target {
	// Build a name → Target map of one-shots.
	byName := make(map[string]*discovery.Target)
	for _, t := range svc.Targets {
		if t.Kind == discovery.TargetKindOneShot {
			byName[t.ComposeName] = t
		}
	}
	// A target joins the result only after all of its one-shot deps.
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
	for _, t := range svc.Targets {
		if byName[t.ComposeName] == t {
			visit(t)
		}
	}
	return out
}

// findService resolves (stack, service) against the runner's discovery
// snapshot, for the infra and dependency code that has no access to the server.
func (r *Runner) findService(stack, service string) (*discovery.Service, error) {
	if svc := r.stacks.Service(stack, service); svc != nil {
		return svc, nil
	}
	return nil, fmt.Errorf("service %q not found in stack %q", service, stack)
}
