package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Lip Gloss styles for every surface, kept together so a theme change has a
// single switch.
var (
	styleFrame    = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))
	styleNav      = lipgloss.NewStyle().Padding(0, 1)
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleHeader   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	styleFooter   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	styleErr      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleSuccess  = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	styleWarn     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleCmd      = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
)

// View is Bubble Tea's render entry point. Bubble Tea v2 makes the alternate
// screen a property of the View, so every frame opts in here.
func (m *model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

// render builds the screen content string for the active view.
func (m *model) render() string {
	if m.width == 0 {
		return "loading..."
	}
	switch m.view {
	case viewHelp:
		return m.renderHelp()
	case viewTopology:
		return m.renderTopology()
	}
	// Main layout (with command-palette overlay if in viewCommand).
	main := m.renderMain()
	if m.view == viewCommand {
		main = overlayCommand(main, m.cmdInput)
	}
	return main
}

// renderMain is the two-column layout: services nav left, target detail
// + log viewer right. Bottom two rows are the status bar + footer. A Lip
// Gloss frame's Width and Height include its border, and the two frames split
// the terminal width.
func (m *model) renderMain() string {
	navWidth := computeNavWidth(m.services)
	mainWidth := m.width - navWidth
	if mainWidth < 20 {
		mainWidth = m.width
		navWidth = 0
	}
	nav := m.renderNav(navWidth, m.contentHeight())
	right := m.renderRight(mainWidth, m.contentHeight())

	var body string
	if navWidth > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, nav, right)
	} else {
		body = right
	}
	status := m.renderStatus(m.width)
	footer := m.renderFooter()
	return lipgloss.JoinVertical(lipgloss.Left, body, status, footer)
}

// renderStatus is the always-visible feedback line for action progress,
// results, errors and validation warnings. It stays one line and truncates
// with an ellipsis, since a wrapped status line breaks the layout below it.
func (m *model) renderStatus(width int) string {
	if m.status.level == statusIdle || m.status.text == "" {
		return styleDim.Render(strings.Repeat("─", width))
	}
	var prefix string
	var style lipgloss.Style
	switch m.status.level {
	case statusBusy:
		prefix = "⏳ "
		style = styleWarn
	case statusInfo:
		prefix = "✓  "
		style = styleSuccess
	case statusError:
		prefix = "✗  "
		style = styleErr
	}
	return style.Render(truncate(prefix+m.status.text, width-1))
}

// renderNav draws the service navigator: one block per service, or the reason
// the list is empty.
func (m *model) renderNav(width, height int) string {
	if len(m.services) == 0 {
		return styleFrame.Width(width).Height(height).Render(styleDim.Render(m.navEmptyText(width)))
	}

	blocks := make([]string, 0, len(m.services))
	for i, svc := range m.services {
		blocks = append(blocks, navBlock(svc, i == m.selectedSvc))
	}
	content := styleHeader.Render("Services") + "\n\n" + strings.Join(blocks, "\n\n")
	return styleFrame.Width(width).Height(height).Render(styleNav.Render(content))
}

// navEmptyText says why the service list is empty: the last refresh failed,
// the first refresh has not landed, or the stack has no services.
func (m *model) navEmptyText(width int) string {
	switch {
	case m.err != nil:
		return "Error refreshing services:\n  " + truncate(m.err.Error(), width-4)
	case !m.loaded:
		return "Loading services..."
	default:
		return "(no services discovered)\nCheck that " + truncate("$A_NOVEL_STACKS app/ dirs exist", width-4)
	}
}

// navBlock is one service in the nav: its name on a line of its own, then one
// status row per kind, each a dot and a running/total count, so neither wraps
// on a narrow nav. The rows sit two columns inside the name. The selected name
// is highlighted, and an idle service without errors is dimmed.
func navBlock(svc *anovelv1.Service, selected bool) string {
	name := svc.GetName()
	nameLine := "  " + name
	switch {
	case selected:
		nameLine = styleSelected.Render("▸ " + name)
	case countRunning(svc) == 0 && !serviceHasError(svc):
		nameLine = "  " + styleDim.Render(name)
	}
	return nameLine + "\n    " + serviceTargetsLine(svc) + "\n    " + serviceInfraLine(svc)
}

// renderRight draws the selected service's pane: its tab rows, a divider, the
// selected tab's header and its logs.
func (m *model) renderRight(width, height int) string {
	if m.tabCount() == 0 {
		return styleFrame.Width(width).Height(height).Render(styleDim.Render("(this service has no targets or infra)"))
	}
	inner := width - 2 // inside the border
	sections := append(m.tabRows(),
		strings.Repeat("─", inner),
		m.detailHeader(),
		m.renderLogs(inner, m.logViewportHeight()),
	)
	return styleFrame.Width(width).Height(height).Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
}

