package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Styles — Lip Gloss surfaces, kept in one place so future polish
// (themes, color palettes) has a single switch.
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

// View is Bubble Tea's render entry point.
func (m *model) View() string {
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
// + log viewer right. Bottom two rows are the status bar + footer.
func (m *model) renderMain() string {
	navWidth := computeNavWidth(m.services)
	mainWidth := m.width - navWidth - 4 // borders
	if mainWidth < 20 {
		mainWidth = m.width
		navWidth = 0
	}
	// Reserve 1 line for status bar + 1 line for footer hint.
	contentHeight := m.height - 4

	nav := m.renderNav(navWidth, contentHeight)
	right := m.renderRight(mainWidth, contentHeight)

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

// renderStatus is the always-visible feedback line: shows action
// progress, results, errors, and validation warnings. Single line so
// it doesn't fight for vertical space with the log viewer; truncates
// long messages with an ellipsis instead of wrapping (a wrapped
// status line plays havoc with the layout below it).
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
	msg := truncate(m.status.text, width-len(prefix)-1)
	return style.Render(prefix + msg)
}

func (m *model) renderNav(width, height int) string {
	// Three "empty" states deserve different messages so the user can
	// tell loading from a hard failure from a genuinely-empty stack:
	//   - !m.loaded            → refresh hasn't completed (or always errors)
	//   - m.err != nil         → last refresh failed; surface the reason
	//   - len(m.services) == 0 → refresh succeeded, stack has no services
	if len(m.services) == 0 {
		var msg string
		switch {
		case m.err != nil:
			msg = "Error refreshing services:\n  " + truncate(m.err.Error(), width-4)
		case !m.loaded:
			msg = "Loading services..."
		default:
			msg = "(no services discovered)\nCheck that " + truncate("$A_NOVEL_STACKS app/ dirs exist", width-4)
		}
		return styleFrame.Width(width).Height(height).Render(styleDim.Render(msg))
	}
	// Vertical per-service block: one line for the name (full width so
	// it doesn't wrap), one indented status line below it, blank line
	// between blocks for breathing room. This trades vertical density
	// for horizontal clarity — service names like
	// "service-authentication" no longer collide with the right-side
	// counts on a narrow nav.
	var lines []string
	lines = append(lines, styleHeader.Render("Services"))
	lines = append(lines, "")
	for i, svc := range m.services {
		name := svc.GetName()
		hasError := serviceHasError(svc)
		inactive := countRunning(svc) == 0 && !hasError
		// Name line — selected wins over inactive dimming (selection
		// is the more important signal).
		var nameLine string
		switch {
		case i == m.selectedSvc:
			nameLine = styleSelected.Render("▸ " + name)
		case inactive:
			nameLine = "  " + styleDim.Render(name)
		default:
			nameLine = "  " + name
		}
		// Two status lines per service — one for targets, one for
		// infra — each on its own row so neither wraps when the nav
		// is narrow. The dot per row summarizes that kind's
		// running/total count. Indented by 4 so the dots align
		// visually with the first letter of the name above.
		targetLine := "    " + serviceTargetsLine(svc)
		infraLine := "    " + serviceInfraLine(svc)
		lines = append(lines, nameLine, targetLine, infraLine)
		if i != len(m.services)-1 {
			lines = append(lines, "")
		}
	}
	content := strings.Join(lines, "\n")
	return styleFrame.Width(width).Height(height).Render(styleNav.Render(content))
}

func (m *model) renderRight(width, height int) string {
	svc := m.activeService()
	if svc == nil || m.tabCount() == 0 {
		return styleFrame.Width(width).Height(height).Render(styleDim.Render("(this service has no targets or infra)"))
	}
	// Two selectable tab rows — infra on top, targets below. Each
	// tab carries a leading colored status dot (no full-word
	// labels). selectedTab walks the concatenated
	// [infras..., targets...] sequence so ←/→ flows across both
	// rows linearly. Either row is omitted entirely when its slice
	// is empty so a service with only targets isn't shadowed by an
	// empty infra line.
	infraTabsRow := renderInfraTabs(svc, m.selectedTab)
	targetTabsRow := renderTargetTabs(svc, m.selectedTab, len(svc.GetInfra()))
	// Detail header — branches on kind. Targets keep the existing
	// "name · mode · phase [pid] [container]" shape; infras get
	// "name · infra · phase healthy container=xxx".
	var header string
	switch m.activeTabKind() {
	case tabKindTarget:
		t := m.activeTarget()
		header = fmt.Sprintf("%s · %s · %s",
			t.GetName(), modeShort(t.GetMode()), phaseShort(t.GetPhase()))
		if t.GetPid() != 0 {
			header += fmt.Sprintf(" pid=%d", t.GetPid())
		}
		if t.GetContainerId() != "" {
			header += " container=" + safeShort(t.GetContainerId(), 12)
		}
	case tabKindInfra:
		in := m.activeInfra()
		header = fmt.Sprintf("%s · %s · %s %s",
			in.GetName(),
			styleDim.Render("infra"),
			phaseShort(in.GetPhase()),
			styleDim.Render(infraHealthLabel(in)))
		if in.GetContainerId() != "" {
			header += " container=" + safeShort(in.GetContainerId(), 12)
		}
	}
	headerStyled := styleHeader.Render(header)
	// Layout: [infra-row] [target-row] [divider] [header] [logs].
	// Each absent row simply skipped from the JoinVertical input.
	sections := []string{}
	rowCount := 0
	if infraTabsRow != "" {
		sections = append(sections, infraTabsRow)
		rowCount++
	}
	if targetTabsRow != "" {
		sections = append(sections, targetTabsRow)
		rowCount++
	}
	// Log pane shrinks by (rowCount-1) so adding the second tab row
	// doesn't push log lines off the bottom (the original layout
	// reserved space for 1 tab row).
	logsHeight := height - 5 - (rowCount - 1)
	if logsHeight < 4 {
		logsHeight = 4
	}
	logsRendered := m.renderLogs(width-4, logsHeight)
	sections = append(sections,
		strings.Repeat("─", width-4),
		headerStyled,
		logsRendered,
	)
	body := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return styleFrame.Width(width).Height(height).Render(body)
}

// renderInfraTabs renders the top tab row: one tab per infra
// container, with a leading colored status dot and {curly} brackets.
// Returns "" when the service has no infra. selectedTab is the
// model's combined index — when it falls in [0, len(infras)), the
// matching tab gets styleSelected.
func renderInfraTabs(svc *anovelv1.Service, selectedTab int) string {
	infras := svc.GetInfra()
	if len(infras) == 0 {
		return ""
	}
	tabs := make([]string, 0, len(infras))
	for i, in := range infras {
		dot := infraDot(in)
		brackets := "{" + in.GetName() + "}"
		if i == selectedTab {
			tabs = append(tabs, dot+" "+styleSelected.Render(brackets))
		} else {
			tabs = append(tabs, dot+" "+styleDim.Render(brackets))
		}
	}
	return styleDim.Render("infra ") + strings.Join(tabs, "  ")
}

// renderTargetTabs renders the bottom tab row: one tab per target,
// with a leading colored status dot and [square] brackets.
// infraOffset shifts the visual selection index — when the combined
// selectedTab falls in [infraOffset, infraOffset+len(targets)), the
// matching target tab is selected.
func renderTargetTabs(svc *anovelv1.Service, selectedTab, infraOffset int) string {
	targets := svc.GetTargets()
	if len(targets) == 0 {
		return ""
	}
	tabs := make([]string, 0, len(targets))
	for i, t := range targets {
		dot := targetStatusDot(t)
		brackets := "[" + t.GetName() + "]"
		if infraOffset+i == selectedTab {
			tabs = append(tabs, dot+" "+styleSelected.Render(brackets))
		} else {
			tabs = append(tabs, dot+" "+styleDim.Render(brackets))
		}
	}
	return styleDim.Render("target ") + strings.Join(tabs, "  ")
}

// targetStatusDot returns the colored status glyph for one target.
// Branches on TargetKind so one-shots reflect their LAST RUN outcome
// (✓ success / ✗ failure) instead of falling back to a dim ○ that
// would look indistinguishable from "never started":
//
//	long-runner running        → ● green
//	long-runner starting       → ● yellow
//	long-runner errored        → ● red    (terminated non-success)
//	long-runner clean exit     → ○ dim    (killed deliberately)
//	long-runner not started    → ○ dim
//
//	one-shot running           → ● yellow (in progress)
//	one-shot completed (ok)    → ✓ green
//	one-shot completed (fail)  → ✗ red
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
		switch t.GetExitReason() {
		case anovelv1.ExitReason_EXIT_REASON_SUCCESS:
			if isOneShot {
				return styleSuccess.Render("✓")
			}
			return styleDim.Render("○")
		case anovelv1.ExitReason_EXIT_REASON_UNSPECIFIED:
			return styleDim.Render("○")
		default:
			if isOneShot {
				return styleErr.Render("✗")
			}
			return styleErr.Render("●")
		}
	}
	return styleDim.Render("○")
}

