package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/a-novel-kit/stack/cli/internal/runner"
)

// globalMinWidth is the narrowest total width that still gets the vertical
// service nav. Below this the nav is hidden and the log uses the full width.
const globalMinWidth = 100

// RunModel is the live `run` dashboard: a tmux-style tabbed log view plus, in
// global scope, a left-side service nav. Spinning targets and monitoring
// their output is the entire job — there is no interactive console; the user
// asked for the tool to stay simple. Every persistent element is static, so
// an idle frame is byte-identical and Bubble Tea's renderer skips the
// repaint (no flicker).
type RunModel struct {
	version string
	runMode string // "container" / "live" — surfaced in the dashboard header
	run     *runner.Runner
	cancel  func() // cancels the runner (triggers full teardown)

	vp      viewport.Model // active process log
	vpReady bool

	procs []runner.ProcView
	sel   int // active tab

	// Render-on-change: the log viewport is only re-fed when the active tab's
	// content sequence (or the selection) actually changed.
	shownSel int
	shownSeq uint64

	width, height int
	stopping      bool // user asked to quit; waiting for teardown
	finished      bool // runner.Done() fired
	quitting      bool // ready to tea.Quit
}

type runDoneMsg struct{}

// tickMsg drives a slow poll of the runner snapshot. It is deliberately NOT
// an animated spinner: an animating glyph changes View() every frame, and
// Bubble Tea only suppresses a repaint when the frame is byte-identical.
type tickMsg struct{}

// pollInterval is the snapshot cadence — live enough, but an unchanged frame
// is genuinely idle (no repaint).
const pollInterval = 150 * time.Millisecond

func tick() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// NewRun builds the dashboard. cancel must cancel the context the runner was
// started with, so q triggers a full scoped teardown. runMode
// ("container" / "live") is shown in the dashboard header so the active
// mode is unambiguous at a glance.
func NewRun(version, runMode string, r *runner.Runner, cancel func()) RunModel {
	return RunModel{
		version: version, runMode: runMode, run: r, cancel: cancel,
		procs: r.Snapshot(), shownSel: -1,
	}
}

func (m RunModel) Init() tea.Cmd {
	return tea.Batch(tick(), m.waitDone())
}

func (m RunModel) waitDone() tea.Cmd {
	r := m.run
	return func() tea.Msg {
		<-r.Done()
		return runDoneMsg{}
	}
}

// runGeom is the resolved responsive layout. globalMode is true when there
// are ≥2 services to navigate between — a vertical service nav appears on
// the far left and the top tab bar narrows to the active service's
// entrypoints. On narrow terminals it auto-disables.
type runGeom struct {
	globalMode  bool
	navW, leftW int
	bodyH       int
}

func (m RunModel) geom() runGeom {
	w, h := m.width, m.height
	if w <= 0 {
		w = termWidth()
	}
	// header(1) + gap(1) + body + gap(1) + footer(1).
	bodyH := h - 4
	if bodyH < 3 {
		bodyH = 3
	}
	globalMode := false
	navW := 0
	if w >= globalMinWidth {
		if svcs := m.services(); len(svcs) >= 2 {
			maxLen := 0
			for _, s := range svcs {
				if l := len(s) + 4; l > maxLen { // 4 = glyph + spaces
					maxLen = l
				}
			}
			navW = clamp(maxLen, 20, 32)
			globalMode = true
		}
	}
	leftW := w - navW
	if navW > 0 {
		leftW -= 3 // " │ " divider between nav and log
	}
	if leftW < 40 && navW > 0 {
		// Too tight for the nav too — fall back to single-column.
		navW = 0
		globalMode = false
		leftW = w
	}
	return runGeom{globalMode: globalMode, navW: navW, leftW: leftW, bodyH: bodyH}
}

