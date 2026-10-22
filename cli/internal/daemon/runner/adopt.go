package runner

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/shared/compose"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// AdoptOrphanContainers scans podman for containers carrying the adoption
// labels (anovel.stack, anovel.service, anovel.target) and reconstitutes an
// Instance record for each. The daemon calls it at startup, so a `core kill`
// and `core start` cycle keeps its container-mode supervision.
//
// It also flags each service's infra session Up, so a later target start does
// not bring the infra up again. One-shot results are not restored. The next
// long-runner start re-runs its one-shots, which are idempotent.
//
// It returns the number of containers and of targets adopted.
func (r *Runner) AdoptOrphanContainers(ctx context.Context) (int, int) {
	entries, err := podmanPS(ctx, "--filter", "label="+labelStack)
	if err != nil {
		return 0, 0
	}
	return r.adoptEntries(ctx, entries)
}

// adoptEntries reconstitutes state from a scan's entries, separated from the scan itself so the
// decisions it makes can be exercised against a fixture.
func (r *Runner) adoptEntries(ctx context.Context, entries []psEntry) (int, int) {
	var containers, targets int
	var infra []psEntry

	for _, e := range entries {
		stack, service := e.Labels[labelStack], e.Labels[labelService]
		if stack == "" || service == "" {
			continue
		}
		containers++

		phase, health := translatePodmanStatus(e.Status)
		if e.Labels[labelTarget] == "" {
			// `podman ps -a` lists exited containers too, and an infra session marked Up
			// short-circuits EnsureDepsReady: the target then starts against a database that is
			// not running. Only an infra container podman reports as running counts, since a
			// running target container says nothing about its database.
			if phase == anovelv1.Phase_PHASE_RUNNING {
				r.markInfraSessionUp(stack, service)
			}
			// An infra container carries no target label and needs no
			// Instance record.
			infra = append(infra, e)
			continue
		}

		if r.adoptTarget(e, phase, health) {
			targets++
		}
	}

	// The env allocator learns the infra host ports, so the next Acquire, such
	// as the migrations one-shot's POSTGRES_PORT, returns the port the
	// container is bound to.
	r.reseedAllocator(ctx, infra)
	return containers, targets
}

// adoptTarget reconstitutes the Instance of a target container and resumes
// watching it, and reports whether it did. A target discovery no longer knows,
// because the compose file changed since the container was created, stays
// orphaned for a manual `podman rm`; one that already holds a record is left
// as it is.
func (r *Runner) adoptTarget(e psEntry, phase anovelv1.Phase, health anovelv1.Health) bool {
	stack, service, target := e.Labels[labelStack], e.Labels[labelService], e.Labels[labelTarget]
	tgt, _ := r.stacks.Target(stack + "/" + service + "/" + target)
	if tgt == nil {
		return false
	}
	inst := &Instance{
		ID:          tgt.ID(),
		Target:      tgt.Name,
		Service:     tgt.Service,
		Stack:       tgt.Stack,
		Phase:       phase,
		Mode:        anovelv1.Mode_MODE_CONTAINER,
		Health:      health,
		ContainerID: e.ID,
		StartedAt:   time.Unix(e.Created, 0),
	}
	r.mu.Lock()
	if _, exists := r.instances[inst.ID]; exists {
		r.mu.Unlock()
		return false
	}
	r.instances[inst.ID] = inst
	r.mu.Unlock()

	// Resume polling and log streaming on the adopted container. A log
	// writer that fails to open costs this adoption only its streaming.
	if phase != anovelv1.Phase_PHASE_TERMINATED && r.logs != nil {
		ctxAdopt, cancel := context.WithCancel(context.Background())
		r.mu.Lock()
		inst.cancel = cancel
		r.mu.Unlock()
		if w, err := r.logs.OpenForWrite(inst.ID, stack, service, target); err == nil {
			go r.streamContainerLogs(ctxAdopt, e.ID, w)
		}
		go r.watchContainer(inst.ID, e.ID)
	}
	return true
}

// reseedAllocator re-records every host-port → `${VAR}` mapping of the adopted
// infra containers, so a later Acquire returns the same host port a running
// container is bound to. POSTGRES_PORT then stays put across a daemon restart.
// One podman call inspects every container.
//
// The mapping goes:
//  1. `podman inspect` → NetworkSettings.Ports = { "5432/tcp": [{HostPort: "38631"}] }
//  2. compose Infra.Ports = [ "${POSTGRES_PORT}:5432" ]
//  3. match each inspected pair to a compose mapping by container-side port,
//     and take its ${VAR}
//  4. Reserve(owner=service, localVar=VAR, port=hostPort, consumer=session)
//
// A container that vanished since the scan, or a pair that fails to parse, is
// skipped, keeping adoption cheap and infallible.
func (r *Runner) reseedAllocator(ctx context.Context, infra []psEntry) {
	if r.alloc == nil || len(infra) == 0 {
		return
	}
	byID := make(map[string]psEntry, len(infra))
	args := []string{"inspect", "--format={{.Id}}|{{.Name}}|{{json .NetworkSettings.Ports}}"}
	for _, e := range infra {
		byID[e.ID] = e
		args = append(args, e.ID)
	}
	// A vanished container fails the call, and podman still prints a line for
	// every other one. Each line names its container, so the others reseed
	// whatever the exit status.
	out, _ := exec.CommandContext(ctx, "podman", args...).Output()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		id, rest, _ := strings.Cut(line, "|")
		if e, ok := byID[id]; ok {
			r.reseedInfra(e.Labels[labelStack], e.Labels[labelService], rest)
		}
	}
}

