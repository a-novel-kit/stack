package runner

import (
	"slices"
	"testing"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// The adoption labels are how a restarted daemon finds its containers, so the
// flag that sets them and the filters that read them must spell them alike.

func TestContainerLabelArgs(t *testing.T) {
	cases := []struct {
		values []string
		want   string
	}{
		{
			[]string{"default", "service-x", "rest"},
			"--podman-run-args=--label anovel.stack=default --label anovel.service=service-x --label anovel.target=rest",
		},
		// Infra containers carry no target label.
		{[]string{"default", "service-x"}, "--podman-run-args=--label anovel.stack=default --label anovel.service=service-x"},
	}
	for _, c := range cases {
		if got := containerLabelArgs(c.values...); got != c.want {
			t.Errorf("containerLabelArgs(%q):\n got %q\nwant %q", c.values, got, c.want)
		}
	}
}

func TestLabelFilters(t *testing.T) {
	got := labelFilters("default", "service-x")
	want := []string{"--filter", "label=anovel.stack=default", "--filter", "label=anovel.service=service-x"}
	if !slices.Equal(got, want) {
		t.Errorf("labelFilters: got %q, want %q", got, want)
	}
}

func TestPodmanPhase(t *testing.T) {
	cases := []struct {
		state string
		want  anovelv1.Phase
	}{
		{"running", anovelv1.Phase_PHASE_RUNNING},
		{"Paused", anovelv1.Phase_PHASE_RUNNING},
		{"created", anovelv1.Phase_PHASE_STARTING},
		{"configured", anovelv1.Phase_PHASE_STARTING},
		{"stopping", anovelv1.Phase_PHASE_STOPPING},
		{"exited", anovelv1.Phase_PHASE_TERMINATED},
		{"stopped", anovelv1.Phase_PHASE_TERMINATED},
		{"removing", anovelv1.Phase_PHASE_UNSPECIFIED},
	}
	for _, c := range cases {
		if got := podmanPhase(c.state); got != c.want {
			t.Errorf("podmanPhase(%q): got %v, want %v", c.state, got, c.want)
		}
	}
}