// tabRows renders the infra tab row above the target tab row. A kind with no
// entries gets no row. selectedTab walks infras then targets, so ←/→ flows
// across both rows.
func (m *model) tabRows() []string {
	svc := m.activeService()
	var rows []string
	if infras := svc.GetInfra(); len(infras) > 0 {
		tabs := make([]string, 0, len(infras))
		for i, in := range infras {
			tabs = append(tabs, renderTab(infraDot(in), "{"+in.GetName()+"}", i == m.selectedTab))
		}
		rows = append(rows, styleDim.Render("infra ")+strings.Join(tabs, "  "))
	}
	if targets := svc.GetTargets(); len(targets) > 0 {
		tabs := make([]string, 0, len(targets))
		offset := len(svc.GetInfra())
		for i, t := range targets {
			tabs = append(tabs, renderTab(targetStatusDot(t), "["+t.GetName()+"]", offset+i == m.selectedTab))
		}
		rows = append(rows, styleDim.Render("target ")+strings.Join(tabs, "  "))
	}
	return rows
}

// detailHeader summarizes the selected tab. A target reads
// "name · mode · phase [pid] [container]", and an infra
// "name · infra · phase [health] [container]".
//
// Each run of text takes its own Render. A Render ends with a reset, which
// clears any style around it.
func (m *model) detailHeader() string {
	if t := m.activeTarget(); t != nil {
		tail := containerSuffix(t.GetContainerId())
		if t.GetPid() != 0 {
			tail = fmt.Sprintf(" pid=%d", t.GetPid()) + tail
		}
		return styleHeader.Render(t.GetName()+" · "+modeShort(t.GetMode())+" · ") +
			phaseShort(t.GetPhase()) +
			styleHeader.Render(tail)
	}
	if in := m.activeInfra(); in != nil {
		health := infraHealthLabel(in)
		if health != "" {
			health = " " + health
		}
		return styleHeader.Render(in.GetName()+" · ") + styleDim.Render("infra") + styleHeader.Render(" · ") +
			phaseShort(in.GetPhase()) +
			styleDim.Render(health) +
			styleHeader.Render(containerSuffix(in.GetContainerId()))
	}
	return ""
}

// containerSuffix is the " container=<short id>" header field, or "" without a
// container.
func containerSuffix(id string) string {
	if id == "" {
		return ""
	}
	return " container=" + safeShort(id, 12)
}

// renderTab renders one tab of the right pane's tab rows: its status dot,
// then its bracketed label, highlighted when selected.
func renderTab(dot, label string, selected bool) string {
	style := styleDim
	if selected {
		style = styleSelected
	}
	return dot + " " + style.Render(label)
}

// targetStatusDot returns the colored status glyph for one target. It branches
// on TargetKind so a finished one-shot shows its outcome (✓ success, ✗
// failure) and keeps the dim ○ for "never started":
//
//	long-runner running        → ● green
//	long-runner starting       → ● yellow
//	long-runner failed         → ● red    (error or crash)
//	long-runner stopped        → ○ dim    (exited 0 or killed)
//	long-runner not started    → ○ dim
//
//	one-shot running           → ● yellow (in progress)
//	one-shot completed (ok)    → ✓ green
//	one-shot failed            → ✗ red
//	one-shot killed            → ○ dim
//	one-shot not started       → ○ dim
func targetStatusDot(t *anovelv1.Target) string {
	isOneShot := t.GetKind() == anovelv1.TargetKind_TARGET_KIND_ONE_SHOT
	switch t.GetPhase() {
	case anovelv1.Phase_PHASE_RUNNING:
		if isOneShot {
			return styleWarn.Render("●")
		}
		return styleSuccess.Render("●")
	case anovelv1.Phase_PHASE_STARTING:
		return styleWarn.Render("●")
	case anovelv1.Phase_PHASE_TERMINATED:
		switch {
		case targetFailed(t) && isOneShot:
			return styleErr.Render("✗")
		case targetFailed(t):
			return styleErr.Render("●")
		case isOneShot && t.GetExitReason() == anovelv1.ExitReason_EXIT_REASON_SUCCESS:
			return styleSuccess.Render("✓")
		}
	}
	return styleDim.Render("○")
}

