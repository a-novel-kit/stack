package runner

import (
	"context"
	"fmt"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// EnsureDepsReady walks `t`'s compose `depends_on` chain and verifies
// every upstream dependency is ready before allowing the target to
// start. Spec §5.4 "ready" definition:
//
//	(phase=running AND (mode=go-exec OR health=healthy))
//	  OR (phase=terminated AND exitReason=success)
//
// Behavior by dep kind:
//   - infra: ensures the service's infra session is Up. Auto-triggers
//     StartInfra if it isn't (cascade per spec §5.4).
//   - one-shot target: ensures the current infra session recorded
//     success for it. Auto-triggers via the infra session if the session
//     is Up but the one-shot hasn't run yet. Refuses if the previous run
//     failed (caller must re-start infra or fix the underlying issue).
//   - long-runner target: must already be running. We DO NOT auto-start
//     long-runners — that's an explicit user action.
func (r *Runner) EnsureDepsReady(ctx context.Context, t *discovery.Target, svc *discovery.Service, oneShotsMode Mode, env []string) error {
	if len(t.DependsOn) == 0 {
		return nil
	}
	for _, depName := range t.DependsOn {
		// Classify the dep by looking it up in the service's discovered
		// targets / infra.
		if isInfra(svc, depName) {
			if err := r.ensureInfraReady(ctx, svc, oneShotsMode, env); err != nil {
				return fmt.Errorf("infra dep %s: %w", depName, err)
			}
			continue
		}
		if depTarget := findTarget(svc, depName); depTarget != nil {
			if depTarget.Kind == discovery.TargetKindOneShot {
				if err := r.ensureOneShotSatisfied(ctx, svc, depTarget, oneShotsMode); err != nil {
					return err
				}
				continue
			}
			// Long-runner — refuse-with-hint if not already running.
			inst, ok := r.Instance(depTarget.ID())
			if !ok || inst.Phase != anovelv1.Phase_PHASE_RUNNING {
				return fmt.Errorf(
					"long-runner dep %s/%s is not running — start it explicitly first (`a-novel run start %s/%s`)",
					svc.Name, depTarget.Name, svc.Name, depTarget.Name)
			}
			// For container long-runners with a healthcheck, require
			// healthy. go-exec or no-healthcheck containers count as
			// ready when running.
			if inst.Mode == ModeContainer && depTarget.Kind == discovery.TargetKindLongRunner {
				if inst.Health == anovelv1.Health_HEALTH_UNHEALTHY || inst.Health == anovelv1.Health_HEALTH_STARTING {
					return fmt.Errorf(
						"long-runner dep %s/%s is %s — wait for it to become healthy",
						svc.Name, depTarget.Name, healthLabel(inst.Health))
				}
			}
			continue
		}
		// Unknown dep — could be a cross-service reference (out of
		// scope for phase 3 per spec §1: "Cross-service credential /
		// token sharing. Deferred to a later API-keys spec."). For
		// now we skip with a warning.
		// TODO: when cross-service is spec'd, resolve here.
	}
	return nil
}

// ensureInfraReady triggers StartInfra if the service's session isn't Up.
// Idempotent — StartInfra is itself a no-op when already running.
func (r *Runner) ensureInfraReady(ctx context.Context, svc *discovery.Service, oneShotsMode Mode, env []string) error {
	if sess, ok := r.InfraSession(svc.Stack, svc.Name); ok && sess.Up {
		return nil
	}
	return r.StartInfra(ctx, svc.Stack, svc.Name, oneShotsMode, env)
}

// ensureOneShotSatisfied verifies the one-shot has succeeded in the
// current infra session, OR runs it now. Refuses-with-hint if a previous
// run failed (caller must fix the failure and re-run infra).
func (r *Runner) ensureOneShotSatisfied(ctx context.Context, svc *discovery.Service, t *discovery.Target, mode Mode) error {
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

// isInfra reports whether `name` is one of the service's infra entries.
func isInfra(svc *discovery.Service, name string) bool {
	for _, in := range svc.Infra {
		if in.Name == name {
			return true
		}
	}
	return false
}

// findTarget returns the target with compose-name `name`, or nil.
func findTarget(svc *discovery.Service, name string) *discovery.Target {
	for _, t := range svc.Targets {
		if t.ComposeName == name {
			return t
		}
	}
	return nil
}

// healthLabel renders a Health enum for error messages.
func healthLabel(h anovelv1.Health) string {
	switch h {
	case anovelv1.Health_HEALTH_HEALTHY:
		return "healthy"
	case anovelv1.Health_HEALTH_UNHEALTHY:
		return "unhealthy"
	case anovelv1.Health_HEALTH_STARTING:
		return "starting"
	default:
		return "unknown"
	}
}
