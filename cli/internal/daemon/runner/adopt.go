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
// It also re-flags each per-service infra session Up, so a target start after
// adoption does not spin infra again. One-shot results stay unrestored, since
// nothing records whether they succeeded in the session that is now gone; being
// idempotent, they re-run on the next infra-up.
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

	for _, e := range entries {
		cid := e.ID
		stack := e.Labels[labelStack]
		service := e.Labels[labelService]
		target := e.Labels[labelTarget]
		if stack == "" || service == "" {
			continue
		}
		containers++

		// `podman ps -a` lists exited containers too, and an infra session marked Up short-circuits
		// EnsureDepsReady: the target then starts against a database that is not running. Only a
		// container podman reports as running counts.
		phase, health := translatePodmanStatus(e.Status)
		if phase == anovelv1.Phase_PHASE_RUNNING {
			r.markInfraSessionUp(stack, service)
		}

		if target == "" {
			// An infra container carries no target label and needs no
			// Instance record, but the env allocator must be re-seeded with
			// the host ports it is bound to. Otherwise the next Acquire,
			// say for the migrations one-shot's POSTGRES_PORT, picks a
			// fresh port and the one-shot connects to the wrong one.
			r.reseedAllocator(ctx, stack, service, cid)
			continue
		}

		// Target container — find it in discovery and reconstitute.
		tgt, _ := r.stacks.Target(stack + "/" + service + "/" + target)
		if tgt == nil {
			// Discovery does not know this target, so the compose file
			// changed since the container was created. Leave it orphaned
			// for a manual `podman rm`.
			continue
		}
		inst := &Instance{
			ID:          tgt.ID(),
			Target:      tgt.Name,
			Service:     tgt.Service,
			Stack:       tgt.Stack,
			Phase:       phase,
			Mode:        anovelv1.Mode_MODE_CONTAINER,
			Health:      health,
			ContainerID: cid,
			StartedAt:   time.Unix(e.Created, 0),
		}
		r.mu.Lock()
		// Skip a target that already holds a live record.
		if _, exists := r.instances[inst.ID]; exists {
			r.mu.Unlock()
			continue
		}
		r.instances[inst.ID] = inst
		r.mu.Unlock()

		// Resume watching and log streaming on the adopted container. A log
		// writer that fails to open costs this adoption only its streaming.
		if phase != anovelv1.Phase_PHASE_TERMINATED && r.logs != nil {
			ctxAdopt, cancel := context.WithCancel(context.Background())
			r.mu.Lock()
			inst.cancel = cancel
			r.mu.Unlock()
			if w, err := r.logs.OpenForWrite(inst.ID, stack, service, target); err == nil {
				go r.streamContainerLogs(ctxAdopt, cid, w)
			}
			go r.watchContainer(ctxAdopt, inst.ID, cid)
		}
		targets++
	}
	return containers, targets
}

// reseedAllocator re-records every host-port → `${VAR}` mapping for an adopted
// infra container, so a later Acquire returns the same host port the running
// container is bound to. Without it a daemon restart moves POSTGRES_PORT
// between sessions, and every target connecting afterward gets "connection
// refused".
//
// The mapping goes:
//  1. `podman inspect` → NetworkSettings.Ports = { "5432/tcp": [{HostPort: "38631"}] }
//  2. compose Infra.Ports = [ "${POSTGRES_PORT}:5432" ]
//  3. match each inspected pair to a compose mapping by container-side port,
//     and take its ${VAR}
//  4. Reserve(owner=service, localVar=VAR, port=hostPort, consumer=session)
//
// A pair that fails to parse is skipped, keeping adoption cheap and infallible.
func (r *Runner) reseedAllocator(ctx context.Context, stack, service, cid string) {
	if r.alloc == nil {
		return
	}
	// The compose `ports:` block maps container-side ports back to ${VAR}
	// names. Container names follow compose's `<project>_<infraName>_N`
	// pattern, and infraName picks the right Infra record among the several a
	// service may declare.
	svc := r.stacks.Service(stack, service)
	if svc == nil {
		return
	}
	// One inspect covers both the name and the ports.
	out, err := exec.CommandContext(ctx, "podman", "inspect", cid,
		"--format", "{{.Name}}|{{json .NetworkSettings.Ports}}").Output()
	if err != nil {
		return
	}
	rawName, portsJSON, ok := strings.Cut(string(out), "|")
	if !ok {
		return
	}
	rawName = strings.TrimPrefix(strings.TrimSpace(rawName), "/") // podman sometimes prefixes
	infra := svc.FindInfra(extractInfraNameFromContainerName(rawName, composeProjectName(stack, service)))
	if infra == nil {
		return
	}
	// Parse podman's port map: { "5432/tcp": [{HostIp:"0.0.0.0", HostPort:"38631"}] }.
	var portMap map[string][]struct {
		HostIP   string `json:"HostIp"`
		HostPort string `json:"HostPort"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(portsJSON)), &portMap); err != nil {
		return
	}
	// Build container-side port → host port lookup.
	containerToHost := make(map[string]int)
	for cport, bindings := range portMap {
		if len(bindings) == 0 {
			continue
		}
		// cport is "5432/tcp"; trim the proto.
		port, _, _ := strings.Cut(cport, "/")
		host, err := strconv.Atoi(bindings[0].HostPort)
		if err != nil {
			continue
		}
		containerToHost[port] = host
	}
	// Walk Infra.Ports and re-seed.
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
// (healthy)", "Exited (0) 2 minutes ago", or "Created", into our Phase and
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
