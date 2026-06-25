package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Podman state strings, hoisted into constants so the half-dozen
// switch sites across container.go, infra.go, adopt.go, and deps.go
// agree on a single spelling. These are podman API contracts (the
// strings podman ps / inspect emit), not arbitrary internal labels —
// renaming them is a podman-version change, not a refactor.
const (
	pmPhaseRunning    = "running"
	pmPhasePaused     = "paused"
	pmPhaseCreated    = "created"
	pmPhaseConfigured = "configured"
	pmPhaseExited     = "exited"
	pmPhaseStopped    = "stopped"

	pmHealthHealthy   = "healthy"
	pmHealthUnhealthy = "unhealthy"
	pmHealthStarting  = "starting"
)

// streamContainerLogs runs `podman logs -f <cid>` and pipes its output
// through the daemon's log writer. Exits when ctx is cancelled (kill)
// or the container is removed (podman logs detects EOF). Closes the
// writer on exit so the file handle + subscribers release.
func (r *Runner) streamContainerLogs(ctx context.Context, cid string, writer *logs.Writer) {
	defer func() { _ = writer.Close() }()
	cmd := exec.CommandContext(ctx, "podman", "logs", "-f", cid)
	// podman's `logs -f` writes container stdout to its own stdout and
	// container stderr to its own stderr — pipe each through the
	// matching writer side so the stream tag is preserved.
	cmd.Stdout = writer.Stdout()
	cmd.Stderr = writer.Stderr()
	_ = cmd.Run()
}

// composeProjectName is the prefix-aware compose project:
// "<stack>_<service>". Used for every podman-compose invocation so
// multi-stack isolation just works.
func composeProjectName(stack, service string) string {
	return stack + "_" + service
}

// containerLabelArgs returns the --podman-args flag value used to inject
// our adoption labels onto every container spawned by a compose
// invocation. The triple (stack, service, target) is the unique key the
// daemon uses to find a container after a restart.
func containerLabelArgs(stack, service, target string) string {
	parts := []string{
		"--label", "anovel.stack=" + stack,
		"--label", "anovel.service=" + service,
	}
	if target != "" {
		parts = append(parts, "--label", "anovel.target="+target)
	}
	// --podman-run-args limits the injection to `podman run` invocations
	// — using the broader --podman-args also forwards these flags to
	// podman-compose's internal `podman ps` calls, which don't accept
	// --label and fail with exit 125. Run-args is the right scope.
	return "--podman-run-args=" + strings.Join(parts, " ")
}

// StartContainer brings up `id` as a podman-compose container in the
// target's declared profile, with: --no-deps to avoid the broken depends_on
// wait machinery in podman-compose 1.5.0+, --label injection so adoption
// works on the next daemon start, and env passthrough so compose's ${VAR}
// substitution resolves to our allocated ports.
//
// Phase 3 chunk 2 first cut. The actual log streaming is deferred to
// phase 5 (logs hub); for now container stdout/stderr stay in podman's
// own journal.
func (r *Runner) StartContainer(ctx context.Context, id string, env []string, warnings []string) (*Instance, error) {
	tgt, svc, err := r.resolveTarget(id)
	if err != nil {
		return nil, err
	}

	// Invariant checks (mutual exclusion, already-running idempotency).
	if existing, idempotent, err := r.canStart(id, ModeContainer); err != nil {
		return nil, err
	} else if idempotent {
		out := *existing
		return &out, nil
	}

	project := composeProjectName(svc.Stack, svc.Name)
	composeFile := svc.ComposePath
	profile := tgt.Profile

	// Register the instance early so concurrent Start callers see the
	// slot occupied.
	now := time.Now()
	procCtx, cancel := context.WithCancel(context.Background())
	inst := &Instance{
		ID:        id,
		Target:    tgt.Name,
		Service:   tgt.Service,
		Stack:     tgt.Stack,
		Phase:     anovelv1.Phase_PHASE_PENDING,
		Mode:      ModeContainer,
		StartedAt: now,
		cancel:    cancel,
	}
	r.mu.Lock()
	r.instances[id] = inst
	r.mu.Unlock()

	r.transition(id, anovelv1.Phase_PHASE_STARTING)

	// Invoke `podman compose ... up -d --build --no-deps`.
	// --no-deps is REQUIRED — see spec rationale + the long debug session
	// we went through with podman-compose 1.5.0's broken
	// depends_on: service_healthy wait machinery.
	args := []string{
		"compose",
		"-p", project,
		"-f", composeFile,
		"--profile", profile,
		containerLabelArgs(svc.Stack, svc.Name, tgt.Name),
		"up", "-d", "--build", "--no-deps",
	}
	cmd := exec.CommandContext(ctx, "podman", args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.markTerminated(id, anovelv1.ExitReason_EXIT_REASON_ERROR,
			fmt.Sprintf("compose up: %v\n%s", err, string(out)))
		cancel()
		return nil, fmt.Errorf("compose up for %s: %w\n%s", id, err, string(out))
	}

	// Find the container ID. We labeled it on creation, so the canonical
	// way to retrieve it is a label-filtered ps. Fall back to name-based
	// lookup if the label query returns nothing — defense against
	// podman-compose versions that swallow the --podman-args label flag.
	cid, err := findContainerByLabels(ctx, svc.Stack, svc.Name, tgt.Name)
	if err != nil || cid == "" {
		cid = findContainerByName(ctx, project, tgt.ComposeName)
	}
	if cid == "" {
		r.markTerminated(id, anovelv1.ExitReason_EXIT_REASON_ERROR,
			"container resolved no ID after compose up")
		cancel()
		return nil, fmt.Errorf("could not resolve container ID for %s", id)
	}

	r.mu.Lock()
	inst.ContainerID = cid
	inst.Phase = anovelv1.Phase_PHASE_RUNNING
	// Long-runner containers start with health=STARTING; the watcher
	// updates as podman's healthcheck progresses. One-shots end up in
	// HEALTH_UNSPECIFIED — they're transient and the lifecycle is what
	// matters.
	inst.Health = anovelv1.Health_HEALTH_STARTING
	r.mu.Unlock()

	// Open the log writer + spawn the per-container `podman logs -f`
	// streamer so `a-novel run logs <target>` shows the container's
	// actual output (not just our shell echoes). Same JSON-line format
	// as go-exec mode so the storage + streaming layers are uniform.
	logWriter, err := r.logs.OpenForWrite(id, tgt.Stack, tgt.Service, tgt.Name)
	if err == nil {
		// Surface any missing-secret warnings (value-free) into the target's
		// log so the operator sees what to set in `run logs` / the TUI.
		for _, w := range warnings {
			_, _ = fmt.Fprintln(logWriter.Stderr(), w)
		}
		go r.streamContainerLogs(procCtx, cid, logWriter)
	}

	go r.watchContainer(procCtx, id, cid)

	out2 := *inst
	return &out2, nil
}

