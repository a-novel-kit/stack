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

// Podman state strings, spelled as `podman ps` and `podman inspect` emit them.
// They are a podman API contract shared by every switch site in this package,
// so a change here follows a podman version.
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

// composeProjectName is the prefix-aware compose project, "<stack>_<service>".
// Every podman-compose invocation uses it, which is what isolates one stack
// from another.
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
	// --podman-run-args scopes the injection to `podman run`, keeping the
	// labels out of podman-compose's internal `podman ps` calls, which reject
	// --label and fail with exit 125.
	return "--podman-run-args=" + strings.Join(parts, " ")
}

// StartContainer brings `id` up as a podman-compose container in the target's
// declared profile, labeling it for adoption and passing the synthesized env so
// compose's ${VAR} port substitution resolves. --no-deps sidesteps
// podman-compose 1.5.0's broken depends_on wait.
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

	// Find the container ID through the labels set at creation, falling back
	// to the name convention for podman-compose versions that swallow the
	// label flag.
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
	// A long-runner starts at health STARTING and the watcher follows
	// podman's healthcheck from there. A one-shot stays UNSPECIFIED, since
	// its lifecycle is what matters.
	inst.Health = anovelv1.Health_HEALTH_STARTING
	r.mu.Unlock()

	// Stream `podman logs -f` into the same JSON-line store go-exec mode uses,
	// so `a-novel run logs <target>` shows the container's own output.
	logWriter, err := r.logs.OpenForWrite(id, tgt.Stack, tgt.Service, tgt.Name)
	if err == nil {
		// Surface the value-free missing-secret warnings so `run logs` and
		// the TUI show the operator what to set.
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

// InfraStatesOf returns the live state of every infra container in the given
// stack, keyed by "<service>/<infraName>". It costs one podman call whatever
// the stack's size, which matters because each invocation carries roughly 1s of
// cold-start overhead on rootless WSL2 podman and the TUI polls every 2s.
//
// Health stays unset here: `podman ps --format json` omits Health.Status, and
// reading it costs one inspect per container. PHASE_RUNNING with
// HEALTH_UNSPECIFIED renders correctly downstream, as a green ● in the TUI and
// "running" in CLI ps.
//
// A podman error yields an empty map, so a caller can check
// `if state, ok := m[key]` without a nil guard.
func (r *Runner) InfraStatesOf(ctx context.Context, stack string) map[string]InfraState {
	// The TUI's ~2s poll hits this cache most of the time, and every mutating
	// RPC invalidates it so a stale entry never outlives a state change.
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
	// Snapshot the generation: the reseed below refuses to write the cache
	// when an invalidation fired meanwhile, so a scan started before a
	// mutation cannot resurrect pre-mutation state.
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
		// Target containers carry anovel.target and are tracked through the
		// runner's own Instance records, so skip them.
		if _, isTarget := e.Labels["anovel.target"]; isTarget {
			continue
		}
		// The compose service name lives in com.docker.compose.service,
		// mirrored by io.podman.compose.service. That is the infra name
		// discovery reads from the compose file.
		infraName := e.Labels["com.docker.compose.service"]
		if infraName == "" {
			infraName = e.Labels["io.podman.compose.service"]
		}
		if svc == "" || infraName == "" {
			continue
		}
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
	// Seed the cache for the TTL window, unless an invalidation landed during
	// the scan.
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

// KillInfraContainer stops one infra container by (stack, service, infraName),
// with a 10s grace. The container survives in Exited state, so a restart keeps
// its pod and network attachments. It errors when no container matches, whether
// already down or never up.
func (r *Runner) KillInfraContainer(ctx context.Context, stack, service, infraName string) error {
	// Drop the cached state so the next ps reflects the stopped container at
	// once.
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

// RestartInfraContainer restarts one infra container in place with `podman
// restart`, skipping compose-up entirely and preserving the container's
// existing volume bindings and labels.
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

// InfraStateOf returns one infra's phase, health, and container ID, reading the
// batched InfraStatesOf snapshot.
func (r *Runner) InfraStateOf(ctx context.Context, stack, service, infraName string) (anovelv1.Phase, anovelv1.Health, string) {
	st, ok := r.InfraStatesOf(ctx, stack)[service+"/"+infraName]
	if !ok {
		return anovelv1.Phase_PHASE_UNSPECIFIED, anovelv1.Health_HEALTH_UNSPECIFIED, ""
	}
	return st.Phase, st.Health, st.ContainerID
}

// podmanInspect returns one container's status, health, and exit code.
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

// updateContainerState folds one inspect result into the runner's view of
// a container instance, translating podman's status and health words into
// our Phase and Health enums. A terminated instance is never revived, and
// a stopping instance isn't bumped back to running; the exited state is
// handled by watchContainer, not here.
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

// killContainer issues `podman stop -t <grace>` to the container.
//
// A stop that fails leaves the container running, so the error travels back to the caller and the
// instance keeps its STOPPING phase. Reporting a kill that did not happen sends the next start into
// a name or port collision with a container the operator was told had gone.
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
		return fmt.Errorf("podman stop %s: %w\n%s", inst.ContainerID, err, string(out))
	}
	// The watcher reaches markTerminated when it observes the exit. Calling it here covers a watcher
	// whose context is already closed, so the phase settles either way.
	r.markTerminated(id, anovelv1.ExitReason_EXIT_REASON_KILLED, "")

	return nil
}
