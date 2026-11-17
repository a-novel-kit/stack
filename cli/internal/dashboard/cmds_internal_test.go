package dashboard

import (
	"testing"

	"github.com/stretchr/testify/require"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func TestRunPaletteCommand(t *testing.T) {
	t.Parallel()

	// The follower replays the log's last lines, so :refresh must restart the
	// buffer instead of appending a second copy of them.
	t.Run("Success/Refresh", func(t *testing.T) {
		t.Parallel()

		m := &model{
			services: []*anovelv1.Service{{
				Name:    "svc",
				Targets: []*anovelv1.Target{{Id: "default/svc/grpc", Name: "grpc"}},
			}},
			logLines:  []*anovelv1.StreamLogsResponse{{Line: "one"}, {Line: "two"}},
			logScroll: 1,
		}
		gen := m.followGen

		require.NotNil(t, m.runPaletteCommand(":refresh"))
		require.Empty(t, m.logLines)
		require.Zero(t, m.logScroll)
		require.NotEqual(t, gen, m.followGen, "an unchanged followGen keeps late lines from the old follower")
	})
}
