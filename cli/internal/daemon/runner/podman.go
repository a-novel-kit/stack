package runner

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/shared/podman"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// podmanPhases maps podman's state words onto a Phase. Any other word maps to
// PHASE_UNSPECIFIED.
var podmanPhases = map[string]anovelv1.Phase{
	podman.StateRunning: anovelv1.Phase_PHASE_RUNNING,
	"paused":            anovelv1.Phase_PHASE_RUNNING,
	"created":           anovelv1.Phase_PHASE_STARTING,
	"configured":        anovelv1.Phase_PHASE_STARTING,
	"stopping":          anovelv1.Phase_PHASE_STOPPING,
	podman.StateExited:  anovelv1.Phase_PHASE_TERMINATED,
	"stopped":           anovelv1.Phase_PHASE_TERMINATED,
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
	case podman.HealthHealthy:
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

// composeProjectName is the prefix-aware compose project, "<stack>_<service>".
// Every podman-compose invocation uses it, which is what isolates one stack
// from another.
func composeProjectName(stack, service string) string {
	return stack + "_" + service
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
	out, err := podman.Output(ctx, nil, append([]string{"ps", "-a", "--format", "json"}, filters...)...)
	if err != nil {
		return nil, err
	}
	var entries []psEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// containerByName finds a container through compose's `<project>_<service>_N`
// naming, the fallback for podman-compose versions that swallow the
// --podman-run-args label flag. It returns "" when none matches.
func containerByName(ctx context.Context, project, composeName string) string {
	if ids, _ := podman.IDs(ctx, nil, "--filter", "name="+project+"_"+composeName); len(ids) > 0 {
		return ids[0]
	}
	return ""
}