// services returns the unique service names across procs, in first-seen
// order. Stable across ticks because procs is built from the runner's
// selection-time slice.
func (m RunModel) services() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range m.procs {
		if s := p.Target.Service; s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// serviceProcs returns the indices into m.procs belonging to svc, preserving
// runner order.
func (m RunModel) serviceProcs(svc string) []int {
	var out []int
	for i, p := range m.procs {
		if p.Target.Service == svc {
			out = append(out, i)
		}
	}
	return out
}

// currentService is the active tab's service.
func (m RunModel) currentService() string {
	if m.sel < 0 || m.sel >= len(m.procs) {
		return ""
	}
	return m.procs[m.sel].Target.Service
}

// serviceStatus aggregates the procs' statuses into one glyph for the nav:
// Failed dominates Running dominates EnvUp dominates Pending dominates Exited.
func (m RunModel) serviceStatus(svc string) runner.Status {
	best := runner.Exited
	rank := func(s runner.Status) int {
		switch s {
		case runner.Failed:
			return 5
		case runner.Running:
			return 4
		case runner.EnvUp:
			return 3
		case runner.Pending:
			return 2
		case runner.Exited:
			return 1
		}
		return 0
	}
	for _, i := range m.serviceProcs(svc) {
		if rank(m.procs[i].Status) > rank(best) {
			best = m.procs[i].Status
		}
	}
	return best
}

func (m RunModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.applyGeom()
		return m, nil

	case tickMsg:
		m.procs = m.run.Snapshot()
		if m.sel >= len(m.procs) {
			m.sel = max(0, len(m.procs)-1)
		}
		m.refreshLog(false)
		return m, tick()

	case runDoneMsg:
		m.finished = true
		if m.stopping {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// applyGeom (re)sizes the log viewport to the current layout. Called on
// resize; safe to call repeatedly.
func (m *RunModel) applyGeom() {
	g := m.geom()
	logH := max(g.bodyH-3, 1) // tab bar + gap + title above the log
	if !m.vpReady {
		m.vp = viewport.New(g.leftW, logH)
		m.vpReady = true
	} else {
		m.vp.Width, m.vp.Height = g.leftW, logH
	}
	m.refreshLog(true)
}

func (m RunModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	switch k {
	case "ctrl+c", keyQ:
		return m.quitOrTeardown()
	case keyLeft, "h", "shift+tab":
		m.navEntry(-1)
		return m, nil
	case keyRight, "l":
		m.navEntry(+1)
		return m, nil
	}
	if m.geom().globalMode {
		switch k {
		case keyUp, "k":
			m.navService(-1)
			return m, nil
		case keyDown, "j":
			m.navService(+1)
			return m, nil
		}
	}
	if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
		// 1-9 jumps to the Nth entrypoint of the current service (global) or
		// the Nth flat proc (per-repo).
		n := int(k[0] - '1')
		if m.geom().globalMode {
			ix := m.serviceProcs(m.currentService())
			if n < len(ix) {
				m.sel = ix[n]
				m.refreshLog(true)
			}
		} else if n < len(m.procs) {
			m.sel = n
			m.refreshLog(true)
		}
		return m, nil
	}
	// Everything else (↑/↓ in per-repo, pgup/pgdn / home/end / k/j in global,
	// space/etc.) scrolls the log viewport.
	if m.vpReady {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

// navEntry moves selection by delta among the CURRENT service's entrypoints
// in global mode, or among all procs flat in single-service mode. Edges are
// hard stops — no wraparound.
func (m *RunModel) navEntry(delta int) {
	var indices []int
	if m.geom().globalMode {
		indices = m.serviceProcs(m.currentService())
	} else {
		indices = make([]int, len(m.procs))
		for i := range m.procs {
			indices[i] = i
		}
	}
	pos := -1
	for i, ix := range indices {
		if ix == m.sel {
			pos = i
			break
		}
	}
	if pos < 0 {
		return
	}
	pos += delta
	if pos < 0 || pos >= len(indices) {
		return
	}
	m.sel = indices[pos]
	m.refreshLog(true)
}

// navService jumps to the next/prev service's FIRST entrypoint. Only
// meaningful in global mode (≥2 services). No-op at edges.
func (m *RunModel) navService(delta int) {
	svcs := m.services()
	cur := m.currentService()
	pos := -1
	for i, s := range svcs {
		if s == cur {
			pos = i
			break
		}
	}
	if pos < 0 {
		return
	}
	pos += delta
	if pos < 0 || pos >= len(svcs) {
		return
	}
	if ix := m.serviceProcs(svcs[pos]); len(ix) > 0 {
		m.sel = ix[0]
		m.refreshLog(true)
	}
}

func (m RunModel) quitOrTeardown() (tea.Model, tea.Cmd) {
	if m.finished {
		m.quitting = true
		return m, tea.Quit
	}
	if !m.stopping {
		m.stopping = true
		m.cancel()
	}
	return m, nil
}

// refreshLog re-feeds the active tab's log, only when something changed (or
// force). Tail-follows unless the user scrolled up.
func (m *RunModel) refreshLog(force bool) {
	if !m.vpReady || len(m.procs) == 0 {
		return
	}
	if m.sel >= len(m.procs) {
		m.sel = len(m.procs) - 1
	}
	p := m.procs[m.sel]
	if !force && m.sel == m.shownSel && p.Seq == m.shownSeq {
		return
	}
	atBottom := m.vp.AtBottom()
	w := m.vp.Width
	if w < 1 {
		w = 1
	}
	m.vp.SetContent(ansi.Hardwrap(strings.TrimRight(p.Output, "\n"), w, true))
	if atBottom || m.sel != m.shownSel {
		m.vp.GotoBottom()
	}
	m.shownSel, m.shownSeq = m.sel, p.Seq
}

// padTo ANSI-aware-truncates s to w and right-pads with spaces to exactly w
// visible columns — so columns align and no line can overflow (overflow
// wraps terminal-side and desyncs the renderer = flicker).
func padTo(s string, w int) string {
	s = ansi.Truncate(s, w, "")
	if gap := w - ansi.StringWidth(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

func (m RunModel) View() string {
	w := m.width
	if w <= 0 {
		w = termWidth()
	}
	g := m.geom()

	head := "run · " + m.runMode
	if m.stopping {
		head += " · tearing down…"
	}
	if m.finished {
		head = "run · " + m.runMode + " · stopped"
	}
	if svcs := m.services(); len(svcs) == 1 {
		head += " · " + svcs[0]
	}

	var b strings.Builder
	b.WriteString(section(head, colGold, w) + "\n\n")

	left := m.leftColumn(g.leftW, g.bodyH, g.globalMode)
	div := styleMuted.Render(" │ ")
	var navLines []string
	if g.globalMode {
		navLines = m.navColumn(g.navW, g.bodyH)
	}
	for i := range g.bodyH {
		line := ""
		if g.globalMode {
			line = padTo(navLines[i], g.navW) + div
		}
		line += padTo(left[i], g.leftW)
		b.WriteString(line + "\n")
	}

	b.WriteString("\n" + ansi.Truncate(styleHelp.Render(m.footer(g)), w, "…"))
	return b.String()
}

// navColumn renders the left service nav: one row per service with a status
// glyph and the name, the active service highlighted. Headed by a section
// title so the column reads as its own pane.
func (m RunModel) navColumn(w, bodyH int) []string {
	svcs := m.services()
	lines := make([]string, 0, 1+len(svcs))
	lines = append(lines, section("services", colGold, w))
	active := m.currentService()
	for _, svc := range svcs {
		var marker, label string
		if svc == active {
			marker = styleSel.Render("▌")
			label = styleBrand.Render(svc)
		} else {
			marker = "  "
			label = styleMuted.Render(svc)
		}
		lines = append(lines, ansi.Truncate(marker+runStatus(m.serviceStatus(svc))+" "+label, w, "…"))
	}
	return fitLines(lines, bodyH)
}

// leftColumn returns exactly bodyH lines: tab bar (entrypoints), gap, log
// title, then the log. In global mode the tab bar shows only the active
// service's entrypoints, because services live in the left nav.
func (m RunModel) leftColumn(w, bodyH int, globalMode bool) []string {
	lines := make([]string, 0, bodyH)
	lines = append(lines, ansi.Truncate(m.tabBar(w, globalMode), w, "…"), "")

	if m.vpReady && len(m.procs) > 0 {
		sp := m.procs[m.sel]
		title := styleGold.Render("▌ ") +
			styleBrand.Render(sp.Target.Name) +
			"  " + styleMuted.Render(runStatusWord(sp.Status))
		lines = append(lines, ansi.Truncate(title, w, "…"))
		lines = append(lines, strings.Split(m.vp.View(), "\n")...)
	}
	return fitLines(lines, bodyH)
}

// fitLines forces a slice to exactly n lines: truncate the excess, pad short.
func fitLines(lines []string, n int) []string {
	if len(lines) > n {
		return lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
}

// footer keeps the full instructions on screen for the whole life of the TUI
// — it never collapses to just "quit". Run state is conveyed by the header.
func (m RunModel) footer(g runGeom) string {
	if g.globalMode {
		return "↑/↓ services · ←/→ entrypoints · 1-9 jump · pgup/pgdn scroll · q quit (teardown)"
	}
	return "↑/↓ pgup/pgdn scroll · ←/→ tabs · 1-9 jump · q quit (teardown)"
}

// tabBar renders one fixed row of tabs (status glyph + entrypoint name), the
// active one highlighted. In global mode the bar shows only the active
// service's entrypoints (services live in the left nav); in per-repo mode it
// shows every proc flat. Either way the label is just the entrypoint name —
// service identity is conveyed by the left nav (global) or the header
// (per-repo).
func (m RunModel) tabBar(w int, globalMode bool) string {
	var indices []int
	if globalMode {
		indices = m.serviceProcs(m.currentService())
	} else {
		indices = make([]int, len(m.procs))
		for i := range m.procs {
			indices[i] = i
		}
	}
	var parts []string
	for n, i := range indices {
		p := m.procs[i]
		nameStyled := styleBrand.Render(p.Target.Name)
		label := fmt.Sprintf("%d %s %s", n+1, runStatus(p.Status), nameStyled)
		if i == m.sel {
			parts = append(parts, styleSel.Render("▌"+label))
		} else {
			parts = append(parts, styleMuted.Render(" "+label))
		}
	}
	return ansi.Truncate(strings.Join(parts, styleMuted.Render(" · ")), w, "…")
}

func runStatus(s runner.Status) string {
	switch s {
	case runner.Running:
		return styleOK.Render("●")
	case runner.EnvUp:
		return styleGold.Render("◐")
	case runner.Pending:
		return styleMuted.Render("○")
	case runner.Exited:
		return styleOK.Render("✓")
	case runner.Failed:
		return styleErr.Render("✗")
	default:
		return styleMuted.Render("?")
	}
}

func runStatusWord(s runner.Status) string {
	switch s {
	case runner.Running:
		return "running"
	case runner.EnvUp:
		return "env up"
	case runner.Pending:
		return "pending"
	case runner.Exited:
		return "exited ok"
	case runner.Failed:
		return "failed"
	default:
		return "?"
	}
}