func (m *model) renderLogs(width, height int) string {
	if len(m.logLines) == 0 {
		return styleDim.Render("(no log lines yet — start the target or press 'r' to refresh)")
	}
	// Reserve the bottom line of the pane for the scroll indicator
	// when we're not at the tail. When at the tail, the full height
	// is available for log content (no indicator needed since
	// auto-follow is the default state).
	contentH := height
	if m.logScroll > 0 {
		contentH = height - 1
		if contentH < 1 {
			contentH = 1
		}
	}
	// Compute the window into logLines:
	//   logScroll == 0    → tail (the last `contentH` lines)
	//   logScroll == N>0  → window `[len-contentH-N, len-N)`
	end := len(m.logLines) - m.logScroll
	if end < 0 {
		end = 0
	}
	from := end - contentH
	if from < 0 {
		from = 0
	}
	var b strings.Builder
	for _, ln := range m.logLines[from:end] {
		tag := "out"
		if ln.GetStream() == anovelv1.LogStream_LOG_STREAM_STDERR {
			tag = "err"
		}
		ts := ln.GetTs().AsTime().Format("15:04:05")
		fmt.Fprintf(&b, "%s %s %s\n",
			styleDim.Render(ts), styleWarn.Render(tag), truncate(ln.GetLine(), width-20))
	}
	// Scroll indicator — only visible when we've paused auto-follow.
	// Tells the user (a) they're paused, (b) how far up they are, and
	// (c) how to get back. End/G snaps to the tail, the other keys
	// step the window further.
	if m.logScroll > 0 {
		indicator := fmt.Sprintf("⏸ paused — %d line(s) below cursor  ·  pgdn/end resumes tail",
			m.logScroll)
		b.WriteString(styleWarn.Render(truncate(indicator, width)))
	}
	return b.String()
}

