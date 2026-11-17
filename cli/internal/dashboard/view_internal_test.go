package dashboard

import (
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/batchview"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func TestRender(t *testing.T) {
	t.Parallel()

	service := &anovelv1.Service{
		Name:    "service-narrative-engine",
		Infra:   []*anovelv1.Infra{{Name: "postgres", Phase: anovelv1.Phase_PHASE_RUNNING}},
		Targets: []*anovelv1.Target{{Id: "default/service-narrative-engine/rest", Name: "rest"}},
	}
	// More lines than the pane holds, each with tabs and longer than the pane.
	logLines := make([]*anovelv1.StreamLogsResponse, 60)
	for i := range logLines {
		logLines[i] = &anovelv1.StreamLogsResponse{Line: "info\tbatch synced\tshard=2\tline=7\tkeys=" + strings.Repeat("k", 200)}
	}

	testCases := []struct {
		name string

		view              view
		height            int
		logLines          []*anovelv1.StreamLogsResponse
		logScroll         int
		discoveryProblems int

		wantText string
	}{
		{name: "Success/Main/Tail", height: 50, logLines: logLines, wantText: "▸ " + service.GetName()},
		{name: "Success/Main/ScrolledBack", height: 50, logLines: logLines, logScroll: 10, wantText: "⏸ paused"},
		{name: "Success/Main/NoLogs", height: 50, wantText: "no log lines yet"},
		{name: "Success/Main/DiscoveryProblems", height: 50, discoveryProblems: 2, wantText: "2 discovery problem(s)"},
		{name: "Success/Help", view: viewHelp, height: 50, wantText: "command reference"},
		// The help is taller than the terminal, so it scrolls in place.
		{name: "Success/Help/Overflow", view: viewHelp, height: 24, wantText: "↑/↓ scroll"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			m := &model{
				width:             120,
				height:            testCase.height,
				view:              testCase.view,
				services:          []*anovelv1.Service{service},
				logLines:          testCase.logLines,
				logScroll:         testCase.logScroll,
				discoveryProblems: testCase.discoveryProblems,
			}
			screen := m.render()

			// The top row is frame border only. JoinVertical pads it with
			// spaces to the widest row, so the trimmed row measures the frames.
			top, _, _ := strings.Cut(ansi.Strip(screen), "\n")
			width, height := lipgloss.Width(strings.TrimRight(top, " ")), lipgloss.Height(screen)
			require.Equal(t, m.width, width, "screen width")
			require.Equal(t, m.height, height, "screen height")
			require.Contains(t, screen, testCase.wantText)
		})
	}
}

func TestDetailHeader(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		service *anovelv1.Service

		want string
	}{
		{
			name: "Success/Target",
			service: &anovelv1.Service{Targets: []*anovelv1.Target{{
				Name:  "grpc",
				Mode:  anovelv1.Mode_MODE_GO_EXEC,
				Phase: anovelv1.Phase_PHASE_RUNNING,
				Pid:   4242,
			}}},
			want: "grpc · go-exec · running pid=4242",
		},
		{
			name: "Success/Target/Container",
			service: &anovelv1.Service{Targets: []*anovelv1.Target{{
				Name:        "grpc",
				Mode:        anovelv1.Mode_MODE_CONTAINER,
				Phase:       anovelv1.Phase_PHASE_STARTING,
				ContainerId: "0123456789abcdef",
			}}},
			want: "grpc · container · starting container=0123456789ab",
		},
		// The daemon reads no infra health, so the phase alone describes it.
		{
			name: "Success/Infra/NoHealth",
			service: &anovelv1.Service{Infra: []*anovelv1.Infra{{
				Name:        "postgres",
				Phase:       anovelv1.Phase_PHASE_RUNNING,
				ContainerId: "0123456789abcdef",
			}}},
			want: "postgres · infra · running container=0123456789ab",
		},
		{
			name: "Success/Infra/Healthy",
			service: &anovelv1.Service{Infra: []*anovelv1.Infra{{
				Name:   "postgres",
				Phase:  anovelv1.Phase_PHASE_RUNNING,
				Health: anovelv1.Health_HEALTH_HEALTHY,
			}}},
			want: "postgres · infra · running healthy",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			m := &model{services: []*anovelv1.Service{testCase.service}}
			header := m.detailHeader()

			require.Equal(t, testCase.want, ansi.Strip(header))
			require.Empty(t, unstyledText(t, header), "unstyled text")
		})
	}
}

