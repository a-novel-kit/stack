package tui

import (
	"testing"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func TestOnServices(t *testing.T) {
	t.Parallel()

	serviceA := &anovelv1.Service{
		Name:    "service-a",
		Targets: []*anovelv1.Target{{Id: "default/service-a/rest", Name: "rest"}},
	}
	serviceB := &anovelv1.Service{
		Name:    "service-b",
		Targets: []*anovelv1.Target{{Id: "default/service-b/rest", Name: "rest"}},
	}

	testCases := []struct {
		name string

		current  []*anovelv1.Service
		services []*anovelv1.Service

		wantFollow bool
	}{
		// The first list selects a tab, so the pane follows its log at launch.
		{name: "FirstList", services: []*anovelv1.Service{serviceB}, wantFollow: true},
		{name: "SameList", current: []*anovelv1.Service{serviceB}, services: []*anovelv1.Service{serviceB}},
		// A service listed first moves another one under the selection.
		{
			name:       "ListShift",
			current:    []*anovelv1.Service{serviceB},
			services:   []*anovelv1.Service{serviceA, serviceB},
			wantFollow: true,
		},
		{name: "NoTab", services: []*anovelv1.Service{{Name: "service-c"}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			m := &model{services: testCase.current}
			m.onServices(testCase.services)

			if followed := m.followGen != 0; followed != testCase.wantFollow {
				t.Errorf("onServices restarted the log follower: %t, want %t", followed, testCase.wantFollow)
			}
		})
	}
}
