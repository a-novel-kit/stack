package runner

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// AdoptOrphanContainers scans podman for containers carrying our
// adoption labels (anovel.stack / anovel.service / anovel.target) and
// reconstitutes runner.Instance records for every one. Called at daemon
// startup so a `core kill` + `core start` cycle doesn't lose
// container-mode supervision. Spec §3.4 step 3.
//
// Per-service infra sessions are also re-flagged Up so the dep-walker
// doesn't try to spin infra again when a target start follows
// adoption. One-shot results aren't restored (no signal for "did this
// succeed in the session that's now gone") — they'll re-run on the
// next infra-start cycle, which is consistent with spec §5.5's
// "idempotent one-shots run on every infra-up".
//
// Returns the number of adopted containers + targets for reporting.
func (r *Runner) AdoptOrphanContainers(ctx context.Context) (containers, targets int) {
	// Use --format json — Go's default map-formatting (used by
	// `{{.Labels}}`) emits `map[k:v k:v]` which has no documented
	// parser and collides with values containing spaces or colons
	// (e.g., image tags). The JSON output is the contract.
	out, err := exec.CommandContext(ctx, "podman", "ps", "-a",
		"--filter", "label=anovel.stack",
		"--format", "json").Output()
	if err != nil {
		return 0, 0
	}
	var entries []struct {
		ID      string            `json:"Id"`
		Status  string            `json:"Status"`
		Labels  map[string]string `json:"Labels"`
		Created int64             `json:"Created"` // unix seconds, much nicer than "11 minutes ago"
	}
	if err := json.Unmarshal(out, &entries); err != nil {
		return 0, 0
	}
	for _, e := range entries {
		cid := e.ID
		status := e.Status
		labels := e.Labels
		stack := labels["anovel.stack"]
		service := labels["anovel.service"]
		target := labels["anovel.target"]
		if stack == "" || service == "" {
			continue
		}
		containers++

		// Mark the infra session Up — we have at least one container
		// from this stack/service alive.
		r.markInfraSessionUp(stack, service)

		if target == "" {
			// Infra container (no target label) — no Instance record
			// needed. But we DO need to re-seed the env allocator with
			// the host ports this container is currently bound to;
			// otherwise the next Acquire (for, e.g., the migrations
			// one-shot's POSTGRES_PORT) picks a fresh port and the
			// one-shot connects to the wrong host port.
			r.reseedAllocator(ctx, stack, service, cid)
			continue
		}

		// Target container — find it in discovery and reconstitute.
		tgt := r.findTargetInDiscovery(stack, service, target)
		if tgt == nil {
			// Discovery doesn't know this target (compose file changed
			// since the container was created). Leave the container
			// orphaned; user can `podman rm` manually.
			continue
		}
		phase, health := translatePodmanStatus(status)
		startedAt := time.Unix(e.Created, 0)
		inst := &Instance{
			ID:          tgt.ID(),
			Target:      tgt.Name,
			Service:     tgt.Service,
			Stack:       tgt.Stack,
			Phase:       phase,
			Mode:        ModeContainer,
			Health:      health,
			ContainerID: cid,
			StartedAt:   startedAt,
		}
		r.mu.Lock()
		// Skip if we already have a live record (shouldn't happen at
		// startup, but defensive).
		if _, exists := r.instances[inst.ID]; exists {
			r.mu.Unlock()
			continue
		}
		r.instances[inst.ID] = inst
		r.mu.Unlock()

		// Resume watching + log streaming on the adopted container.
		// Best-effort: if the log writer can't open (e.g., disk full),
		// just skip log streaming for this adoption.
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

// reseedAllocator re-records every host-port → `${VAR}` mapping for an
// adopted infra container, so subsequent Acquire calls (e.g., from a
// one-shot or long-runner's ForTarget) return the same host port the
// already-running container is bound to. Without this, daemon restart
// silently moves POSTGRES_PORT between sessions and any target that
// connects to it after the restart gets "connection refused".
//
// Mapping algorithm:
//  1. `podman inspect` → NetworkSettings.Ports = { "5432/tcp": [{HostPort: "38631"}] }
//  2. Compose Infra.Ports = [ "${POSTGRES_PORT}:5432" ]
//  3. For each (containerPort, hostPort) inspect pair, find the matching
//     compose port-mapping by container-side port → extract its ${VAR}.
//  4. Reserve(owner=service, localVar=VAR, port=hostPort, consumer=session).
//
// Best-effort: any parse or RPC error logs (silently for now) and skips
// that pair. The adoption itself stays cheap and infallible.
func (r *Runner) reseedAllocator(ctx context.Context, stack, service, cid string) {
	if r.alloc == nil {
		return
	}
	// Find the infra discovery record. We need the compose ports: block
	// to map container-side ports back to ${VAR} names. The container
	// name follows compose's pattern `<project>_<infraName>_N`; extract
	// infraName so we can pick the right Infra record (a service may
	// have multiple infras — e.g., postgres + mailserver).
	svc := r.findServiceInDiscovery(stack, service)
	if svc == nil {
		return
	}
	// Inspect for both name + ports.
	out, err := exec.CommandContext(ctx, "podman", "inspect", cid,
		"--format", "{{.Name}}|{{json .NetworkSettings.Ports}}").Output()
	if err != nil {
		return
	}
	pipe := strings.IndexByte(string(out), '|')
	if pipe < 0 {
		return
	}
	rawName := strings.TrimSpace(string(out)[:pipe])
	rawName = strings.TrimPrefix(rawName, "/") // podman sometimes prefixes
	portsJSON := strings.TrimSpace(string(out)[pipe+1:])
	infraName := extractInfraNameFromContainerName(rawName, composeProjectName(stack, service))
	if infraName == "" {
		return
	}
	var infra *discovery.Infra
	for _, in := range svc.Infra {
		if in.Name == infraName {
			infra = in
			break
		}
	}
	if infra == nil {
		return
	}
	// Parse podman's port map: { "5432/tcp": [{HostIp:"0.0.0.0", HostPort:"38631"}] }.
	var portMap map[string][]struct {
		HostIP   string `json:"HostIp"`
		HostPort string `json:"HostPort"`
	}
	if err := json.Unmarshal([]byte(portsJSON), &portMap); err != nil {
		return
	}
	// Build container-side port → host port lookup.
	containerToHost := make(map[string]int)
	for cport, bindings := range portMap {
		if len(bindings) == 0 {
			continue
		}
		// cport is "5432/tcp"; trim the proto.
		port := cport
		if slash := strings.IndexByte(port, '/'); slash > 0 {
			port = port[:slash]
		}
		host, err := strconv.Atoi(bindings[0].HostPort)
		if err != nil {
			continue
		}
		containerToHost[port] = host
	}
	// Walk Infra.Ports and re-seed.
	consumer := sessionKey(stack, service) + "-infra"
	for _, raw := range infra.Ports {
		varName, containerPort := parseInfraPortMapping(raw)
		if varName == "" || containerPort == "" {
			continue
		}
		hostPort, ok := containerToHost[containerPort]
		if !ok {
			continue
		}
		r.alloc.Reserve(service, varName, hostPort, consumer)
	}
}

// findServiceInDiscovery returns the discovery record for (stack, service),
// or nil if not found.
func (r *Runner) findServiceInDiscovery(stack, service string) *discovery.Service {
	for _, st := range r.discovery {
		if st.Name != stack {
			continue
		}
		for _, svc := range st.Services {
			if svc.Name == service {
				return svc
			}
		}
	}
	return nil
}

// containerInfraNameRe matches the trailing infra name in compose's
// container-naming pattern: `<project>_<infraName>_<replica>`.
var containerInfraNameRe = regexp.MustCompile(`_(\d+)$`)

// extractInfraNameFromContainerName recovers the infra service name
// (e.g., "postgres-template") from a compose-generated container name
// like "default_service-template_postgres-template_1".
func extractInfraNameFromContainerName(containerName, projectName string) string {
	// Strip the project prefix.
	prefix := projectName + "_"
	if !strings.HasPrefix(containerName, prefix) {
		return ""
	}
	rest := containerName[len(prefix):]
	// Trim trailing `_<replica>` if present.
	if m := containerInfraNameRe.FindStringIndex(rest); m != nil {
		rest = rest[:m[0]]
	}
	return rest
}

// infraPortRe matches a compose port mapping like "${POSTGRES_PORT}:5432"
// and captures the var name + container-side port.
var infraPortRe = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-[^}]*)?\}:(\d+)$`)

// parseInfraPortMapping returns (varName, containerPort) from a
// "${VAR}:N" mapping, or ("", "") if the mapping isn't in the
// expected form (e.g., a literal "5432:5432" mapping has no allocated
// var to re-seed).
func parseInfraPortMapping(raw string) (string, string) {
	m := infraPortRe.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

// markInfraSessionUp flips the infra session for (stack, service) to
// Up without re-running one-shots. Idempotent.
func (r *Runner) markInfraSessionUp(stack, service string) {
	r.sessMu.Lock()
	defer r.sessMu.Unlock()
	key := sessionKey(stack, service)
	if sess, ok := r.infraSessions[key]; ok {
		sess.Up = true
		return
	}
	r.infraSessions[key] = &infraSession{
		Stack:              stack,
		Service:            service,
		Up:                 true,
		OneShotResults:     make(map[string]anovelv1.ExitReason),
		AllocationConsumer: key + "-infra",
	}
}

// findTargetInDiscovery is the adoption-time target lookup. Returns nil
// if the (stack, service, target) triple doesn't match anything the
// daemon discovered.
func (r *Runner) findTargetInDiscovery(stack, service, target string) *discovery.Target {
	for _, st := range r.discovery {
		if st.Name != stack {
			continue
		}
		for _, svc := range st.Services {
			if svc.Name != service {
				continue
			}
			for _, t := range svc.Targets {
				if t.Name == target {
					return t
				}
			}
		}
	}
	return nil
}

// translatePodmanStatus maps podman's status string into our Phase +
// Health enums. Examples of podman status: "Up 5 seconds (healthy)",
// "Exited (0) 2 minutes ago", "Created".
func translatePodmanStatus(status string) (anovelv1.Phase, anovelv1.Health) {
	s := strings.ToLower(status)
	health := anovelv1.Health_HEALTH_UNKNOWN
	switch {
	case strings.Contains(s, "(healthy)"):
		health = anovelv1.Health_HEALTH_HEALTHY
	case strings.Contains(s, "(unhealthy)"):
		health = anovelv1.Health_HEALTH_UNHEALTHY
	case strings.Contains(s, "(starting)"):
		health = anovelv1.Health_HEALTH_STARTING
	}
	switch {
	case strings.HasPrefix(s, "up"):
		return anovelv1.Phase_PHASE_RUNNING, health
	case strings.HasPrefix(s, "created"):
		return anovelv1.Phase_PHASE_STARTING, health
	case strings.HasPrefix(s, "paused"):
		return anovelv1.Phase_PHASE_RUNNING, health
	case strings.HasPrefix(s, "exited"), strings.HasPrefix(s, "stopped"):
		return anovelv1.Phase_PHASE_TERMINATED, anovelv1.Health_HEALTH_UNSPECIFIED
	default:
		return anovelv1.Phase_PHASE_RUNNING, health
	}
}

