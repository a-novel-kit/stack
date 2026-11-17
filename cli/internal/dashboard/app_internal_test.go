package dashboard

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

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
		{name: "Success/FirstList", services: []*anovelv1.Service{serviceB}, wantFollow: true},
		{name: "Success/SameList", current: []*anovelv1.Service{serviceB}, services: []*anovelv1.Service{serviceB}},
		// A service listed first moves another one under the selection.
		{
			name:       "Success/ListShift",
			current:    []*anovelv1.Service{serviceB},
			services:   []*anovelv1.Service{serviceA, serviceB},
			wantFollow: true,
		},
		{name: "Success/NoTab", services: []*anovelv1.Service{{Name: "service-c"}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			m := &model{services: testCase.current}
			m.onServices(testCase.services)

			require.Equal(t, testCase.wantFollow, m.followGen != 0, "onServices restarted the log follower")
		})
	}
}

func TestHandleKey(t *testing.T) {
	t.Parallel()

	down := tea.KeyPressMsg{Code: tea.KeyDown}
	up := tea.KeyPressMsg{Code: tea.KeyUp}
	// A 24-row terminal shows 21 help lines above the scroll position.
	lastFrom := len(helpLines) - 21

	keys := func(key tea.KeyPressMsg, n int) []tea.KeyPressMsg {
		presses := make([]tea.KeyPressMsg, n)
		for i := range presses {
			presses[i] = key
		}
		return presses
	}

	testCases := []struct {
		name string

		keys []tea.KeyPressMsg

		wantView view
		wantFrom int
	}{
		{name: "Success/Help/ScrollDown", keys: keys(down, 3), wantView: viewHelp, wantFrom: 3},
		{name: "Success/Help/ScrollStopsAtEnd", keys: keys(down, 100), wantView: viewHelp, wantFrom: lastFrom},
		// Up answers at once after scrolling past the end.
		{name: "Success/Help/ScrollUpFromEnd", keys: append(keys(down, 100), up), wantView: viewHelp, wantFrom: lastFrom - 1},
		{name: "Success/Help/ScrollStopsAtTop", keys: keys(up, 2), wantView: viewHelp},
		{name: "Success/Help/OtherKeyReturns", keys: []tea.KeyPressMsg{{Code: 'x', Text: "x"}}, wantView: viewMain},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			m := &model{width: 120, height: 24, view: viewHelp}
			for _, key := range testCase.keys {
				m.handleKey(key)
			}

			require.Equal(t, testCase.wantView, m.view)
			_, from, _ := m.pageWindow()
			require.Equal(t, testCase.wantFrom, from, "first line the page shows")
		})
	}
}