// targetFailed reports whether t terminated on a failure of its own: a
// non-zero exit or a crash. A killed target was stopped on request, and reads
// as stopped, as in `run ps`.
func targetFailed(t *anovelv1.Target) bool {
	reason := t.GetExitReason()
	return t.GetPhase() == anovelv1.Phase_PHASE_TERMINATED &&
		(reason == anovelv1.ExitReason_EXIT_REASON_ERROR || reason == anovelv1.ExitReason_EXIT_REASON_CRASHED)
}

// logPrefixWidth is the width of a log row's "15:04:05 out " prefix.
const logPrefixWidth = 13

// renderLogs draws the window of the followed log that fits in width by height
// cells, ending logScroll lines before the tail. While scrolled back, its last
// row is the paused indicator.
func (m *model) renderLogs(width, height int) string {
	if len(m.logLines) == 0 {
		return styleDim.Render("(no log lines yet — start the target or run :refresh)")
	}
	// Away from the tail, the bottom line of the pane holds the scroll
	// indicator; at the tail the full height goes to log content.
	contentH := height
	if m.logScroll > 0 {
		contentH = max(height-1, 1)
	}
	// The window into logLines ends logScroll lines before the tail.
	end := max(len(m.logLines)-m.logScroll, 0)
	from := max(end-contentH, 0)
	rows := make([]string, 0, end-from+1)
	for _, ln := range m.logLines[from:end] {
		tag := "out"
		if ln.GetStream() == anovelv1.LogStream_LOG_STREAM_STDERR {
			tag = "err"
		}
		ts := ln.GetTs().AsTime().Format("15:04:05")
		rows = append(rows, styleDim.Render(ts)+" "+styleWarn.Render(tag)+" "+truncate(ln.GetLine(), width-logPrefixWidth))
	}
	if m.logScroll > 0 {
		indicator := fmt.Sprintf("⏸ paused — %d line(s) below cursor  ·  pgdn/end resumes tail",
			m.logScroll)
		rows = append(rows, styleWarn.Render(truncate(indicator, width)))
	}
	// The rows end without a newline. A trailing one adds a row, which grows
	// the frame past its height.
	return strings.Join(rows, "\n")
}

// renderFooter draws the one-line key hint under the status bar. While the log
// pane is scrolled back, it offers End to resume the tail. Keys and text take
// separate Renders, because each Render ends with a reset that clears the
// style around it.
func (m *model) renderFooter() string {
	scrollHint := footerHint("PgUp/PgDn", "scroll logs")
	if m.logScroll > 0 {
		scrollHint = footerHint("End", "resume tail")
	}
	hints := []string{
		footerHint("?", "help"),
		footerHint("Esc", "command"),
		footerHint("↑↓", "service"),
		footerHint("←→", "target"),
		scrollHint,
		footerHint("q", "quit"),
	}
	return strings.Join(hints, styleFooter.Render("  ·  "))
}

// footerHint renders one footer hint: the key, then what it does.
func footerHint(key, text string) string {
	return styleCmd.Render(key) + styleFooter.Render(" "+text)
}

// renderHelp draws the full-screen help listing every command, grouped
// by category.
func (m *model) renderHelp() string {
	lines := []string{
		styleHeader.Render("a-novel run ui  —  command reference"),
		"",
		styleHeader.Render("Navigation"),
		"  ↑/↓ or j/k           select service",
		"  ←/→ or h/l or Tab   select target tab",
		"",
		styleHeader.Render("Log pane"),
		"  PgUp/PgDn             scroll a half page (auto-follow pauses on PgUp)",
		"  Ctrl-↑/Ctrl-↓        step one line",
		"  Home / g              jump to oldest buffered line",
		"  End  / G              jump to tail and resume auto-follow",
		styleDim.Render("  (terminal text selection works — click + drag to copy lines)"),
		"",
		styleHeader.Render("Tab kinds"),
		"  [target]               daemon-supervised process (go-exec or container)",
		"  {infra}                podman container (postgres, mailserver, ...)",
		"",
		styleHeader.Render("Daemon-backed actions (via Esc command palette)"),
		"  :start                 start active target (default go-exec) — targets only",
		"  :start container       start active target in container mode",
		"  :kill                  kill active tab (target → SIGTERM; infra → podman stop)",
		"  :restart               kill then start (target) / podman restart (infra)",
		"  :infra-start           bring up active service's WHOLE infra + one-shots",
		"  :infra-kill            tear down active service's WHOLE infra (refuses if targets up)",
		"  :infra-kill force      cascade-kill targets + infra",
		"  :volume-backup         snapshot active service's volumes",
		"  :topology              show the dependency graph (any key returns)",
		"  :refresh               refresh services + logs now",
		"",
		styleHeader.Render("UI"),
		"  ?                      this help screen (any key to dismiss)",
		"  Esc                    open command palette",
		"  q or Ctrl-C            quit (daemon keeps running)",
		"",
		styleHeader.Render("Status bar (above the footer)"),
		"  " + styleWarn.Render("⏳") + "  busy   — action in flight; persists until it resolves",
		"  " + styleSuccess.Render("✓") + "  done   — last action succeeded; fades after 5s",
		"  " + styleErr.Render("✗") + "  error  — last action failed or invalid; sticks until the next action",
		"",
		styleDim.Render("(daemon-backed verbs are implemented via the same RPC as the CLI; what you see in the UI is what `a-novel run ps` would show)"),
	}
	return styleFrame.Width(m.width).Height(m.height).Render(strings.Join(lines, "\n"))
}

