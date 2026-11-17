package runner

import (
	"testing"

	"github.com/stretchr/testify/require"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestContainerLabelArgs pins that the flag setting the adoption labels and the
// filters reading them spell them alike: a restarted daemon finds its
// containers through them.
func TestContainerLabelArgs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		values []string

		want string
	}{
		{
			name:   "Success",
			values: []string{"default", "service-x", "rest"},
			want:   "--podman-run-args=--label anovel.stack=default --label anovel.service=service-x --label anovel.target=rest",
		},
		// Infra containers carry no target label.
		{
			name:   "Success/Infra",
			values: []string{"default", "service-x"},
			want:   "--podman-run-args=--label anovel.stack=default --label anovel.service=service-x",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, containerLabelArgs(testCase.values...))
		})
	}
}

func TestLabelFilters(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		[]string{"--filter", "label=anovel.stack=default", "--filter", "label=anovel.service=service-x"},
		labelFilters("default", "service-x"),
	)
}

func TestPodmanPhase(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		state string

		want anovelv1.Phase
	}{
		{name: "Success/Running", state: "running", want: anovelv1.Phase_PHASE_RUNNING},
		{name: "Success/PausedAnyCase", state: "Paused", want: anovelv1.Phase_PHASE_RUNNING},
		{name: "Success/Created", state: "created", want: anovelv1.Phase_PHASE_STARTING},
		{name: "Success/Configured", state: "configured", want: anovelv1.Phase_PHASE_STARTING},
		{name: "Success/Stopping", state: "stopping", want: anovelv1.Phase_PHASE_STOPPING},
		{name: "Success/Exited", state: "exited", want: anovelv1.Phase_PHASE_TERMINATED},
		{name: "Success/Stopped", state: "stopped", want: anovelv1.Phase_PHASE_TERMINATED},
		{name: "Success/UnknownWord", state: "removing", want: anovelv1.Phase_PHASE_UNSPECIFIED},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, podmanPhase(testCase.state))
		})
	}
}
