package runner

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os/exec"
	"strconv"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// streamContainerLogs runs `podman logs -f <cid>` and pipes its output through
// the daemon's log writer. It exits when ctx is cancelled or the container is
// removed, closing the writer so its file handle and subscribers release.
func (r *Runner) streamContainerLogs(ctx context.Context, cid string, writer *logs.Writer) {
	defer func() { _ = writer.Close() }()
	cmd := exec.CommandContext(ctx, "podman", "logs", "-f", cid)
	// `podman logs -f` mirrors the container's stdout and stderr onto its own,
	// so piping each through the matching writer preserves the stream tag.
	cmd.Stdout = writer.Stdout()
	cmd.Stderr = writer.Stderr()
	_ = cmd.Run()
}

// startContainer brings t up as a podman-compose container in its profile,
// labeling it for adoption and passing env so compose's ${VAR} port
// substitution resolves. --no-deps sidesteps podman-compose 1.5.0's broken
// depends_on wait. Naming the compose service starts t alone: a one-shot its
// profile also enables would otherwise come up with t's labels.
func (r *Runner) startContainer(ctx context.Context, inst *Instance, t *discovery.Target, env, warnings []string) (*Instance, error) {
	id := t.ID()
	procCtx, cancel := context.WithCancel(context.Background())
	if err := r.begin(inst, nil, cancel); err != nil {
		return nil, err
	}

	if err := podman(ctx, env, "compose",
		"-p", composeProjectName(t.Stack, t.Service),
		"-f", r.stacks.Service(t.Stack, t.Service).ComposePath,
		"--profile", t.Name,
		containerLabelArgs(t.Stack, t.Service, t.Name),
		dockerFormatBuildArgs,
		"up", "-d", "--build", "--no-deps", t.ComposeName,
	); err != nil {
		r.markTerminated(id, anovelv1.ExitReason_EXIT_REASON_ERROR, "compose up: "+err.Error())
		return nil, fmt.Errorf("compose up for %s: %w", id, err)
	}

	cid := targetContainer(ctx, t)
	if cid == "" {
		r.markTerminated(id, anovelv1.ExitReason_EXIT_REASON_ERROR, "container resolved no ID after compose up")
		return nil, fmt.Errorf("could not resolve container ID for %s", id)
	}

	r.mu.Lock()
	inst.ContainerID = cid
	// Health starts at STARTING, and the watcher follows podman's healthcheck
	// from there.
	inst.Health = anovelv1.Health_HEALTH_STARTING
	killed := inst.Phase == anovelv1.Phase_PHASE_STOPPING
	r.mu.Unlock()
	// A kill during compose up found no container to stop and left the
	// instance STOPPING, so the start stops the container itself.
	if killed {
		if err := r.killContainer(ctx, id, 0); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%s: %w", id, errKilledWhileStarting)
	}
	r.advance(id, anovelv1.Phase_PHASE_STARTING, anovelv1.Phase_PHASE_RUNNING)

	// Stream `podman logs -f` into the same JSON-line store go-exec mode uses,
	// so `a-novel run logs <target>` shows the container's own output. A log
	// writer that fails to open costs this start only its log capture.
	if logWriter, err := r.logs.OpenForWrite(id, t.Stack, t.Service, t.Name); err == nil {
		// Surface the value-free missing-secret warnings so `run logs` and
		// the TUI show the operator what to set.
		for _, w := range warnings {
			_, _ = fmt.Fprintln(logWriter.Stderr(), w)
		}
		go r.streamContainerLogs(procCtx, cid, logWriter)
	}

	go r.watchContainer(procCtx, id, cid)

	r.mu.RLock()
	defer r.mu.RUnlock()
	out := *inst
	return &out, nil
}

// targetContainer returns the ID of t's container, found through the adoption
// labels set at creation or, failing that, compose's naming. It returns "" when
// neither finds one.
func targetContainer(ctx context.Context, t *discovery.Target) string {
	if ids, err := podmanIDs(ctx, labelFilters(t.Stack, t.Service, t.Name)...); err == nil && len(ids) > 0 {
		return ids[0]
	}
	return containerByName(ctx, composeProjectName(t.Stack, t.Service), t.ComposeName)
}

