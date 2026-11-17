package dashboard

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/a-novel-kit/stack/cli/internal/batchview"
	"github.com/a-novel-kit/stack/cli/internal/shared/enumword"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// The dashboard's own surfaces, drawn in the palette batchview shares with
// every screen of the CLI.
var (
	styleFrame  = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(batchview.Muted.GetForeground())
	styleNav    = lipgloss.NewStyle().Padding(0, 1)
	styleFooter = batchview.Muted.Italic(true)
	styleCmd    = batchview.Accent.Bold(true)
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
	case viewHelp, viewTopology:
		return m.renderPage()
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
// With no action to report, it notes the stack's discovery problems, if any.
func (m *model) renderStatus(width int) string {
	if m.status.level == statusIdle || m.status.text == "" {
		if m.discoveryProblems > 0 {
			note := fmt.Sprintf("!  %d discovery problem(s): run `a-novel core status` to read them", m.discoveryProblems)
			return batchview.Warn.Render(batchview.Truncate(note, width-1))
		}
		return batchview.Muted.Render(strings.Repeat("─", width))
	}
	var prefix string
	var style lipgloss.Style
	switch m.status.level {
	case statusBusy:
		prefix = "⏳ "
		style = batchview.Warn
	case statusInfo:
		prefix = batchview.GlyphOK + "  "
		style = batchview.OK
	case statusError:
		prefix = batchview.GlyphFail + "  "
		style = batchview.Err
	}
	return style.Render(batchview.Truncate(prefix+m.status.text, width-1))
}

// renderNav draws the service navigator: one block per service, or the reason
// the list is empty.
func (m *model) renderNav(width, height int) string {
	if len(m.services) == 0 {
		return styleFrame.Width(width).Height(height).Render(batchview.Muted.Render(m.navEmptyText(width)))
	}

	blocks := make([]string, 0, len(m.services))
	for i, svc := range m.services {
		blocks = append(blocks, navBlock(svc, i == m.selectedSvc))
	}
	content := batchview.Gold.Render("Services") + "\n\n" + strings.Join(blocks, "\n\n")
	return styleFrame.Width(width).Height(height).Render(styleNav.Render(content))
}

// navEmptyText says why the service list is empty: the last refresh failed,
// the first refresh has not landed, or the stack has no services.
func (m *model) navEmptyText(width int) string {
	switch {
	case m.err != nil:
		return "Error refreshing services:\n  " + batchview.Truncate(m.err.Error(), width-4)
	case !m.loaded:
		return "Loading services..."
	default:
		return "(no services discovered)\nCheck that " + batchview.Truncate("$A_NOVEL_STACKS app/ dirs exist", width-4)
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
		nameLine = batchview.Brand.Render(batchview.GlyphCursor + " " + name)
	case countRunning(svc) == 0 && !serviceHasError(svc):
		nameLine = "  " + batchview.Muted.Render(name)
	}
	return nameLine + "\n    " + serviceTargetsLine(svc) + "\n    " + serviceInfraLine(svc)
}

// renderRight draws the selected service's pane: its tab rows, a divider, the
// selected tab's header and its logs.
func (m *model) renderRight(width, height int) string {
	if m.tabCount() == 0 {
		return styleFrame.Width(width).Height(height).Render(batchview.Muted.Render("(this service has no targets or infra)"))
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
		rows = append(rows, batchview.Muted.Render("infra ")+strings.Join(tabs, "  "))
	}
	if targets := svc.GetTargets(); len(targets) > 0 {
		tabs := make([]string, 0, len(targets))
		offset := len(svc.GetInfra())
		for i, t := range targets {
			tabs = append(tabs, renderTab(targetStatusDot(t), "["+t.GetName()+"]", offset+i == m.selectedTab))
		}
		rows = append(rows, batchview.Muted.Render("target ")+strings.Join(tabs, "  "))
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
		return batchview.Gold.Render(t.GetName()+" · "+enumword.Word(t.GetMode())+" · ") +
			renderPhase(t.GetPhase()) +
			batchview.Gold.Render(tail)
	}
	if in := m.activeInfra(); in != nil {
		health := infraHealthLabel(in)
		if health != "" {
			health = " " + health
		}
		return batchview.Gold.Render(in.GetName()+" · ") + batchview.Muted.Render("infra") + batchview.Gold.Render(" · ") +
			renderPhase(in.GetPhase()) +
			batchview.Muted.Render(health) +
			batchview.Gold.Render(containerSuffix(in.GetContainerId()))
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
	style := batchview.Muted
	if selected {
		style = batchview.Brand
	}
	return dot + " " + style.Render(label)
}

// targetStatusDot returns the colored status glyph for one target. It branches
// on TargetKind so a finished one-shot shows its outcome (✓ success, ✗
// failure) and keeps the dim ○ for "never started":
//
//	long-runner running        → ● green
//	long-runner starting       → ● yellow
//	long-runner failed         → ● crit   (error or crash)
//	long-runner stopped        → ○ dim    (exited 0 or killed)
//	long-runner not started    → ○ dim
//
//	one-shot running           → ● yellow (in progress)
//	one-shot completed (ok)    → ✓ green
//	one-shot failed            → ✗ crit
//	one-shot killed            → ○ dim
//	one-shot not started       → ○ dim
func targetStatusDot(t *anovelv1.Target) string {
	isOneShot := t.GetKind() == anovelv1.TargetKind_TARGET_KIND_ONE_SHOT
	switch t.GetPhase() {
	case anovelv1.Phase_PHASE_RUNNING:
		if isOneShot {
			return batchview.Warn.Render("●")
		}
		return batchview.OK.Render("●")
	case anovelv1.Phase_PHASE_STARTING:
		return batchview.Warn.Render("●")
	case anovelv1.Phase_PHASE_TERMINATED:
		switch {
		case targetFailed(t) && isOneShot:
			return batchview.Crit.Render(batchview.GlyphFail)
		case targetFailed(t):
			return batchview.Crit.Render("●")
		case isOneShot && t.GetExitReason() == anovelv1.ExitReason_EXIT_REASON_SUCCESS:
			return batchview.OK.Render(batchview.GlyphOK)
		}
	}
	return batchview.Muted.Render("○")
}

// targetFailed reports whether t terminated on a failure of its own: a
// non-zero exit or a crash. A killed target was stopped on request, and reads
// as stopped, as in `run ps`.
func targetFailed(t *anovelv1.Target) bool {
	reason := t.GetExitReason()
	return t.GetPhase() == anovelv1.Phase_PHASE_TERMINATED &&
		(reason == anovelv1.ExitReason_EXIT_REASON_ERROR || reason == anovelv1.ExitReason_EXIT_REASON_CRASHED)
}

const (
	// logPrefixWidth is the width of a log row's "15:04:05 out " prefix.
	logPrefixWidth = 13
	// tabSpaces is what Lip Gloss renders a tab as by default.
	tabSpaces = "    "
)

// renderLogs draws the window of the followed log that fits in width by height
// cells, ending logScroll lines before the tail. While scrolled back, its last
// row is the paused indicator.
func (m *model) renderLogs(width, height int) string {
	if len(m.logLines) == 0 {
		return batchview.Muted.Render("(no log lines yet: start the target or run :refresh)")
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
		// Truncate measures a tab as zero cells, and the frame renders it as
		// tabSpaces. Tabs are expanded first, so Truncate counts their cells.
		line := strings.ReplaceAll(ln.GetLine(), "\t", tabSpaces)
		rows = append(rows, batchview.Muted.Render(ts)+" "+batchview.Warn.Render(tag)+" "+batchview.Truncate(line, width-logPrefixWidth))
	}
	if m.logScroll > 0 {
		indicator := fmt.Sprintf("⏸ paused: %d line(s) below cursor  ·  pgdn/end resumes tail",
			m.logScroll)
		rows = append(rows, batchview.Warn.Render(batchview.Truncate(indicator, width)))
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

// helpLines is the help page, listing every command, grouped by category.
var helpLines = []string{
	batchview.Gold.Render("a-novel run ui: command reference"),
	"",
	batchview.Gold.Render("Navigation"),
	"  ↑/↓ or j/k           select service",
	"  ←/→ or h/l or Tab   select target tab",
	"",
	batchview.Gold.Render("Log pane"),
	"  PgUp/PgDn             scroll a half page (auto-follow pauses on PgUp)",
	"  Ctrl-↑/Ctrl-↓        step one line",
	"  Home / g              jump to oldest buffered line",
	"  End  / G              jump to tail and resume auto-follow",
	batchview.Muted.Render("  (terminal text selection works: click + drag to copy lines)"),
	"",
	batchview.Gold.Render("Tab kinds"),
	"  [target]               daemon-supervised process (go-exec or container)",
	"  {infra}                podman container (postgres, mailserver, ...)",
	"",
	batchview.Gold.Render("Daemon-backed actions (via Esc command palette)"),
	"  :start                 start active target (default go-exec), targets only",
	"  :start container       start active target in container mode",
	"  :kill                  kill active tab (target → SIGTERM; infra → podman stop)",
	"  :restart               kill then start (target) / podman restart (infra)",
	"  :infra-start           bring up active service's WHOLE infra + one-shots",
	"  :infra-kill            tear down active service's WHOLE infra (refuses if targets up)",
	"  :infra-kill force      cascade-kill targets + infra",
	"  :volume-backup         snapshot active service's volumes",
	"  :topology              show the dependency graph (↑/↓ scroll, any other key returns)",
	"  :refresh               refresh services + logs now",
	"",
	batchview.Gold.Render("UI"),
	"  ?                      this help screen (↑/↓ scroll, any other key returns)",
	"  Esc                    open command palette",
	"  q or Ctrl-C            quit (daemon keeps running)",
	"",
	batchview.Gold.Render("Status bar (above the footer)"),
	"  " + batchview.Warn.Render("⏳") + "  busy    action in flight; persists until it resolves",
	"  " + batchview.OK.Render(batchview.GlyphOK) + "  done    last action succeeded; fades after 5s",
	"  " + batchview.Err.Render(batchview.GlyphFail) + "  error   last action failed or invalid; sticks until the next action",
	"  " + batchview.Warn.Render("!") + "  issues  discovery found problems in the stack; `a-novel core status` lists them",
	"",
	batchview.Muted.Render("(daemon-backed verbs are implemented via the same RPC as the CLI; what you see in the UI is what `a-novel run ps` would show)"),
}

// pageLines returns the lines of the full-screen page in view: the help, or
// the dependency graph from the last :topology.
func (m *model) pageLines() []string {
	if m.view != viewTopology {
		return helpLines
	}
	lines := append([]string{batchview.Gold.Render("Dependency topology"), ""}, strings.Split(m.topologyTx, "\n")...)
	return append(lines, "", batchview.Muted.Render("(↑/↓ scroll, any other key returns)"))
}

// pageWindow returns the page in view, the index of its first line shown, and
// how many lines the frame shows. A page taller than the frame gives up the
// frame's last row to its scroll position, and pageScroll is clamped to the
// page.
func (m *model) pageWindow() ([]string, int, int) {
	lines := m.pageLines()
	rows := m.height - 2 // inside the border
	if len(lines) > rows {
		rows--
	}
	rows = max(rows, 1)
	return lines, min(m.pageScroll, max(len(lines)-rows, 0)), rows
}

// renderPage draws the help or topology page in a frame that fills the
// terminal. Each line is cut to the frame's width, so the frame keeps its
// height. A page taller than the frame ends on its scroll position.
func (m *model) renderPage() string {
	lines, from, rows := m.pageWindow()
	shown := make([]string, 0, rows+1)
	for _, line := range lines[from:min(from+rows, len(lines))] {
		shown = append(shown, batchview.Truncate(line, m.width-2))
	}
	if len(lines) > rows {
		position := fmt.Sprintf("lines %d–%d of %d  ·  ↑/↓ scroll  ·  any other key returns", from+1, from+rows, len(lines))
		shown = append(shown, batchview.Muted.Render(batchview.Truncate(position, m.width-2)))
	}
	return styleFrame.Width(m.width).Height(m.height).Render(strings.Join(shown, "\n"))
}

// overlayCommand draws the command input and autocomplete suggestions
// over the bottom-most lines of the main view: as the user types, it
// shows matching commands with one-line descriptions.
func overlayCommand(main, input string) string {
	// The input sits on the bottom line with any suggestions stacked above
	// it, so it stays anchored where the user is typing.
	overlay := append(suggestCommands(strings.TrimPrefix(input, ":")), styleCmd.Render(input)+batchview.Muted.Render("█"))
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
	{"topology", "show dependency graph (↑/↓ scroll, any other key returns)"},
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
				styleCmd.Render(":"+c.verb)+"  "+batchview.Muted.Render(c.desc))
			if len(matches) == 5 {
				break
			}
		}
	}
	return matches
}

// countRunning counts the service's targets in the running phase.
func countRunning(svc *anovelv1.Service) int {
	var n int
	for _, t := range svc.GetTargets() {
		if t.GetPhase() == anovelv1.Phase_PHASE_RUNNING {
			n++
		}
	}
	return n
}

// serviceTargetsLine is the nav's targets row. A failed target turns it into a
// crit "● errored". Otherwise it shows running out of total targets, behind a
// green dot when one runs and a dim one when none does.
func serviceTargetsLine(svc *anovelv1.Service) string {
	if serviceHasError(svc) {
		return batchview.Crit.Render("●") + " errored"
	}
	running := countRunning(svc)
	total := len(svc.GetTargets())
	dot := batchview.Muted.Render("○")
	if running > 0 {
		dot = batchview.OK.Render("●")
	}
	return dot + " " + strconv.Itoa(running) + "/" + strconv.Itoa(total) + " targets"
}

// serviceInfraLine is the per-service "X/Y infra" row in the nav block. It is
// rendered even at zero, so the panel stays rectangular and the absence of
// infra is visible.
func serviceInfraLine(svc *anovelv1.Service) string {
	healthy, total := countInfraHealthy(svc)
	if total == 0 {
		return batchview.Muted.Render("  (no infra)")
	}
	dot := batchview.Muted.Render("○")
	if healthy > 0 {
		dot = batchview.OK.Render("●")
	}
	return dot + " " + strconv.Itoa(healthy) + "/" + strconv.Itoa(total) + " infra"
}

// countInfraHealthy returns (healthy, total) across a service's infra entries.
// A healthy infra is running and reports HEALTH_HEALTHY or no health at all,
// HEALTH_UNSPECIFIED, which the daemon's infra scan leaves unread. Any other
// health counts as not ready.
func countInfraHealthy(svc *anovelv1.Service) (int, int) {
	total := len(svc.GetInfra())
	var healthy int
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
		return batchview.Muted.Render("○")
	}
	switch in.GetHealth() {
	case anovelv1.Health_HEALTH_HEALTHY, anovelv1.Health_HEALTH_UNSPECIFIED:
		return batchview.OK.Render("●")
	case anovelv1.Health_HEALTH_STARTING:
		return batchview.Warn.Render("●")
	case anovelv1.Health_HEALTH_UNHEALTHY:
		return batchview.Crit.Render("●")
	}
	return batchview.Muted.Render("○")
}