// renderFooter is layer-1 of the three-layer palette per spec §14.3:
// always-visible hint of the most-relevant commands for the current
// context. Single-line — action feedback now lives in the dedicated
// status bar above (renderStatus). When the log pane is scrolled
// back, the "scroll" hint adapts to advertise End/G as the way out.
func (m *model) renderFooter() string {
	scrollHint := styleCmd.Render("PgUp/PgDn") + " scroll logs"
	if m.logScroll > 0 {
		scrollHint = styleCmd.Render("End") + " resume tail"
	}
	hints := []string{
		styleCmd.Render("?") + " help",
		styleCmd.Render("Esc") + " command",
		styleCmd.Render("↑↓") + " service",
		styleCmd.Render("←→") + " target",
		scrollHint,
		styleCmd.Render("q") + " quit",
	}
	return styleFooter.Render(strings.Join(hints, "  ·  "))
}

// renderHelp is layer 3 — full-screen help showing every command,
// organized by category. Spec §14.3.
func (m *model) renderHelp() string {
	body := strings.Join([]string{
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
		"  :env                   show env block for active service in stderr",
		"  :volume-backup         snapshot active service's volumes",
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
	}, "\n")
	return styleFrame.Width(m.width - 2).Height(m.height - 2).Render(body)
}

// renderTopology is the dedicated topology screen, populated by the
// :topology palette command's response. Spec §14.4. Any keystroke
// returns to viewMain.
func (m *model) renderTopology() string {
	body := strings.Join([]string{
		styleHeader.Render("Dependency topology"),
		"",
		m.topologyTx,
		"",
		styleDim.Render("(press any key to return)"),
	}, "\n")
	return styleFrame.Width(m.width - 2).Height(m.height - 2).Render(body)
}

// overlayCommand draws the command input + autocomplete suggestions over
// the bottom-most lines of the main view. Spec §14.3 layer 2: as the
// user types, show matching commands with one-line descriptions.
func overlayCommand(main, input string) string {
	cmd := styleCmd.Render(input) + styleDim.Render("█")
	suggestions := suggestCommands(strings.TrimPrefix(input, ":"))
	lines := strings.Split(main, "\n")
	if len(lines) == 0 {
		return cmd
	}
	// Replace the bottom line with the cmd input + any suggestions
	// above it (so the input stays visually anchored at the bottom).
	overlayLines := suggestions
	overlayLines = append(overlayLines, cmd)
	// Splice in: drop as many bottom lines as we need to overlay, then
	// append the overlay block.
	drop := len(overlayLines)
	if drop > len(lines) {
		drop = len(lines)
	}
	lines = lines[:len(lines)-drop]
	lines = append(lines, overlayLines...)
	return strings.Join(lines, "\n")
}

