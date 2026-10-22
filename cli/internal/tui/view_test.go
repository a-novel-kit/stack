package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func TestTruncate(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		s      string
		maxLen int

		want string
	}{
		{name: "Fits", s: "abc", maxLen: 5, want: "abc"},
		{name: "CutASCII", s: "abcdef", maxLen: 4, want: "abc…"},
		{name: "CutKeepsMultiByteRunes", s: "a—b—c—d", maxLen: 4, want: "a—b…"},
		{name: "CutCountsWideCells", s: "日本語テキスト", maxLen: 5, want: "日本…"},
		{name: "CutKeepsANSISequences", s: "\x1b[31mabcdef\x1b[m", maxLen: 4, want: "\x1b[31mabc…\x1b[m"},
		{name: "NonPositiveKeepsWhole", s: "abcdef", maxLen: 0, want: "abcdef"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := truncate(testCase.s, testCase.maxLen); got != testCase.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", testCase.s, testCase.maxLen, got, testCase.want)
			}
		})
	}
}

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

		view      view
		logLines  []*anovelv1.StreamLogsResponse
		logScroll int

		wantText string
	}{
		{name: "Main/Tail", logLines: logLines, wantText: "▸ " + service.GetName()},
		{name: "Main/ScrolledBack", logLines: logLines, logScroll: 10, wantText: "⏸ paused"},
		{name: "Main/NoLogs", wantText: "no log lines yet"},
		{name: "Help", view: viewHelp, wantText: "command reference"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			m := &model{
				width:     120,
				height:    50,
				view:      testCase.view,
				services:  []*anovelv1.Service{service},
				logLines:  testCase.logLines,
				logScroll: testCase.logScroll,
			}
			screen := m.render()

			// The top row is frame border only. JoinVertical pads it with
			// spaces to the widest row, so the trimmed row measures the frames.
			top, _, _ := strings.Cut(ansi.Strip(screen), "\n")
			width, height := lipgloss.Width(strings.TrimRight(top, " ")), lipgloss.Height(screen)
			if width != m.width || height != m.height {
				t.Errorf("screen is %d wide and %d high, want %d and %d", width, height, m.width, m.height)
			}
			if !strings.Contains(screen, testCase.wantText) {
				t.Errorf("screen lacks %q on one line", testCase.wantText)
			}
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
			name: "Target",
			service: &anovelv1.Service{Targets: []*anovelv1.Target{{
				Name:  "grpc",
				Mode:  anovelv1.Mode_MODE_GO_EXEC,
				Phase: anovelv1.Phase_PHASE_RUNNING,
				Pid:   4242,
			}}},
			want: "grpc · go-exec · running pid=4242",
		},
		{
			name: "Target/Container",
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
			name: "Infra/NoHealth",
			service: &anovelv1.Service{Infra: []*anovelv1.Infra{{
				Name:        "postgres",
				Phase:       anovelv1.Phase_PHASE_RUNNING,
				ContainerId: "0123456789abcdef",
			}}},
			want: "postgres · infra · running container=0123456789ab",
		},
		{
			name: "Infra/Healthy",
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

			if got := ansi.Strip(header); got != testCase.want {
				t.Errorf("detailHeader() reads %q, want %q", got, testCase.want)
			}
			if unstyled := unstyledText(t, header); unstyled != "" {
				t.Errorf("detailHeader() leaves %q unstyled", unstyled)
			}
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
		{name: "Tail", want: "? help  ·  Esc command  ·  ↑↓ service  ·  ←→ target  ·  PgUp/PgDn scroll logs  ·  q quit"},
		{name: "ScrolledBack", logScroll: 3, want: "? help  ·  Esc command  ·  ↑↓ service  ·  ←→ target  ·  End resume tail  ·  q quit"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			footer := (&model{logScroll: testCase.logScroll}).renderFooter()

			if got := ansi.Strip(footer); got != testCase.want {
				t.Errorf("renderFooter() reads %q, want %q", got, testCase.want)
			}
			if unstyled := unstyledText(t, footer); unstyled != "" {
				t.Errorf("renderFooter() leaves %q unstyled", unstyled)
			}
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
		{name: "LongRunner/Exited", reason: anovelv1.ExitReason_EXIT_REASON_SUCCESS, want: styleDim.Render("○")},
		{name: "LongRunner/Killed", reason: anovelv1.ExitReason_EXIT_REASON_KILLED, want: styleDim.Render("○")},
		{name: "LongRunner/Error", reason: anovelv1.ExitReason_EXIT_REASON_ERROR, want: styleErr.Render("●")},
		{name: "LongRunner/Crashed", reason: anovelv1.ExitReason_EXIT_REASON_CRASHED, want: styleErr.Render("●")},
		{
			name:   "OneShot/Succeeded",
			kind:   anovelv1.TargetKind_TARGET_KIND_ONE_SHOT,
			reason: anovelv1.ExitReason_EXIT_REASON_SUCCESS,
			want:   styleSuccess.Render("✓"),
		},
		{
			name:   "OneShot/Killed",
			kind:   anovelv1.TargetKind_TARGET_KIND_ONE_SHOT,
			reason: anovelv1.ExitReason_EXIT_REASON_KILLED,
			want:   styleDim.Render("○"),
		},
		{
			name:   "OneShot/Error",
			kind:   anovelv1.TargetKind_TARGET_KIND_ONE_SHOT,
			reason: anovelv1.ExitReason_EXIT_REASON_ERROR,
			want:   styleErr.Render("✗"),
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
			if got := targetStatusDot(target); got != testCase.want {
				t.Errorf("targetStatusDot() = %q, want %q", got, testCase.want)
			}
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
		{name: "Running", phase: anovelv1.Phase_PHASE_RUNNING},
		{name: "Exited", phase: anovelv1.Phase_PHASE_TERMINATED, reason: anovelv1.ExitReason_EXIT_REASON_SUCCESS},
		// A requested stop, as `run ps` reads it.
		{name: "Killed", phase: anovelv1.Phase_PHASE_TERMINATED, reason: anovelv1.ExitReason_EXIT_REASON_KILLED},
		{name: "Error", phase: anovelv1.Phase_PHASE_TERMINATED, reason: anovelv1.ExitReason_EXIT_REASON_ERROR, want: true},
		{
			name:   "Crashed",
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
			if got := serviceHasError(service); got != testCase.want {
				t.Errorf("serviceHasError() = %t, want %t", got, testCase.want)
			}
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
