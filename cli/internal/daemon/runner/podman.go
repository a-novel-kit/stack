package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Podman state and health words, spelled as `podman ps` and `podman inspect`
// emit them. They are a podman API contract, so a change here follows a podman
// version.
const (
	pmPhaseRunning  = "running"
	pmPhaseExited   = "exited"
	pmHealthHealthy = "healthy"
)

// podmanPhases maps podman's state words onto a Phase. Any other word maps to
// PHASE_UNSPECIFIED.
var podmanPhases = map[string]anovelv1.Phase{
	pmPhaseRunning: anovelv1.Phase_PHASE_RUNNING,
	"paused":       anovelv1.Phase_PHASE_RUNNING,
	"created":      anovelv1.Phase_PHASE_STARTING,
	"configured":   anovelv1.Phase_PHASE_STARTING,
	"stopping":     anovelv1.Phase_PHASE_STOPPING,
	pmPhaseExited:  anovelv1.Phase_PHASE_TERMINATED,
	"stopped":      anovelv1.Phase_PHASE_TERMINATED,
}

// podmanPhase translates a podman state word into a Phase.
func podmanPhase(state string) anovelv1.Phase {
	return podmanPhases[strings.ToLower(state)]
}

// podmanHealth translates a podman healthcheck word into a Health. A container
// with no healthcheck reports "-", which maps to HEALTH_UNKNOWN like any other
// unrecognized word.
func podmanHealth(word string) anovelv1.Health {
	switch word {
	case pmHealthHealthy:
		return anovelv1.Health_HEALTH_HEALTHY
	case "unhealthy":
		return anovelv1.Health_HEALTH_UNHEALTHY
	case "starting":
		return anovelv1.Health_HEALTH_STARTING
	default:
		return anovelv1.Health_HEALTH_UNKNOWN
	}
}

// The adoption labels set on every container the daemon spawns. They name the
// owning stack, service, and target, so a restarted daemon finds its
// containers again. An infra container carries no target label.
const (
	labelStack   = "anovel.stack"
	labelService = "anovel.service"
	labelTarget  = "anovel.target"
)

// labelPairs returns the adoption labels set to values, taken in stack,
// service, target order.
func labelPairs(values ...string) []string {
	keys := [...]string{labelStack, labelService, labelTarget}
	pairs := make([]string, len(values))
	for i, v := range values {
		pairs[i] = keys[i] + "=" + v
	}
	return pairs
}

// labelFilters returns the `podman ps` filter args selecting the containers
// whose adoption labels equal values, taken in stack, service, target order.
func labelFilters(values ...string) []string {
	args := make([]string, 0, 2*len(values))
	for _, pair := range labelPairs(values...) {
		args = append(args, "--filter", "label="+pair)
	}
	return args
}

// containerLabelArgs returns the flag that sets the adoption labels, taken in
// stack, service, target order, on every container a compose invocation
// creates. --podman-run-args scopes the injection to `podman run`, keeping the
// labels out of podman-compose's internal `podman ps` calls, which reject
// --label and fail with exit 125.
func containerLabelArgs(values ...string) string {
	return "--podman-run-args=--label " + strings.Join(labelPairs(values...), " --label ")
}

// dockerFormatBuildArgs builds images in the docker format, as `a-novel test` does. Podman's default
// OCI format drops a Dockerfile's HEALTHCHECK, and the runner follows a container's health from it.
const dockerFormatBuildArgs = "--podman-build-args=--format docker -q"

// composeProjectName is the prefix-aware compose project, "<stack>_<service>".
// Every podman-compose invocation uses it, which is what isolates one stack
// from another.
func composeProjectName(stack, service string) string {
	return stack + "_" + service
}

// podman runs one podman command with env, nil meaning the daemon's own, and
// folds its combined output into the error so a failure shows what podman
// printed.
func podman(ctx context.Context, env []string, args ...string) error {
	cmd := exec.CommandContext(ctx, "podman", args...)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}

// psEntry is the slice of `podman ps --format json` the runner reads.
type psEntry struct {
	ID      string            `json:"Id"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Labels  map[string]string `json:"Labels"`
	Created int64             `json:"Created"` // unix seconds, parseable where Status reads "11 minutes ago"
}

// podmanPS lists the containers filters select, stopped ones included. JSON is
// the parseable contract: `{{.Labels}}` renders Go's `map[k:v k:v]`, which
// breaks on values holding spaces or colons, such as image tags.
func podmanPS(ctx context.Context, filters ...string) ([]psEntry, error) {
	out, err := exec.CommandContext(ctx, "podman", append([]string{"ps", "-a", "--format", "json"}, filters...)...).Output()
	if err != nil {
		return nil, err
	}
	var entries []psEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// podmanIDs returns the IDs of the containers filters select, stopped ones
// included.
func podmanIDs(ctx context.Context, filters ...string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "podman", append([]string{"ps", "-a", "--format", "{{.ID}}"}, filters...)...).Output()
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// containerByName finds a container through compose's `<project>_<service>_N`
// naming, the fallback for podman-compose versions that swallow the
// --podman-run-args label flag. It returns "" when none matches.
func containerByName(ctx context.Context, project, composeName string) string {
	if ids, _ := podmanIDs(ctx, "--filter", "name="+project+"_"+composeName); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// inspectFormat prints a container's state word, health word and exit code on
// one line.
const inspectFormat = "{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}-{{end}}|{{.State.ExitCode}}"

// podmanInspectAll inspects every container of ids in one podman call. Any
// failure, a vanished container included, fails the whole call.
func podmanInspectAll(ctx context.Context, ids []string) ([]infraContainerState, error) {
	args := append([]string{"inspect", "--format=" + inspectFormat}, ids...)
	out, err := exec.CommandContext(ctx, "podman", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", strings.Join(ids, " "), err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(ids) {
		return nil, fmt.Errorf("inspect %s: malformed output %q", strings.Join(ids, " "), string(out))
	}
	states := make([]infraContainerState, 0, len(ids))
	for _, line := range lines {
		s, ok := parseInspect(line)
		if !ok {
			return nil, fmt.Errorf("inspect %s: malformed output %q", strings.Join(ids, " "), string(out))
		}
		states = append(states, s)
	}
	return states, nil
}

// parseInspect reads one inspectFormat line.
func parseInspect(line string) (infraContainerState, bool) {
	parts := strings.SplitN(strings.TrimSpace(line), "|", 3)
	if len(parts) < 3 {
		return infraContainerState{}, false
	}
	// podman prints a bare integer, so Atoi fails only on output it never
	// produces.
	exitCode, _ := strconv.Atoi(parts[2])
	return infraContainerState{phase: parts[0], health: parts[1], exitCode: exitCode}, true
}