// paletteCommands is the master list shown by the autocomplete suggester.
// Each entry is (verb, one-line description). Kept here (vs cmds.go) so
// the view is the single rendering surface for the palette.
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

// suggestCommands returns the top 5 paletteCommands matching `prefix`
// (prefix-match on verb), formatted as styled lines.
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

func countRunning(svc *anovelv1.Service) int {
	n := 0
	for _, t := range svc.GetTargets() {
		if t.GetPhase() == anovelv1.Phase_PHASE_RUNNING {
			n++
		}
	}
	return n
}

// serviceTargetsLine is the per-service "X/Y targets" row in the nav
// block. Green dot when any target is running, red when any has an
// error exit, dim otherwise. Single line — no infra count here.
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

// serviceInfraLine is the per-service "X/Y infra" row in the nav
// block. Always rendered (even when Y == 0) so the panel layout
// stays rectangular and the user knows whether infra exists.
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

// countInfraHealthy returns (healthy, total) across a service's infra
// entries. A "healthy" infra is one with PHASE_RUNNING AND either
// HEALTH_HEALTHY or no healthcheck declared (HEALTH_UNSPECIFIED on a
// running container = no probe, treat as up). Distinguishes from
// "starting" (HEALTH_STARTING, probe failing) which is NOT healthy.
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

// infraDot returns the colored status glyph for one infra entry,
// matching the four-state model: green ● healthy, yellow ● starting,
// red ● unhealthy, dim ○ down/terminated/idle.
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
// with infraDot ("healthy", "starting", "unhealthy", "down").
func infraHealthLabel(in *anovelv1.Infra) string {
	if in.GetPhase() != anovelv1.Phase_PHASE_RUNNING {
		return "down"
	}
	switch in.GetHealth() {
	case anovelv1.Health_HEALTH_HEALTHY:
		return "healthy"
	case anovelv1.Health_HEALTH_UNSPECIFIED:
		return "running" // no probe declared
	case anovelv1.Health_HEALTH_STARTING:
		return "starting"
	case anovelv1.Health_HEALTH_UNHEALTHY:
		return "unhealthy"
	}
	return "unknown"
}

// computeNavWidth picks the sidebar width so the longest service name
// fits on one line. Clamped to [24, 40]: 24 keeps the panel a
// recognizable width even when nothing's discovered yet; 40 caps it so
// a rogue super-long name doesn't squeeze the right pane.
func computeNavWidth(svcs []*anovelv1.Service) int {
	const minW, maxW = 24, 40
	w := minW
	for _, svc := range svcs {
		// "▸ " prefix + name + 2 cols of right padding.
		needed := 4 + len(svc.GetName())
		if needed > w {
			w = needed
		}
	}
	if w > maxW {
		w = maxW
	}
	return w
}

// serviceHasError reports whether any target in the service terminated
// with a non-success exit. Used both by serviceStatusLine (to render
// the red "● errored" line) and the inactive-row dim logic (an errored
// row shouldn't dim — it needs attention).
func serviceHasError(svc *anovelv1.Service) bool {
	for _, t := range svc.GetTargets() {
		if t.GetPhase() == anovelv1.Phase_PHASE_TERMINATED &&
			t.GetExitReason() != anovelv1.ExitReason_EXIT_REASON_SUCCESS &&
			t.GetExitReason() != anovelv1.ExitReason_EXIT_REASON_UNSPECIFIED {
			return true
		}
	}
	return false
}

func modeShort(m anovelv1.Mode) string {
	switch m {
	case anovelv1.Mode_MODE_GO_EXEC:
		return "go-exec"
	case anovelv1.Mode_MODE_CONTAINER:
		return "container"
	default:
		return "-"
	}
}

func phaseShort(p anovelv1.Phase) string {
	switch p {
	case anovelv1.Phase_PHASE_PENDING:
		return "pending"
	case anovelv1.Phase_PHASE_STARTING:
		return "starting"
	case anovelv1.Phase_PHASE_RUNNING:
		return styleSuccess.Render("running")
	case anovelv1.Phase_PHASE_STOPPING:
		return styleWarn.Render("stopping")
	case anovelv1.Phase_PHASE_TERMINATED:
		return styleDim.Render("terminated")
	default:
		return "idle"
	}
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	if len(s) > maxLen {
		return s[:maxLen-1] + "…"
	}
	return s
}

func safeShort(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
