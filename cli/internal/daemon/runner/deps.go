package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// EnsureDepsReady readies everything t's compose depends_on names before t
// starts, by dependency kind:
//   - infra: the service's infra starts when it is down.
//   - one-shot target: the service's infra starts when it is down, whatever
//     the order of the dependencies. The one-shot must have succeeded in the
//     current infra session, and it runs now when it has not run yet. A failed
//     run refuses the start.
//   - long-runner target: it must already be running. A container one must not
//     report a starting or unhealthy healthcheck. The daemon never starts a
//     long-runner on its own.
//
// A long-runner also waits for every one-shot of its service. Its compose file
// leaves them out, since its standalone image runs them at boot, so the daemon
// adds the gate.
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

// ensureInfraReady triggers StartInfra if the service's session isn't Up. It
// is idempotent, as StartInfra is.
func (r *Runner) ensureInfraReady(ctx context.Context, svc *discovery.Service, oneShotsMode anovelv1.Mode) error {
	if sess, ok := r.InfraSession(svc.Stack, svc.Name); ok && sess.Up {
		return nil
	}
	return r.StartInfra(ctx, svc.Stack, svc.Name, oneShotsMode)
}

// ensureOneShotSatisfied verifies the one-shot has succeeded in the
// current infra session, OR runs it now. Refuses-with-hint if a previous
// run failed (caller must fix the failure and re-run infra).
//
// A one-shot runs against its service's infra, so the infra comes up first
// when it is down. Bringing it up runs the service's one-shots, this one
// included, so the result is then on record.
func (r *Runner) ensureOneShotSatisfied(ctx context.Context, svc *discovery.Service, t *discovery.Target, mode anovelv1.Mode) error {
	if err := r.ensureInfraReady(ctx, svc, mode); err != nil {
		return fmt.Errorf("infra: %w", err)
	}
	// A teardown between the bring-up and this read leaves no session.
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
	// A one-shot not yet run in this session runs now, and its outcome is
	// recorded, so a failure refuses later starts until the infra restarts.
	err := r.runOneShot(ctx, t, mode)
	r.recordOneShot(svc.Stack, svc.Name, t.Name, err)
	if err != nil {
		return fmt.Errorf("one-shot %s/%s: %w", svc.Name, t.Name, err)
	}
	return nil
}