// renderTopology is the dedicated topology screen, populated by the
// :topology palette command's response. Any keystroke returns to
// viewMain.
func (m *model) renderTopology() string {
	lines := []string{
		styleHeader.Render("Dependency topology"),
		"",
		m.topologyTx,
		"",
		styleDim.Render("(press any key to return)"),
	}
	return styleFrame.Width(m.width).Height(m.height).Render(strings.Join(lines, "\n"))
}

// overlayCommand draws the command input and autocomplete suggestions
// over the bottom-most lines of the main view: as the user types, it
// shows matching commands with one-line descriptions.
func overlayCommand(main, input string) string {
	// The input sits on the bottom line with any suggestions stacked above
	// it, so it stays anchored where the user is typing.
	overlay := append(suggestCommands(strings.TrimPrefix(input, ":")), styleCmd.Render(input)+styleDim.Render("█"))
	// Drop as many bottom lines as the overlay needs, then append it.
	lines := strings.Split(main, "\n")
	lines = append(lines[:len(lines)-min(len(overlay), len(lines))], overlay...)
	return strings.Join(lines, "\n")
}

// paletteCommands is the list the autocomplete suggester draws from, each
// entry a verb and its one-line description.
var paletteCommands = []struct{ verb, desc string }{
	{"start", "start the active target (default go-exec)"},
	{"start container", "start the active target in container mode"},
	{"kill", "kill the active target (graceful 10s SIGTERM)"},
	{"restart", "kill then start the active target"},
	{"infra-start", "bring up active service's infra + auto-run one-shots"},
	{"infra-kill", "tear down active service's infra (refuses if targets up)"},
	{"infra-kill force", "cascade-kill targets + infra"},
	{"volume-backup", "snapshot active service's volumes"},
	{"topology", "show dependency graph (any key returns)"},
	{"refresh", "force-refresh services + logs"},
	{"quit", "exit the UI (daemon keeps running)"},
}

// suggestCommands returns, as styled lines, up to five palette commands whose
// verb starts with prefix, in table order.
func suggestCommands(prefix string) []string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil
	}
	var matches []string
	for _, c := range paletteCommands {
		if strings.HasPrefix(c.verb, prefix) {
			matches = append(matches,
				styleCmd.Render(":"+c.verb)+"  "+styleDim.Render(c.desc))
			if len(matches) == 5 {
				break
			}
		}
	}
	return matches
}

// countRunning counts the service's targets in the running phase.
func countRunning(svc *anovelv1.Service) int {
	n := 0
	for _, t := range svc.GetTargets() {
		if t.GetPhase() == anovelv1.Phase_PHASE_RUNNING {
			n++
		}
	}
	return n
}

// serviceTargetsLine is the nav's targets row. A failed target turns it into a
// red "● errored". Otherwise it shows running out of total targets, behind a
// green dot when one runs and a dim one when none does.
func serviceTargetsLine(svc *anovelv1.Service) string {
	if serviceHasError(svc) {
		return styleErr.Render("●") + " errored"
	}
	running := countRunning(svc)
	total := len(svc.GetTargets())
	dot := styleDim.Render("○")
	if running > 0 {
		dot = styleSuccess.Render("●")
	}
	return dot + fmt.Sprintf(" %d/%d targets", running, total)
}

// serviceInfraLine is the per-service "X/Y infra" row in the nav block. It is
// rendered even at zero, so the panel stays rectangular and the absence of
// infra is visible.
func serviceInfraLine(svc *anovelv1.Service) string {
	healthy, total := countInfraHealthy(svc)
	if total == 0 {
		return styleDim.Render("  (no infra)")
	}
	dot := styleDim.Render("○")
	if healthy > 0 {
		dot = styleSuccess.Render("●")
	}
	return dot + fmt.Sprintf(" %d/%d infra", healthy, total)
}

