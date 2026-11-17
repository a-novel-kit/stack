package podman

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// DockerFormatBuildArgs makes podman-compose build images in the docker format.
// Podman's default OCI format drops a Dockerfile's HEALTHCHECK, and readiness
// follows a container's health.
const DockerFormatBuildArgs = "--podman-build-args=--format docker -q"

// Container states and the health word, spelled as podman prints them. They
// are a podman API contract, so a change here follows a podman version.
const (
	StateRunning  = "running"
	StateExited   = "exited"
	HealthHealthy = "healthy"
	// HealthNone stands for the health of a container without a healthcheck.
	HealthNone = "-"
)

// command prepares `podman args...` with env, nil keeping the caller's
// environment. Cancelling ctx kills podman, but podman-compose and its podman
// children keep the output pipes open, so WaitDelay bounds the wait for them.
func command(ctx context.Context, env []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "podman", args...)
	cmd.Env = env
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

// Output runs `podman args...` with env, nil keeping the caller's
// environment, and returns its trimmed stdout. A failure carries what podman
// printed on stderr.
func Output(ctx context.Context, env []string, args ...string) (string, error) {
	cmd := command(ctx, env, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// Run runs `podman args...` for its effect, as Output does, discarding stdout.
func Run(ctx context.Context, env []string, args ...string) error {
	_, err := Output(ctx, env, args...)
	return err
}

// IDs returns the IDs of the containers filters select, stopped ones included.
func IDs(ctx context.Context, env []string, filters ...string) ([]string, error) {
	out, err := Output(ctx, env, slices.Concat([]string{"ps", "-a", "--format", "{{.ID}}"}, filters)...)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// Container is the part of `podman inspect` the CLI reads.
type Container struct {
	ID              string `json:"Id"`
	Name            string `json:"Name"`
	State           State  `json:"State"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
}

// State is a container's run state. Health is nil without a healthcheck.
type State struct {
	Status   string `json:"Status"`
	ExitCode int    `json:"ExitCode"`
	Health   *struct {
		Status string `json:"Status"`
	} `json:"Health"`
}

// Inspect inspects every container of ids in one podman call. A container
// that vanished fails the call, and podman still reports every other one, so
// the result holds what podman printed even beside an error: nil when it
// printed nothing, an empty slice when it reported no container.
func Inspect(ctx context.Context, env, ids []string) ([]Container, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	cmd := command(ctx, env, slices.Concat([]string{"inspect", "--type", "container"}, ids)...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, runErr := cmd.Output()

	var containers []Container
	if err := json.Unmarshal(out, &containers); err != nil && runErr == nil {
		return nil, fmt.Errorf("decode podman inspect: %w", err)
	}
	if runErr != nil {
		return containers, fmt.Errorf("inspect %s: %w: %s", strings.Join(ids, " "), runErr, strings.TrimSpace(stderr.String()))
	}
	return containers, nil
}

// Find returns the container of containers whose ID starts with id, since
// `podman ps` prints short IDs where inspect prints full ones.
func Find(containers []Container, id string) (Container, bool) {
	i := slices.IndexFunc(containers, func(c Container) bool { return strings.HasPrefix(c.ID, id) })
	if i < 0 {
		return Container{}, false
	}
	return containers[i], true
}

// HealthStatus is the container's healthcheck word, or HealthNone without a
// healthcheck.
func (c Container) HealthStatus() string {
	if c.State.Health == nil {
		return HealthNone
	}
	return c.State.Health.Status
}

// Ready reports whether something can depend on the container: it runs with a
// healthy healthcheck or none, or it exited 0 as a one-shot init or migration.
func (c Container) Ready() bool {
	switch c.State.Status {
	case StateRunning:
		health := c.HealthStatus()
		return health == HealthNone || health == HealthHealthy
	case StateExited:
		return c.State.ExitCode == 0
	default:
		return false
	}
}

// Failed reports whether the container exited with a non-zero code.
func (c Container) Failed() bool {
	return c.State.Status == StateExited && c.State.ExitCode != 0
}

// HostPorts maps each container-side port, such as "5432", to the host port
// its first binding publishes. A port without a numeric binding is left out.
func (c Container) HostPorts() map[string]int {
	out := make(map[string]int)
	for containerPort, bindings := range c.NetworkSettings.Ports {
		if len(bindings) == 0 {
			continue
		}
		port, _, _ := strings.Cut(containerPort, "/") // "5432/tcp"
		if host, err := strconv.Atoi(bindings[0].HostPort); err == nil {
			out[port] = host
		}
	}
	return out
}