// findContainerByLabels queries `podman ps -a` filtered on our adoption
// labels. Returns the first matching container ID, or "" if none.
func findContainerByLabels(ctx context.Context, stack, service, target string) (string, error) {
	cmd := exec.CommandContext(ctx, "podman", "ps", "-a",
		"--filter", "label=anovel.stack="+stack,
		"--filter", "label=anovel.service="+service,
		"--filter", "label=anovel.target="+target,
		"--format", "{{.ID}}")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return firstLine(string(out)), nil
}

// findContainerByName is the naming-convention fallback for podman-compose
// versions that don't honor --podman-args label injection. Compose names
// containers `<project>_<service-name>_1`.
func findContainerByName(ctx context.Context, project, composeServiceName string) string {
	cmd := exec.CommandContext(ctx, "podman", "ps", "-a",
		"--filter", "name="+project+"_"+composeServiceName,
		"--format", "{{.ID}}")
	out, _ := cmd.Output()
	return firstLine(string(out))
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// watchContainer polls `podman inspect` every 2s to update the instance's
// phase + health + exit code. Cheap enough for local dev (one inspect
// per running container per tick) and far simpler than wiring
// `podman events` streaming. Terminates when the container exits OR ctx
// is cancelled (via kill).
func (r *Runner) watchContainer(ctx context.Context, id, cid string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	// First tick: immediate, so a fast-exiting container is observed
	// without waiting 2s.
	for {
		phase, health, exitCode, err := podmanInspect(ctx, cid)
		if err != nil {
			// Container missing — typically means it's been removed.
			// Mark terminated unless we already did.
			r.markTerminated(id, anovelv1.ExitReason_EXIT_REASON_CRASHED, err.Error())
			return
		}
		r.updateContainerState(id, phase, health)
		if phase == pmPhaseExited || phase == pmPhaseStopped {
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

// InfraState is one row in the batched podman scan: container ID +
// translated phase + translated health for a single (stack, service,
// infraName) tuple.
type InfraState struct {
	ContainerID string
	Phase       anovelv1.Phase
	Health      anovelv1.Health
}

// InfraStatesOf returns the live state of every infra container in the
// given stack, keyed by "<service>/<infraName>". One podman call total
// regardless of how many services / infras the stack has — drastically
// faster than the prior per-infra approach (each podman invocation has
// ~1s cold-start overhead on rootless WSL2 podman, so N infras serial
// = N seconds, often exceeding the TUI's 2s poll cadence).
//
// Health: deliberately NOT fetched here. podman ps --format json
// doesn't expose Health.Status, and a per-container `podman inspect`
// would double the call count. PHASE_RUNNING + HEALTH_UNSPECIFIED
// renders correctly downstream (green ● in TUI, "running" in CLI ps).
// Callers needing precise health for a single infra can fall back to
// InfraStateOf (which does the inspect).
//
// Returns an empty map (not nil) on podman errors so callers can
// uniformly check `if state, ok := m[key]` without nil-guarding.
func (r *Runner) InfraStatesOf(ctx context.Context, stack string) map[string]InfraState {
	// Cache check — TUI poll cadence (~2s) hits this most of the time.
	// Mutating RPCs call InvalidateInfraStateCache so a stale entry
	// never outlives an explicit state change.
	r.infraStateMu.Lock()
	if entry, ok := r.infraStateCache[stack]; ok && time.Since(entry.at) < infraStateCacheTTL {
		// Copy the map so callers don't race a concurrent invalidation.
		cp := make(map[string]InfraState, len(entry.states))
		for k, v := range entry.states {
			cp[k] = v
		}
		r.infraStateMu.Unlock()
		return cp
	}
	// Snapshot the generation. The reseed at the end of this function
	// will refuse to write the cache if Invalidate fired in the
	// meantime — otherwise a long podman scan started PRE-mutation
	// would resurrect pre-mutation state after the mutation cleared
	// the cache.
	startGen := r.infraStateGen
	r.infraStateMu.Unlock()

	out := make(map[string]InfraState)
	cmd := exec.CommandContext(ctx, "podman", "ps", "-a",
		"--filter", "label=anovel.stack="+stack,
		"--format", "json")
	raw, err := cmd.Output()
	if err != nil {
		return out
	}
	var entries []struct {
		ID     string            `json:"Id"`
		Labels map[string]string `json:"Labels"`
		State  string            `json:"State"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return out
	}
	for _, e := range entries {
		svc := e.Labels["anovel.service"]
		// Skip target containers (those carry anovel.target); we only
		// care about infra here (target state is tracked via the
		// runner's own Instance records).
		if _, isTarget := e.Labels["anovel.target"]; isTarget {
			continue
		}
		// Compose service name lives in com.docker.compose.service
		// (mirrored by io.podman.compose.service). That's the infra
		// name we discover from the compose file.
		infraName := e.Labels["com.docker.compose.service"]
		if infraName == "" {
			infraName = e.Labels["io.podman.compose.service"]
		}
		if svc == "" || infraName == "" {
			continue
		}
		// Map lowercase JSON state directly: "running" → RUNNING,
		// "exited"/"stopped" → TERMINATED, "created" → STARTING.
		var p anovelv1.Phase
		switch strings.ToLower(e.State) {
		case pmPhaseRunning, pmPhasePaused:
			p = anovelv1.Phase_PHASE_RUNNING
		case pmPhaseCreated, pmPhaseConfigured:
			p = anovelv1.Phase_PHASE_STARTING
		case pmPhaseExited, pmPhaseStopped:
			p = anovelv1.Phase_PHASE_TERMINATED
		default:
			p = anovelv1.Phase_PHASE_UNSPECIFIED
		}
		out[svc+"/"+infraName] = InfraState{ContainerID: e.ID, Phase: p, Health: anovelv1.Health_HEALTH_UNSPECIFIED}
	}
	// Seed cache for the TTL window — but only if no invalidation
	// happened during our scan. Otherwise we'd resurrect pre-mutation
	// state for the next ListServices caller until TTL expires.
	cached := make(map[string]InfraState, len(out))
	for k, v := range out {
		cached[k] = v
	}
	r.infraStateMu.Lock()
	if r.infraStateGen == startGen {
		r.infraStateCache[stack] = infraStateCacheEntry{at: time.Now(), states: cached}
	}
	r.infraStateMu.Unlock()
	return out
}

// KillInfraContainer stops a single infra container by (stack,
// service, infraName). Uses podman stop with a 10s grace; the
// container survives in Exited state so the user can restart it
// without losing its pod / network attachments. Returns an error if
// no container matches (already down, or never up).
func (r *Runner) KillInfraContainer(ctx context.Context, stack, service, infraName string) error {
	// Drop the cached state so the next ps reflects the stopped
	// container immediately, even though podman's filtered scan
	// would have caught it inside the TTL anyway.
	defer r.InvalidateInfraStateCache()
	st, ok := r.InfraStatesOf(ctx, stack)[service+"/"+infraName]
	if !ok || st.ContainerID == "" {
		return fmt.Errorf("no container for %s/%s/%s", stack, service, infraName)
	}
	cmd := exec.CommandContext(ctx, "podman", "stop", "--time", "10", st.ContainerID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman stop %s: %w\n%s", st.ContainerID, err, string(out))
	}
	return nil
}

// RestartInfraContainer restarts a single infra container in place
// (`podman restart`, which is stop+start without recreating). Faster
// than KillInfra → StartInfra because it skips compose-up entirely
// and preserves the container's existing volume bindings / labels.
func (r *Runner) RestartInfraContainer(ctx context.Context, stack, service, infraName string) error {
	defer r.InvalidateInfraStateCache()
	st, ok := r.InfraStatesOf(ctx, stack)[service+"/"+infraName]
	if !ok || st.ContainerID == "" {
		return fmt.Errorf("no container for %s/%s/%s", stack, service, infraName)
	}
	cmd := exec.CommandContext(ctx, "podman", "restart", "--time", "10", st.ContainerID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman restart %s: %w\n%s", st.ContainerID, err, string(out))
	}
	return nil
}

// InfraStateOf is the legacy single-infra query — preserved for
// callers that don't have the batched map yet. Returns
// (phase, health, containerID). Internally uses InfraStatesOf for
// consistency.
func (r *Runner) InfraStateOf(ctx context.Context, stack, service, infraName string) (anovelv1.Phase, anovelv1.Health, string) {
	st, ok := r.InfraStatesOf(ctx, stack)[service+"/"+infraName]
	if !ok {
		return anovelv1.Phase_PHASE_UNSPECIFIED, anovelv1.Health_HEALTH_UNSPECIFIED, ""
	}
	return st.Phase, st.Health, st.ContainerID
}

// podmanInspect parses just the fields we care about (status, health,
// exit code) from one container. Returns (phase, health, exitCode, err).
func podmanInspect(ctx context.Context, cid string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, "podman", "inspect", cid,
		"--format", "{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}-{{end}}|{{.State.ExitCode}}")
	out, err := cmd.Output()
	if err != nil {
		return "", "", 0, fmt.Errorf("inspect %s: %w", cid, err)
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 3)
	if len(parts) < 3 {
		return "", "", 0, fmt.Errorf("inspect %s: malformed output %q", cid, string(out))
	}
	exitCode, _ := strconv.Atoi(parts[2])
	return parts[0], parts[1], exitCode, nil
}

// updateContainerState mutates the runner's view of a container instance
// to reflect the latest inspect result. Phase translations between
// podman's status words and our PHASE enum:
//
//	"running"   → PHASE_RUNNING
//	"created"   → PHASE_STARTING (container exists, no command running yet)
//	"exited"    → PHASE_TERMINATED (set by the caller — we only get
//	              here on running/created transitions)
//	"stopping"  → PHASE_STOPPING
//	"paused"    → PHASE_RUNNING (we don't model paused separately)
//
// Health: "healthy"|"unhealthy"|"starting"|"-" → our Health enum.
func (r *Runner) updateContainerState(id, phase, health string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst, ok := r.instances[id]
	if !ok || inst.Phase == anovelv1.Phase_PHASE_TERMINATED {
		return
	}
	switch phase {
	case pmPhaseRunning, pmPhasePaused:
		if inst.Phase != anovelv1.Phase_PHASE_STOPPING {
			inst.Phase = anovelv1.Phase_PHASE_RUNNING
		}
	case pmPhaseCreated:
		inst.Phase = anovelv1.Phase_PHASE_STARTING
	case "stopping":
		inst.Phase = anovelv1.Phase_PHASE_STOPPING
	}
	switch health {
	case pmHealthHealthy:
		inst.Health = anovelv1.Health_HEALTH_HEALTHY
	case pmHealthUnhealthy:
		inst.Health = anovelv1.Health_HEALTH_UNHEALTHY
	case pmHealthStarting:
		inst.Health = anovelv1.Health_HEALTH_STARTING
	default:
		inst.Health = anovelv1.Health_HEALTH_UNKNOWN
	}
}

// killContainer issues `podman stop -t <grace>` to the container, then
// closes the watch goroutine. The watcher does the final markTerminated
// when it observes the exited state.
func (r *Runner) killContainer(ctx context.Context, id string, grace time.Duration) error {
	r.mu.RLock()
	inst, ok := r.instances[id]
	r.mu.RUnlock()
	if !ok || inst.ContainerID == "" {
		return nil
	}
	seconds := int(grace.Seconds())
	if seconds < 0 {
		seconds = 0
	}
	cmd := exec.CommandContext(ctx, "podman", "stop", "-t", strconv.Itoa(seconds), inst.ContainerID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Surface but don't fail the kill — the watcher will eventually
		// observe and mark terminated.
		_ = out
	}
	// The watcher (if still running) will observe exited and call
	// markTerminated. If the watcher's ctx is already cancelled (e.g.,
	// we got killed twice in a row), force markTerminated here so
	// callers don't see a stuck stopping state.
	r.markTerminated(id, anovelv1.ExitReason_EXIT_REASON_KILLED, "")
	return nil
}
