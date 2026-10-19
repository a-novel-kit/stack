package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// EnsureDepsReady walks `t`'s compose `depends_on` chain and verifies
// every upstream dependency is ready before allowing the target to
// start. A dependency counts as ready when:
//
//	(phase=running AND (mode=go-exec OR health=healthy))
//	  OR (phase=terminated AND exitReason=success)
//
// Behavior by dep kind:
//   - infra: ensures the service's infra session is Up. Auto-triggers
//     StartInfra if it isn't (cascade).
//   - one-shot target: ensures the current infra session recorded
//     success for it. Auto-triggers via the infra session if the session
//     is Up but the one-shot hasn't run yet. Refuses if the previous run
//     failed (caller must re-start infra or fix the underlying issue).
//   - long-runner target: must already be running. A long-runner is never
//     auto-started; that stays an explicit user action.
//
// A long-runner also needs every one-shot of its service, which its compose
// file leaves out: its standalone image runs them at boot, so a plain
// `podman compose` up waits on no one-shot, which podman would run again.
func (r *Runner) EnsureDepsReady(ctx context.Context, t *discovery.Target, svc *discovery.Service, oneShotsMode anovelv1.Mode) error {
	for _, depName := range t.DependsOn {
		if err := r.ensureDepReady(ctx, svc, depName, oneShotsMode); err != nil {
			return err
		}
	}

	oneShots := topoSortOneShots(svc)
	if t.Kind != discovery.TargetKindLongRunner || len(oneShots) == 0 {
		return nil
	}
	if err := r.ensureInfraReady(ctx, svc, oneShotsMode); err != nil {
		return fmt.Errorf("infra: %w", err)
	}
	for _, oneShot := range oneShots {
		if err := r.ensureOneShotSatisfied(ctx, svc, oneShot, oneShotsMode); err != nil {
			return err
		}
	}
	return nil
}

// ensureDepReady readies one compose dependency of a target of svc, classified
// against the service's discovered infra and targets.
func (r *Runner) ensureDepReady(ctx context.Context, svc *discovery.Service, depName string, oneShotsMode anovelv1.Mode) error {
	if svc.FindInfra(depName) != nil {
		if err := r.ensureInfraReady(ctx, svc, oneShotsMode); err != nil {
			return fmt.Errorf("infra dep %s: %w", depName, err)
		}
		return nil
	}
	dep := svc.FindTargetByComposeName(depName)
	switch {
	case dep == nil:
		// An unknown dep is most likely a cross-service reference, so skip it.
		// TODO: resolve cross-service dependencies here.
		return nil
	case dep.Kind == discovery.TargetKindOneShot:
		return r.ensureOneShotSatisfied(ctx, svc, dep, oneShotsMode)
	}
	inst, ok := r.Instance(dep.ID())
	if !ok || inst.Phase != anovelv1.Phase_PHASE_RUNNING {
		return fmt.Errorf(
			"long-runner dep %s/%s is not running — start it explicitly first (`a-novel run start %s/%s`)",
			svc.Name, dep.Name, svc.Name, dep.Name)
	}
	// A container long-runner with a healthcheck must be healthy. go-exec or
	// no-healthcheck containers count as ready when running.
	if inst.Mode == anovelv1.Mode_MODE_CONTAINER && dep.Kind == discovery.TargetKindLongRunner &&
		(inst.Health == anovelv1.Health_HEALTH_UNHEALTHY || inst.Health == anovelv1.Health_HEALTH_STARTING) {
		return fmt.Errorf(
			"long-runner dep %s/%s is %s — wait for it to become healthy",
			svc.Name, dep.Name, strings.ToLower(strings.TrimPrefix(inst.Health.String(), "HEALTH_")))
	}
	return nil
}

// ensureInfraReady triggers StartInfra if the service's session isn't Up.
// Idempotent — StartInfra is itself a no-op when already running.
func (r *Runner) ensureInfraReady(ctx context.Context, svc *discovery.Service, oneShotsMode anovelv1.Mode) error {
	if sess, ok := r.InfraSession(svc.Stack, svc.Name); ok && sess.Up {
		return nil
	}
	return r.StartInfra(ctx, svc.Stack, svc.Name, oneShotsMode)
}

// ensureOneShotSatisfied verifies the one-shot has succeeded in the
// current infra session, OR runs it now. Refuses-with-hint if a previous
// run failed (caller must fix the failure and re-run infra).
func (r *Runner) ensureOneShotSatisfied(ctx context.Context, svc *discovery.Service, t *discovery.Target, mode anovelv1.Mode) error {
	sess, ok := r.InfraSession(svc.Stack, svc.Name)
	if !ok || !sess.Up {
		return fmt.Errorf("one-shot dep %s/%s requires infra up — call `a-novel run service infra start %s` first",
			svc.Name, t.Name, svc.Name)
	}
	if prev, recorded := sess.OneShotResults[t.Name]; recorded {
		if prev == anovelv1.ExitReason_EXIT_REASON_SUCCESS {
			return nil
		}
		return fmt.Errorf("one-shot dep %s/%s last exited %s — restart infra to retry (`a-novel run service infra kill %s && a-novel run service infra start %s`)",
			svc.Name, t.Name, prev, svc.Name, svc.Name)
	}
	// Not yet run in this session — run it now.
	if err := r.runOneShot(ctx, t, mode); err != nil {
		return fmt.Errorf("one-shot %s/%s: %w", svc.Name, t.Name, err)
	}
	// Record success in the session.
	r.sessMu.Lock()
	defer r.sessMu.Unlock()
	if s, ok := r.infraSessions[sessionKey(svc.Stack, svc.Name)]; ok {
		s.OneShotResults[t.Name] = anovelv1.ExitReason_EXIT_REASON_SUCCESS
	}
	return nil
}