// reseedInfra reserves the host ports of one infra container of stack and
// service. line holds the rest of its inspect line, "<name>|<ports JSON>".
func (r *Runner) reseedInfra(stack, service, line string) {
	// The compose `ports:` block maps container-side ports back to ${VAR}
	// names. Container names follow compose's `<project>_<infraName>_N`
	// pattern, and infraName picks the right Infra record among the several a
	// service may declare.
	svc := r.stacks.Service(stack, service)
	if svc == nil {
		return
	}
	rawName, portsJSON, ok := strings.Cut(line, "|")
	if !ok {
		return
	}
	rawName = strings.TrimPrefix(strings.TrimSpace(rawName), "/") // podman sometimes prefixes
	infra := svc.FindInfra(extractInfraNameFromContainerName(rawName, composeProjectName(stack, service)))
	if infra == nil {
		return
	}
	containerToHost, err := hostPorts(portsJSON)
	if err != nil {
		return
	}
	consumer := infraConsumer(stack, service)
	for _, raw := range infra.Ports {
		varName, containerPort, ok := compose.HostPort(raw)
		if !ok {
			continue
		}
		hostPort, ok := containerToHost[containerPort]
		if !ok {
			continue
		}
		r.alloc.Reserve(service, varName, hostPort, consumer)
	}
}

// hostPorts maps each container-side port of podman's port map, such as
// { "5432/tcp": [{"HostIp": "0.0.0.0", "HostPort": "38631"}] }, to the host port
// its first binding uses. A port without a numeric binding is left out.
func hostPorts(portsJSON string) (map[string]int, error) {
	var portMap map[string][]struct {
		HostPort string `json:"HostPort"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(portsJSON)), &portMap); err != nil {
		return nil, err
	}
	containerToHost := make(map[string]int)
	for containerPort, bindings := range portMap {
		if len(bindings) == 0 {
			continue
		}
		port, _, _ := strings.Cut(containerPort, "/") // "5432/tcp"
		if host, err := strconv.Atoi(bindings[0].HostPort); err == nil {
			containerToHost[port] = host
		}
	}
	return containerToHost, nil
}

// containerInfraNameRe matches the trailing infra name in compose's
// container-naming pattern: `<project>_<infraName>_<replica>`.
var containerInfraNameRe = regexp.MustCompile(`_(\d+)$`)

// extractInfraNameFromContainerName recovers the infra service name
// (e.g., "postgres-template") from a compose-generated container name
// like "default_service-template_postgres-template_1".
func extractInfraNameFromContainerName(containerName, projectName string) string {
	rest, ok := strings.CutPrefix(containerName, projectName+"_")
	if !ok {
		return ""
	}
	// Trim trailing `_<replica>` if present.
	if m := containerInfraNameRe.FindStringIndex(rest); m != nil {
		rest = rest[:m[0]]
	}
	return rest
}

// markInfraSessionUp flips the infra session for (stack, service) to
// Up without re-running one-shots. Idempotent.
func (r *Runner) markInfraSessionUp(stack, service string) {
	r.sessMu.Lock()
	defer r.sessMu.Unlock()
	key := sessionKey(stack, service)
	sess, ok := r.infraSessions[key]
	if !ok {
		sess = newInfraSession(stack, service)
		r.infraSessions[key] = sess
	}
	sess.Up = true
}

// translatePodmanStatus maps podman's status text, such as "Up 5 seconds
// (healthy)", "Exited (0) 2 minutes ago", or "Created", into the Phase and
// Health enums. Its first word is the state, "up" standing for running, and
// the parenthesized word after it the health.
func translatePodmanStatus(status string) (anovelv1.Phase, anovelv1.Health) {
	state, rest, _ := strings.Cut(strings.ToLower(status), " ")
	if state == "up" {
		state = pmPhaseRunning
	}
	phase := podmanPhase(state)
	if phase == anovelv1.Phase_PHASE_TERMINATED {
		return phase, anovelv1.Health_HEALTH_UNSPECIFIED
	}
	_, health, _ := strings.Cut(rest, "(")
	health, _, _ = strings.Cut(health, ")")
	return phase, podmanHealth(health)
}
