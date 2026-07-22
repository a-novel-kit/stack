package runner

import (
	"bytes"
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
	var containers, targets int
	// `--format json` is the parseable contract: `{{.Labels}}` renders Go's
	// `map[k:v k:v]`, which has no documented parser and breaks on values
	// holding spaces or colons, such as image tags.
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
		Created int64             `json:"Created"` // unix seconds — parseable, unlike podman's "11 minutes ago" status text
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

		// At least one container of this stack and service is alive, so the
		// infra session counts as Up.
		r.markInfraSessionUp(stack, service)

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
		tgt := r.findTargetInDiscovery(stack, service, target)
		if tgt == nil {
			// Discovery does not know this target, so the compose file
			// changed since the container was created. Leave it orphaned
			// for a manual `podman rm`.
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
	svc := r.findServiceInDiscovery(stack, service)
	if svc == nil {
		return
	}
	// One inspect covers both the name and the ports.
	out, err := exec.CommandContext(ctx, "podman", "inspect", cid,
		"--format", "{{.Name}}|{{json .NetworkSettings.Ports}}").Output()
	if err != nil {
		return
	}
	pipe := bytes.IndexByte(out, '|')
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
