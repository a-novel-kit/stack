package tui

import (
	"testing"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func TestRunPaletteCommand(t *testing.T) {
	t.Parallel()

	// The follower replays the whole log, so :refresh must restart the buffer
	// instead of appending a second copy of the history.
	t.Run("Refresh/RestartsLogBuffer", func(t *testing.T) {
		t.Parallel()

		m := &model{
			services: []*anovelv1.Service{{
				Name:    "svc",
				Targets: []*anovelv1.Target{{Id: "default/svc/grpc", Name: "grpc"}},
			}},
			logLines:  []*anovelv1.LogLine{{Line: "one"}, {Line: "two"}},
			logScroll: 1,
		}
		gen := m.followGen

		if cmd := m.runPaletteCommand(":refresh"); cmd == nil {
			t.Fatal("runPaletteCommand(:refresh) returned no command")
		}
		if len(m.logLines) != 0 || m.logScroll != 0 {
			t.Errorf("after :refresh, logLines = %d, logScroll = %d; want 0, 0", len(m.logLines), m.logScroll)
		}
		if m.followGen == gen {
			t.Error("after :refresh, followGen is unchanged; late lines from the old follower would be kept")
		}
	})
}
