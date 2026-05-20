package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/a-novel-kit/stack/cli/internal/runner"
)

// focus is which pane keystrokes drive. There are only two: the log view
// (scrolling is always on; ←/→ and 1-9 still switch the process tab) and the
// console. tab toggles directly between them (console skipped when the
// terminal is too narrow to show it).
type focusZone int

const (
	focusLogs    focusZone = iota // scroll the log; ←/→ · 1-9 switch tab
	focusConsole                  // type a quick command (curl, …)
)

// consoleMinWidth is the narrowest total width that still gets a console
// column. Below this the console is hidden and the log uses the full width
// (the user asked for the log to fill the screen when there is no room).
const consoleMinWidth = 100

// RunModel is the live `run` dashboard: a full-width split — a tmux-style
// tabbed log view on the left, and a single shared interactive console on the
// right (curl & co. against the run's own ports). Every persistent element is
// static, so an idle frame is byte-identical and Bubble Tea's renderer skips
// the repaint — no flicker.
type RunModel struct {
	version string
	run     *runner.Runner
	cancel  func() // cancels the runner (triggers full teardown)

	vp      viewport.Model // active process log
	cvp     viewport.Model // console output (shared, not per-tab)
	ti      textinput.Model
	vpReady bool

	procs []runner.ProcView
	sel   int       // active tab
	focus focusZone // which pane has the keys

	// Render-on-change: the log viewport is only re-fed when the active tab's
	// content sequence (or the selection) actually changed.
	shownSel int
	shownSeq uint64

	cbuf []string // shared console scrollback (prompts + command output)

	width, height int
	stopping      bool // user asked to quit; waiting for teardown
	finished      bool // runner.Done() fired
	quitting      bool // ready to tea.Quit
}

type runDoneMsg struct{}