// watchContainer polls `podman inspect` every 2s to update the instance's
// phase, health, and exit code, one inspect per running container per tick. It
// returns when the container exits or ctx is cancelled.
func (r *Runner) watchContainer(ctx context.Context, id, cid string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	// First tick: immediate, so a fast-exiting container is observed
	// without waiting 2s.
	for {
		phase, health, exitCode, err := podmanInspect(ctx, cid)
		if err != nil {
			// A failed inspect means the container is gone, so mark
			// the instance terminated.
			r.markTerminated(id, anovelv1.ExitReason_EXIT_REASON_CRASHED, err.Error())
			return
		}
		r.updateContainerState(id, phase, health)
		if podmanPhase(phase) == anovelv1.Phase_PHASE_TERMINATED {
			reason := anovelv1.ExitReason_EXIT_REASON_SUCCESS
			r.mu.RLock()
			stopping := r.instances[id] != nil && r.instances[id].Phase == anovelv1.Phase_PHASE_STOPPING
			r.mu.RUnlock()
			switch {
			case stopping:
				reason = anovelv1.ExitReason_EXIT_REASON_KILLED
			case exitCode != 0:
				reason = anovelv1.ExitReason_EXIT_REASON_ERROR
			}
			r.markTerminated(id, reason, "")
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// InfraState is one row in the batched podman scan: the container ID and
// translated phase of a single (stack, service, infraName) tuple.
type InfraState struct {
	ContainerID string
	Phase       anovelv1.Phase
}

// InfraStatesOf returns the live state of every infra container in the given
// stack, keyed by "<service>/<infraName>". It costs one podman call whatever
// the stack's size.
//
// It reads no health: `podman ps --format json` omits Health.Status, and
// reading it costs one inspect per container. PHASE_RUNNING with
// HEALTH_UNSPECIFIED renders correctly downstream, as a green ● in the TUI and
// "running" in CLI ps.
//
// A podman error yields an empty map.
func (r *Runner) InfraStatesOf(ctx context.Context, stack string) map[string]InfraState {
	states, gen, ok := r.cachedInfraStates(stack)
	if ok {
		return states
	}

	entries, err := podmanPS(ctx, labelFilters(stack)...)
	if err != nil {
		return map[string]InfraState{}
	}

	states = infraStatesFrom(entries)
	r.storeInfraStates(stack, gen, states)
	return states
}

// cachedInfraStates returns a copy of the stack's cached scan while it is
// fresh. It also returns the cache generation, which a new scan hands to
// storeInfraStates.
func (r *Runner) cachedInfraStates(stack string) (map[string]InfraState, uint64, bool) {
	r.infraStateMu.Lock()
	defer r.infraStateMu.Unlock()
	if entry, ok := r.infraStateCache[stack]; ok && time.Since(entry.at) < infraStateCacheTTL {
		return maps.Clone(entry.states), r.infraStateGen, true
	}
	return nil, r.infraStateGen, false
}

// storeInfraStates caches a copy of a scan that started at generation gen. An
// invalidation since then skips the write, so a scan older than a mutation
// cannot bring back the state the mutation changed.
func (r *Runner) storeInfraStates(stack string, gen uint64, states map[string]InfraState) {
	r.infraStateMu.Lock()
	defer r.infraStateMu.Unlock()
	if r.infraStateGen == gen {
		r.infraStateCache[stack] = infraStateCacheEntry{at: time.Now(), states: maps.Clone(states)}
	}
}

// infraStatesFrom keys the infra containers of a podman scan by
// "<service>/<infraName>". Target containers carry anovel.target, and the
// runner tracks them through its own Instance records instead.
func infraStatesFrom(entries []psEntry) map[string]InfraState {
	states := make(map[string]InfraState)
	for _, e := range entries {
		if _, isTarget := e.Labels[labelTarget]; isTarget {
			continue
		}
		svc, infraName := e.Labels[labelService], composeServiceName(e.Labels)
		if svc == "" || infraName == "" {
			continue
		}
		states[svc+"/"+infraName] = InfraState{ContainerID: e.ID, Phase: podmanPhase(e.State)}
	}
	return states
}

// composeServiceName returns the compose service a container runs, the infra
// name discovery reads from the compose file. podman-compose mirrors the
// docker label into its own.
func composeServiceName(labels map[string]string) string {
	return cmp.Or(labels["com.docker.compose.service"], labels["io.podman.compose.service"])
}

// InfraContainer runs `podman <verb> --time 10` on one infra container, found
// by (stack, service, infraName), leaving the rest of the service's infra up.
// With "stop" the container survives in Exited state, so a restart keeps its
// pod and network attachments; with "restart" it comes back in place, keeping
// its volume bindings and labels. It errors when no container matches, whether
// already down or never up.
func (r *Runner) InfraContainer(ctx context.Context, stack, service, infraName, verb string) error {
	// Drop the cached state so the next ps reflects the change at once.
	defer r.InvalidateInfraStateCache()
	st, ok := r.InfraStatesOf(ctx, stack)[service+"/"+infraName]
	if !ok || st.ContainerID == "" {
		return fmt.Errorf("no container for %s/%s/%s", stack, service, infraName)
	}
	if err := podman(ctx, nil, verb, "--time", "10", st.ContainerID); err != nil {
		return fmt.Errorf("podman %s %s: %w", verb, st.ContainerID, err)
	}
	return nil
}

// updateContainerState folds one inspect result into the runner's view of
// a container instance, translating podman's state and health words into
// the Phase and Health enums. A terminated instance is never revived, and
// a stopping instance isn't bumped back to running; the exited state is
// handled by watchContainer, not here. A phase change emits its PhaseEvent.
func (r *Runner) updateContainerState(id, state, health string) {
	r.mu.Lock()
	inst, ok := r.instances[id]
	if !ok || inst.Phase == anovelv1.Phase_PHASE_TERMINATED {
		r.mu.Unlock()
		return
	}
	var ev PhaseEvent
	var changed bool
	switch phase := podmanPhase(state); phase {
	case anovelv1.Phase_PHASE_RUNNING:
		if inst.Phase != anovelv1.Phase_PHASE_STOPPING {
			ev, changed = setPhase(inst, phase)
		}
	case anovelv1.Phase_PHASE_STARTING, anovelv1.Phase_PHASE_STOPPING:
		ev, changed = setPhase(inst, phase)
	}
	inst.Health = podmanHealth(health)
	r.mu.Unlock()
	if changed {
		r.emitPhase(ev)
	}
}

// killContainer issues `podman stop -t <grace>` to the container.
//
// A stop that fails leaves the container running, so the error travels back to the caller and the
// instance keeps its STOPPING phase. Reporting a kill that did not happen sends the next start into
// a name or port collision with a container the operator was told had gone.
func (r *Runner) killContainer(ctx context.Context, id string, grace time.Duration) error {
	r.mu.RLock()
	inst, ok := r.instances[id]
	var cid string
	if ok {
		cid = inst.ContainerID
	}
	r.mu.RUnlock()
	// A start in flight has no container yet. It finds the instance STOPPING
	// once compose up returns, and stops the container itself.
	if cid == "" {
		return nil
	}
	seconds := max(int(grace.Seconds()), 0)
	if err := podman(ctx, nil, "stop", "-t", strconv.Itoa(seconds), cid); err != nil {
		return fmt.Errorf("podman stop %s: %w", cid, err)
	}
	// The watcher reaches markTerminated when it observes the exit. Calling it here covers a watcher
	// whose context is already closed, so the phase settles either way.
	r.markTerminated(id, anovelv1.ExitReason_EXIT_REASON_KILLED, "")

	return nil
}
