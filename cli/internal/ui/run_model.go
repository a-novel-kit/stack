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

// focus is which pane keystrokes drive. tab cycles tabs → log → console
// (console skipped when the terminal is too narrow to show it).
type focusZone int

const (
	focusTabs    focusZone = iota // ←/→/1-9 switch the active process tab
	focusLog                      // ↑/↓/pgup scroll the active process log
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

// runGeom is the resolved responsive layout. On a narrow terminal consoleOn
// is false and leftW is the full width (the console is hidden).
type runGeom struct {
	consoleOn              bool
	leftW, consoleW, bodyH int
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
	consoleW := clamp(w/3, 34, 56)
	leftW := w - consoleW - 3 // 3 = " │ " divider
	if w >= consoleMinWidth && leftW >= 40 {
		return runGeom{true, leftW, consoleW, bodyH}
	}
	return runGeom{false, w, 0, bodyH}
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
			m.focus = focusTabs
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

	switch k {
	case keyQ:
		return m.quitOrTeardown()
	case "tab":
		return m.cycleFocus()
	}

	// ←/h/shift+tab and →/l always switch tabs; ↑↓/kj switch tabs unless the
	// log is focused, where they scroll it instead.
	prev := k == keyLeft || k == "h" || k == "shift+tab"
	next := k == keyRight || k == "l"
	if m.focus != focusLog {
		prev = prev || k == keyUp || k == "k"
		next = next || k == keyDown || k == "j"
	}
	switch {
	case prev && m.sel > 0:
		m.sel--
		m.refreshLog(true)
		return m, nil
	case next && m.sel < len(m.procs)-1:
		m.sel++
		m.refreshLog(true)
		return m, nil
	case prev || next:
		return m, nil
	}
	if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
		if idx := int(k[0] - '1'); idx < len(m.procs) {
			m.sel = idx
			m.refreshLog(true)
		}
		return m, nil
	}
	if m.focus == focusLog && m.vpReady {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
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

// cycleFocus advances tabs → log → console → tabs, skipping console when the
// terminal is too narrow to show it.
func (m RunModel) cycleFocus() (tea.Model, tea.Cmd) {
	switch m.focus {
	case focusTabs:
		m.focus = focusLog
	case focusLog:
		if m.geom().consoleOn {
			m.focus = focusConsole
			return m, m.ti.Focus()
		}
		m.focus = focusTabs
	case focusConsole:
		m.focus = focusTabs
		m.ti.Blur()
	}
	return m, nil
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
		return consoleResultMsg{out: s}
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

	head := "run · live"
	if m.stopping {
		head = "run · tearing down…"
	}
	if m.finished {
		head = "run · stopped"
	}

	var b strings.Builder
	b.WriteString(section(head, colGold, w) + "\n\n")

	left := m.leftColumn(g.leftW, g.bodyH)
	if g.consoleOn {
		right := m.rightColumn(g.consoleW, g.bodyH)
		div := styleMuted.Render(" │ ")
		for i := range g.bodyH {
			b.WriteString(padTo(left[i], g.leftW) + div + padTo(right[i], g.consoleW) + "\n")
		}
	} else {
		for i := range g.bodyH {
			b.WriteString(padTo(left[i], g.leftW) + "\n")
		}
	}

	b.WriteString("\n" + ansi.Truncate(styleHelp.Render(m.footer(g.consoleOn)), w, "…"))
	return b.String()
}

// leftColumn returns exactly bodyH lines: tab bar, gap, title, then the log.
func (m RunModel) leftColumn(w, bodyH int) []string {
	lines := make([]string, 0, bodyH)
	lines = append(lines, ansi.Truncate(m.tabBar(w), w, "…"), "")

	if m.vpReady && len(m.procs) > 0 {
		sp := m.procs[m.sel]
		title := styleGold.Render("▌ ") +
			runName(sp.Target.Service, sp.Target.Name) +
			"  " + styleMuted.Render(runStatusWord(sp.Status))
		if m.focus == focusLog {
			title += "  " + styleMuted.Render("(scrolling)")
		}
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

func (m RunModel) footer(consoleOn bool) string {
	if m.finished {
		return "q quit"
	}
	switch m.focus {
	case focusConsole:
		return "type a command · enter run · esc leave console · tab next pane · ctrl+c quit (teardown)"
	case focusLog:
		return "↑/↓ pgup/pgdn scroll · tab next pane · q quit (teardown)"
	default:
		hint := "←/→ tabs · 1-9 jump · tab next pane · q quit (teardown)"
		if consoleOn {
			hint = "←/→ tabs · 1-9 jump · tab → log → console · q quit (teardown)"
		}
		return hint
	}
}

// tabBar renders one fixed row of tabs (status glyph + service-qualified
// name), the active one highlighted.
func (m RunModel) tabBar(w int) string {
	var parts []string
	for i, p := range m.procs {
		label := fmt.Sprintf("%d %s %s", i+1,
			runStatus(p.Status), runName(p.Target.Service, p.Target.Name))
		if i == m.sel {
			parts = append(parts, styleSel.Render("▌"+label))
		} else {
			parts = append(parts, styleMuted.Render(" "+label))
		}
	}
	return ansi.Truncate(strings.Join(parts, styleMuted.Render(" · ")), w, "…")
}

// runName is the service-qualified process name, so identically-named
// entrypoints across services are unambiguous in the tab bar.
func runName(service, name string) string {
	if service == "" {
		return styleBrand.Render(name)
	}
	return styleMuted.Render(service+"/") + styleBrand.Render(name)
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
