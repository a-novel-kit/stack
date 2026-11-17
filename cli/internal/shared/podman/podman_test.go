package podman_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/podman"
)

// inspectJSON is a trimmed `podman inspect` print of three containers: a
// healthy service, a one-shot that exited 0, and one without a healthcheck that
// exited 1. It pins the JSON fields Container decodes.
const inspectJSON = `[
  {"Id": "aaaa1111", "Name": "proj_postgres_1",
   "State": {"Status": "running", "ExitCode": 0, "Health": {"Status": "healthy"}},
   "NetworkSettings": {"Ports": {"5432/tcp": [{"HostIp": "0.0.0.0", "HostPort": "38631"}], "9000/tcp": null}}},
  {"Id": "bbbb2222", "Name": "proj_migrations_1",
   "State": {"Status": "exited", "ExitCode": 0},
   "NetworkSettings": {"Ports": {}}},
  {"Id": "cccc3333", "Name": "proj_broken_1",
   "State": {"Status": "exited", "ExitCode": 1},
   "NetworkSettings": {"Ports": {}}}
]`

func decode(t *testing.T) []podman.Container {
	t.Helper()

	var containers []podman.Container
	if err := json.Unmarshal([]byte(inspectJSON), &containers); err != nil {
		panic(err)
	}
	return containers
}

func TestContainer(t *testing.T) {
	t.Parallel()

	containers := decode(t)

	testCases := []struct {
		name       string
		id         string
		wantHealth string
		wantReady  bool
		wantFailed bool
		wantPorts  map[string]int
	}{
		{
			name: "Success/HealthyService", id: "aaaa",
			wantHealth: podman.HealthHealthy, wantReady: true,
			wantPorts: map[string]int{"5432": 38631},
		},
		{
			name: "Success/OneShotDone", id: "bbbb",
			wantHealth: podman.HealthNone, wantReady: true, wantPorts: map[string]int{},
		},
		{
			name: "Error/ExitedNonZero", id: "cccc",
			wantHealth: podman.HealthNone, wantFailed: true, wantPorts: map[string]int{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			c, ok := podman.Find(containers, testCase.id)
			require.True(t, ok)
			require.Equal(t, testCase.wantHealth, c.HealthStatus())
			require.Equal(t, testCase.wantReady, c.Ready())
			require.Equal(t, testCase.wantFailed, c.Failed())
			require.Equal(t, testCase.wantPorts, c.HostPorts())
		})
	}
}

func TestContainerReady(t *testing.T) {
	t.Parallel()

	starting := &struct {
		Status string `json:"Status"`
	}{Status: "starting"}

	testCases := []struct {
		name  string
		state podman.State
		want  bool
	}{
		{name: "Success/RunningWithoutHealthcheck", state: podman.State{Status: podman.StateRunning}, want: true},
		{name: "Success/StartingHealthcheck", state: podman.State{Status: podman.StateRunning, Health: starting}},
		{name: "Success/Created", state: podman.State{Status: "created"}},
		{name: "Success/ExitedZero", state: podman.State{Status: podman.StateExited}, want: true},
		{name: "Success/ExitedNonZero", state: podman.State{Status: podman.StateExited, ExitCode: 2}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, podman.Container{State: testCase.state}.Ready())
		})
	}
}

func TestFind(t *testing.T) {
	t.Parallel()

	containers := decode(t)

	t.Run("Success/ShortID", func(t *testing.T) {
		t.Parallel()

		c, ok := podman.Find(containers, "bbbb")
		require.True(t, ok)
		require.Equal(t, "proj_migrations_1", c.Name)
	})

	t.Run("Error/Missing", func(t *testing.T) {
		t.Parallel()

		_, ok := podman.Find(containers, "dddd")
		require.False(t, ok)
	})
}
