package cli

import (
	"slices"
	"testing"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestCoalesce pins how `ps --watch` paints a burst: the events that arrive
// while a paint runs reach a single next paint, as the newest of them.
func TestCoalesce(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		// events are handed over in order. The first starts a paint, and the
		// rest arrive while that paint runs.
		events []string
		want   []string
	}{
		{name: "Success/NoEvent"},
		{name: "Success/OneEvent", events: []string{"a"}, want: []string{"a"}},
		{name: "Success/Burst", events: []string{"a", "b", "c", "d"}, want: []string{"a", "d"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			painting, release := make(chan struct{}), make(chan struct{})
			var painted []string
			handle, wait := coalesce(func(ev *anovelv1.WatchResponse) {
				painted = append(painted, ev.GetDescription())
				if len(painted) == 1 {
					close(painting)
					<-release
				}
			})

			for i, description := range testCase.events {
				handle(&anovelv1.WatchResponse{Description: description})
				if i == 0 {
					<-painting
				}
			}
			close(release)
			wait()

			if !slices.Equal(painted, testCase.want) {
				t.Errorf("painted %q, want %q", painted, testCase.want)
			}
		})
	}
}