func TestRenderFooter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		logScroll int

		want string
	}{
		{name: "Success/Tail", want: "? help  ·  Esc command  ·  ↑↓ service  ·  ←→ target  ·  PgUp/PgDn scroll logs  ·  q quit"},
		{name: "Success/ScrolledBack", logScroll: 3, want: "? help  ·  Esc command  ·  ↑↓ service  ·  ←→ target  ·  End resume tail  ·  q quit"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			footer := (&model{logScroll: testCase.logScroll}).renderFooter()

			require.Equal(t, testCase.want, ansi.Strip(footer))
			require.Empty(t, unstyledText(t, footer), "unstyled text")
		})
	}
}

func TestTargetStatusDot(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		kind   anovelv1.TargetKind
		reason anovelv1.ExitReason

		want string
	}{
		{name: "Success/LongRunner/Exited", reason: anovelv1.ExitReason_EXIT_REASON_SUCCESS, want: batchview.Muted.Render("○")},
		{name: "Success/LongRunner/Killed", reason: anovelv1.ExitReason_EXIT_REASON_KILLED, want: batchview.Muted.Render("○")},
		{name: "Success/LongRunner/Error", reason: anovelv1.ExitReason_EXIT_REASON_ERROR, want: batchview.Crit.Render("●")},
		{name: "Success/LongRunner/Crashed", reason: anovelv1.ExitReason_EXIT_REASON_CRASHED, want: batchview.Crit.Render("●")},
		{
			name:   "Success/OneShot/Succeeded",
			kind:   anovelv1.TargetKind_TARGET_KIND_ONE_SHOT,
			reason: anovelv1.ExitReason_EXIT_REASON_SUCCESS,
			want:   batchview.OK.Render(batchview.GlyphOK),
		},
		{
			name:   "Success/OneShot/Killed",
			kind:   anovelv1.TargetKind_TARGET_KIND_ONE_SHOT,
			reason: anovelv1.ExitReason_EXIT_REASON_KILLED,
			want:   batchview.Muted.Render("○"),
		},
		{
			name:   "Success/OneShot/Error",
			kind:   anovelv1.TargetKind_TARGET_KIND_ONE_SHOT,
			reason: anovelv1.ExitReason_EXIT_REASON_ERROR,
			want:   batchview.Crit.Render(batchview.GlyphFail),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			target := &anovelv1.Target{
				Kind:       testCase.kind,
				Phase:      anovelv1.Phase_PHASE_TERMINATED,
				ExitReason: testCase.reason,
			}
			require.Equal(t, testCase.want, targetStatusDot(target))
		})
	}
}

func TestServiceHasError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		phase  anovelv1.Phase
		reason anovelv1.ExitReason

		want bool
	}{
		{name: "Success/Running", phase: anovelv1.Phase_PHASE_RUNNING},
		{name: "Success/Exited", phase: anovelv1.Phase_PHASE_TERMINATED, reason: anovelv1.ExitReason_EXIT_REASON_SUCCESS},
		// A requested stop, as `run ps` reads it.
		{name: "Success/Killed", phase: anovelv1.Phase_PHASE_TERMINATED, reason: anovelv1.ExitReason_EXIT_REASON_KILLED},
		{name: "Success/Error", phase: anovelv1.Phase_PHASE_TERMINATED, reason: anovelv1.ExitReason_EXIT_REASON_ERROR, want: true},
		{
			name:   "Success/Crashed",
			phase:  anovelv1.Phase_PHASE_TERMINATED,
			reason: anovelv1.ExitReason_EXIT_REASON_CRASHED,
			want:   true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := &anovelv1.Service{Targets: []*anovelv1.Target{
				{Phase: anovelv1.Phase_PHASE_RUNNING},
				{Phase: testCase.phase, ExitReason: testCase.reason},
			}}
			require.Equal(t, testCase.want, serviceHasError(service))
		})
	}
}

// unstyledText returns the text of s that no SGR style covers: text before the
// first style, or after a reset. It reads every escape sequence in s as SGR.
func unstyledText(t *testing.T, s string) string {
	t.Helper()

	var unstyled strings.Builder
	styled := false
	for s != "" {
		if strings.HasPrefix(s, "\x1b[") {
			end := strings.IndexByte(s, 'm') + 1
			styled = s[:end] != "\x1b[m" && s[:end] != "\x1b[0m"
			s = s[end:]
			continue
		}
		r, size := utf8.DecodeRuneInString(s)
		if !styled {
			unstyled.WriteRune(r)
		}
		s = s[size:]
	}
	return unstyled.String()
}