// consoleResultMsg carries the combined output of a finished console command.
type consoleResultMsg struct{ out string }

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
// started with, so q triggers a full scoped teardown.
func NewRun(version string, r *runner.Runner, cancel func()) RunModel {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "curl $SERVICE_JSON_KEYS_REST_URL/healthcheck …"
	// Static cursor: a blinking cursor would change View() every blink and
	// reintroduce the exact flicker we just removed.
	ti.Cursor.SetMode(cursor.CursorStatic)
	return RunModel{
		version: version, run: r, cancel: cancel,
		procs: r.Snapshot(), shownSel: -1, ti: ti,
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

// runGeom is the resolved responsive layout. consoleOn is false on a narrow
// terminal (console hidden, log column widens). globalMode is true when there
// are ≥2 services to navigate between — a vertical service nav appears on the
// far left and the top tab bar narrows to the active service's entrypoints.
type runGeom struct {
	consoleOn, globalMode bool
	navW, leftW, consoleW int
	bodyH                 int
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
	// Global mode: ≥2 services in scope. The nav column fits the longest
	// service name (capped) plus a tiny status glyph; we hide it on narrow
	// terminals because it would crowd the log.
	globalMode := false
	navW := 0
	if w >= consoleMinWidth {
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
	// Console: shown when the width still fits a reasonable log column.
	consoleOn := false
	consoleW := 0
	if w >= consoleMinWidth {
		consoleW = clamp(w/3, 34, 56)
	}
	// leftW = log column = remaining width after nav + console + dividers.
	leftW := w - navW - consoleW
	if navW > 0 {
		leftW -= 3 // " │ " divider between nav and log
	}
	if consoleW > 0 {
		leftW -= 3 // " │ " divider between log and console
	}
	if leftW < 40 {
		// Not enough room with the console; drop the console first.
		consoleW = 0
		leftW = w - navW
		if navW > 0 {
			leftW -= 3
		}
	}
	if leftW < 40 && navW > 0 {
		// Still tight — drop the nav too and fall back to single-service layout.
		navW = 0
		globalMode = false
		leftW = w
	}
	if consoleW > 0 {
		consoleOn = true
	}
	return runGeom{consoleOn: consoleOn, globalMode: globalMode, navW: navW, leftW: leftW, consoleW: consoleW, bodyH: bodyH}
}

// services returns the unique service names across procs, in first-seen
// order. The order is stable across ticks because procs is built from the
// runner's selection-time slice.
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

	case consoleResultMsg:
		m.appendConsole(msg.out)
		return m, nil

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

// applyGeom (re)sizes the viewports/input to the current layout. Called on
// resize; safe to call repeatedly.
func (m *RunModel) applyGeom() {
	g := m.geom()
	logH := max(g.bodyH-3, 1) // tab bar + gap + title above the log
	if !m.vpReady {
		m.vp = viewport.New(g.leftW, logH)
		m.cvp = viewport.New(max(g.consoleW, 1), max(g.bodyH-2, 1))
		m.vpReady = true
	} else {
		m.vp.Width, m.vp.Height = g.leftW, logH
		m.cvp.Width, m.cvp.Height = max(g.consoleW, 1), max(g.bodyH-2, 1)
	}
	if g.consoleOn {
		m.ti.Width = g.consoleW - 3
	}
	m.refreshLog(true)
	m.refreshConsole()
}

func (m RunModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	// ctrl+c is the only ALWAYS-global key: q is a typeable character, so it
	// must not quit while the console is focused.
	if k == "ctrl+c" {
		return m.quitOrTeardown()
	}

	if m.focus == focusConsole {
		switch k {
		case keyEsc:
			m.focus = focusLogs
			m.ti.Blur()
			return m, nil
		case "tab":
			return m.cycleFocus()
		case keyEnter:
			line := strings.TrimSpace(m.ti.Value())
			if line == "" {
				return m, nil
			}
			m.appendConsole(styleGold.Render("❯ ") + line)
			m.ti.Reset()
			return m, m.runConsole(line)
		default:
			var cmd tea.Cmd
			m.ti, cmd = m.ti.Update(msg)
			return m, cmd
		}
	}

	// focusLogs key dispatch differs between single-service and global modes.
	// Single: ←/→ + 1-9 = tab nav, everything else (↑/↓, pgup/pgdn, k/j…)
	// scrolls (the prior "scroll always on" rule). Global (left nav active):
	// ↑/↓ switch service, ←/→ switch in-service entrypoint, 1-9 jump to an
	// in-service entrypoint, pgup/pgdn/home/end scroll. The two axes need
	// distinct keys; pgup/pgdn becomes the scroll path.
	globalMode := m.geom().globalMode

	switch k {
	case keyQ:
		return m.quitOrTeardown()
	case "tab":
		return m.cycleFocus()
	case keyLeft, "h", "shift+tab":
		m.navEntry(-1)
		return m, nil
	case keyRight, "l":
		m.navEntry(+1)
		return m, nil
	}
	if globalMode {
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
		// 1-9 = jump to the Nth entrypoint of the current service in global
		// mode, or the Nth flat proc otherwise.
		n := int(k[0] - '1')
		if globalMode {
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
	g := m.geom()
	var indices []int
	if g.globalMode {
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

// cycleFocus toggles between the log view and the console. When the console
// is hidden (narrow terminal) it is a no-op — there is nowhere else to go.
func (m RunModel) cycleFocus() (tea.Model, tea.Cmd) {
	if m.focus == focusConsole {
		m.focus = focusLogs
		m.ti.Blur()
		return m, nil
	}
	if m.geom().consoleOn {
		m.focus = focusConsole
		return m, m.ti.Focus()
	}
	return m, nil // console hidden — stay on logs
}

// runConsole executes one quick command with the RUN's dir/env, so a curl
// hits the same ports the run allocated. Combined output, 30s ceiling.
func (m RunModel) runConsole(line string) tea.Cmd {
	dir, env := m.run.Console()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, "bash", "-lc", line)
		if dir != "" {
			c.Dir = dir
		}
		if env != nil {
			c.Env = env
		}
		out, err := c.CombinedOutput()
		s := strings.TrimRight(string(out), "\n")
		if err != nil && s == "" {
			s = err.Error()
		}
		// Sanitize EXACTLY like process logs: a command that emits \r
		// progress or cursor moves (curl -v, anything coloured) otherwise
		// breaks out of the console column and corrupts the log to its left.
		lines := strings.Split(s, "\n")
		for i, ln := range lines {
			lines[i] = runner.SanitizeLine(ln)
		}
		return consoleResultMsg{out: strings.Join(lines, "\n")}
	}
}

// appendConsole pushes lines onto the shared console scrollback (bounded) and
// re-feeds the console viewport, tail-following.
func (m *RunModel) appendConsole(s string) {
	m.cbuf = append(m.cbuf, strings.Split(s, "\n")...)
	const maxConsole = 2000
	if len(m.cbuf) > maxConsole {
		m.cbuf = m.cbuf[len(m.cbuf)-maxConsole:]
	}
	m.refreshConsole()
	m.cvp.GotoBottom()
}

func (m *RunModel) refreshConsole() {
	if !m.vpReady {
		return
	}
	w := m.cvp.Width
	if w < 1 {
		w = 1
	}
	m.cvp.SetContent(ansi.Hardwrap(strings.Join(m.cbuf, "\n"), w, true))
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
// visible columns — so columns align and no line can overflow (overflow wraps
// terminal-side and desyncs the renderer = flicker).
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

	// "running" not "live" — the dashboard is in steady state. "live" used
	// to clash with the live/container MODE vocabulary, which is confusing
	// when the actual mode is container.
	head := "run · running"
	if m.stopping {
		head = "run · tearing down…"
	}
	if m.finished {
		head = "run · stopped"
	}
	// Per-repo (single-service) mode: there is no left nav to carry the
	// service identity, so put it in the header. In global mode services
	// live in the left nav and don't belong in the header.
	if svcs := m.services(); len(svcs) == 1 {
		head += " · " + svcs[0]
	}

	var b strings.Builder
	b.WriteString(section(head, colGold, w) + "\n\n")

	left := m.leftColumn(g.leftW, g.bodyH, g.globalMode)
	div := styleMuted.Render(" │ ")
	var navLines, rightLines []string
	if g.globalMode {
		navLines = m.navColumn(g.navW, g.bodyH)
	}
	if g.consoleOn {
		rightLines = m.rightColumn(g.consoleW, g.bodyH)
	}
	for i := range g.bodyH {
		line := ""
		if g.globalMode {
			line = padTo(navLines[i], g.navW) + div
		}
		line += padTo(left[i], g.leftW)
		if g.consoleOn {
			line += div + padTo(rightLines[i], g.consoleW)
		}
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

// rightColumn returns exactly bodyH lines: console title, output, input.
func (m RunModel) rightColumn(w, bodyH int) []string {
	title := "console"
	if m.focus == focusConsole {
		title = "console (live)"
	}
	lines := []string{section(title, colGold, w)}
	lines = append(lines, strings.Split(m.cvp.View(), "\n")...)
	lines = fitLines(lines, bodyH-1)
	return append(lines, ansi.Truncate(m.ti.View(), w, ""))
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

// footer keeps the full, focus-appropriate instructions on screen for the
// whole life of the TUI — it never collapses to just "quit". Run state
// (live / tearing down / stopped) is conveyed by the header instead, so the
// keys you can still press stay visible until the result screen replaces the
// TUI entirely.
func (m RunModel) footer(g runGeom) string {
	if m.focus == focusConsole {
		return "type a command · enter run · esc/tab → logs · ctrl+c quit (teardown)"
	}
	if g.globalMode {
		hint := "↑/↓ services · ←/→ entrypoints · 1-9 jump · pgup/pgdn scroll · q quit (teardown)"
		if g.consoleOn {
			hint += " · tab → console"
		}
		return hint
	}
	hint := "↑/↓ pgup/pgdn scroll · ←/→ tabs · 1-9 jump · q quit (teardown)"
	if g.consoleOn {
		hint = "↑/↓ scroll · ←/→ tabs · 1-9 jump · tab → console · q quit (teardown)"
	}
	return hint
}

// tabBar renders one fixed row of tabs (status glyph + entrypoint name), the
// active one highlighted. In global mode the bar shows only the active
// service's entrypoints (services live in the left nav); in single-service
// mode it shows every proc flat. Either way the LABEL is just the entrypoint
// name — service identity is conveyed by the left nav (global) or the
// header (per-repo), so the tab label stays compact.
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