// infraHealthLabel returns the health word that pairs with infraDot, "down"
// for an infra that is not running, or "" when the daemon reads no health for
// a running infra: the header's phase already says "running".
func infraHealthLabel(in *anovelv1.Infra) string {
	switch {
	case in.GetPhase() != anovelv1.Phase_PHASE_RUNNING:
		return "down"
	case in.GetHealth() == anovelv1.Health_HEALTH_UNSPECIFIED:
		return ""
	}
	return enumword.Word(in.GetHealth())
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
// serviceTargetsLine renders the crit "● errored" line from it, and the nav
// keeps such a service undimmed since it needs attention.
func serviceHasError(svc *anovelv1.Service) bool {
	return slices.ContainsFunc(svc.GetTargets(), targetFailed)
}

// renderPhase renders a phase's word for the detail header: in its own color
// when running, stopping or terminated, in the header style otherwise.
func renderPhase(p anovelv1.Phase) string {
	style := batchview.Gold
	switch p {
	case anovelv1.Phase_PHASE_RUNNING:
		style = batchview.OK
	case anovelv1.Phase_PHASE_STOPPING:
		style = batchview.Warn
	case anovelv1.Phase_PHASE_TERMINATED:
		style = batchview.Muted
	}
	return style.Render(enumword.Word(p))
}

// safeShort returns the first n bytes of an ASCII identifier such as a
// container ID, or all of it when shorter.
func safeShort(s string, n int) string {
	return s[:min(n, len(s))]
}