// countInfraHealthy returns (healthy, total) across a service's infra entries.
// A healthy infra is running and reports HEALTH_HEALTHY or no health at all,
// HEALTH_UNSPECIFIED, which the daemon's infra scan leaves unread. Any other
// health counts as not ready.
func countInfraHealthy(svc *anovelv1.Service) (int, int) {
	total := len(svc.GetInfra())
	healthy := 0
	for _, in := range svc.GetInfra() {
		if in.GetPhase() != anovelv1.Phase_PHASE_RUNNING {
			continue
		}
		h := in.GetHealth()
		if h == anovelv1.Health_HEALTH_HEALTHY || h == anovelv1.Health_HEALTH_UNSPECIFIED {
			healthy++
		}
	}
	return healthy, total
}

// infraDot returns the colored status glyph for one infra entry: a dim ○ when
// it is not running or reports an unknown health, else a dot colored by
// health.
func infraDot(in *anovelv1.Infra) string {
	if in.GetPhase() != anovelv1.Phase_PHASE_RUNNING {
		return styleDim.Render("○")
	}
	switch in.GetHealth() {
	case anovelv1.Health_HEALTH_HEALTHY, anovelv1.Health_HEALTH_UNSPECIFIED:
		return styleSuccess.Render("●")
	case anovelv1.Health_HEALTH_STARTING:
		return styleWarn.Render("●")
	case anovelv1.Health_HEALTH_UNHEALTHY:
		return styleErr.Render("●")
	}
	return styleDim.Render("○")
}

// infraHealthLabel returns the short human descriptor that pairs
// with infraDot ("healthy", "starting", "unhealthy", "down"), or "" when the
// daemon reads no health for a running infra: the header's phase already says
// "running".
func infraHealthLabel(in *anovelv1.Infra) string {
	if in.GetPhase() != anovelv1.Phase_PHASE_RUNNING {
		return "down"
	}
	switch in.GetHealth() {
	case anovelv1.Health_HEALTH_HEALTHY:
		return "healthy"
	case anovelv1.Health_HEALTH_UNSPECIFIED:
		return ""
	case anovelv1.Health_HEALTH_STARTING:
		return "starting"
	case anovelv1.Health_HEALTH_UNHEALTHY:
		return "unhealthy"
	}
	return "unknown"
}

// computeNavWidth picks the sidebar's frame width so the longest service name
// fits on one line, clamped to [24, 40]: 24 keeps the panel a recognizable
// width when nothing is discovered yet, 40 stops one very long name squeezing
// the right pane.
func computeNavWidth(svcs []*anovelv1.Service) int {
	const minW, maxW = 24, 40
	w := minW
	for _, svc := range svcs {
		// Two border columns, styleNav's padding column on each side, then the
		// "▸ " prefix and the name.
		w = max(w, 6+len(svc.GetName()))
	}
	return min(w, maxW)
}

// serviceHasError reports whether any target in the service failed.
// serviceTargetsLine renders the red "● errored" line from it, and the nav
// keeps such a service undimmed since it needs attention.
func serviceHasError(svc *anovelv1.Service) bool {
	return slices.ContainsFunc(svc.GetTargets(), targetFailed)
}

func modeShort(m anovelv1.Mode) string {
	switch m {
	case anovelv1.Mode_MODE_GO_EXEC:
		return modeGoExec
	case anovelv1.Mode_MODE_CONTAINER:
		return modeContainer
	default:
		return "-"
	}
}

// phaseShort renders a phase for the detail header: in its own color when
// running, stopping or terminated, in the header style otherwise.
func phaseShort(p anovelv1.Phase) string {
	switch p {
	case anovelv1.Phase_PHASE_PENDING:
		return styleHeader.Render("pending")
	case anovelv1.Phase_PHASE_STARTING:
		return styleHeader.Render("starting")
	case anovelv1.Phase_PHASE_RUNNING:
		return styleSuccess.Render("running")
	case anovelv1.Phase_PHASE_STOPPING:
		return styleWarn.Render("stopping")
	case anovelv1.Phase_PHASE_TERMINATED:
		return styleDim.Render("terminated")
	default:
		return styleHeader.Render("idle")
	}
}

// truncate fits s into maxLen terminal cells, ending it with an ellipsis
// when cut. It never splits a multi-byte character or an ANSI escape
// sequence. A non-positive maxLen leaves s whole.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	return ansi.Truncate(s, maxLen, "…")
}

// safeShort returns the first n bytes of an ASCII identifier such as a
// container ID, or all of it when shorter.
func safeShort(s string, n int) string {
	return s[:min(n, len(s))]
}
