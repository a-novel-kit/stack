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

// RunModel is the live `run` dashboard. It is a tmux-style tabbed view: one
// tab per long-lived process, a single contained viewport showing the active
// one's scrollback. The layout height is fixed (it never grows with the
// process count), so the frame size is stable tick-to-tick — that, plus
// sanitized output and render-on-change, is what keeps it from flickering or
// bleeding into the host terminal.
type RunModel struct {
	version string
	run     *runner.Runner
	cancel  func() // cancels the runner (triggers full teardown)

	vp      viewport.Model
	vpReady bool

	procs    []runner.ProcView
	sel      int  // active tab
	logFocus bool // true: keys scroll the viewport; false: keys switch tabs

	// Render-on-change: the viewport is only re-fed when the active tab's
	// content sequence (or the selection) actually changed, instead of every
	// spinner tick — re-wrapping a multi-KB string 10×/s was the flicker.
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
// Bubble Tea's renderer only suppresses a repaint when the frame is
// byte-identical. A non-animated frame means the screen is rewritten ONLY
// when log content or status actually changes — no idle flicker (the WSL /
// ConPTY flicker is just frequent full-frame rewrites).
type tickMsg struct{}

// pollInterval is the snapshot cadence — fast enough to feel live, slow
// enough that an unchanged frame is genuinely idle.
const pollInterval = 150 * time.Millisecond

func tick() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// NewRun builds the dashboard. cancel must cancel the context the runner was
// started with, so q triggers a full scoped teardown.
func NewRun(version string, r *runner.Runner, cancel func()) RunModel {
	// Seed the process list so the first frame already shows every tab
	// (Pending) instead of an empty box that fills on the first tick.
	return RunModel{
		version: version, run: r, cancel: cancel,
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

func (m RunModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		h := m.logHeight()
		if !m.vpReady {
			m.vp = viewport.New(msg.Width, h)
			m.vpReady = true
		} else {
			m.vp.Width, m.vp.Height = msg.Width, h
		}
		m.refreshViewport(true) // geometry changed → force a re-feed
		return m, nil

	case tickMsg:
		m.procs = m.run.Snapshot()
		if m.sel >= len(m.procs) {
			m.sel = max(0, len(m.procs)-1)
		}
		m.refreshViewport(false) // only re-feeds if the active tab changed
		return m, tick()         // reschedule the next poll

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

	if m.logFocus && m.vpReady {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m RunModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "ctrl+c", "q":
		if m.finished {
			m.quitting = true
			return m, tea.Quit
		}
		if !m.stopping {
			m.stopping = true
			m.cancel() // scoped teardown of every env + kill all procs
		}
		return m, nil
	case "tab":
		m.logFocus = !m.logFocus
		return m, nil
	}

	// Tab navigation. ←/h/shift+tab and →/l always switch tabs; ↑↓/kj also
	// switch tabs UNLESS the log is focused, where they scroll instead.
	prev := k == keyLeft || k == "h" || k == "shift+tab"
	next := k == keyRight || k == "l"
	if !m.logFocus {
		prev = prev || k == keyUp || k == "k"
		next = next || k == keyDown || k == "j"
	}
	switch {
	case prev && m.sel > 0:
		m.sel--
		m.refreshViewport(true)
		return m, nil
	case next && m.sel < len(m.procs)-1:
		m.sel++
		m.refreshViewport(true)
		return m, nil
	case prev || next:
		return m, nil // at an edge — swallow so it doesn't scroll the log
	}
	// Number keys jump straight to a tab (1-indexed).
	if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
		if idx := int(k[0] - '1'); idx < len(m.procs) {
			m.sel = idx
			m.refreshViewport(true)
		}
		return m, nil
	}
	if m.logFocus && m.vpReady {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

// logHeight is the viewport's row count. It depends ONLY on the terminal
// height — never on the process count — so the frame never reflows as
// processes come and go (a reflow every tick reads as flicker).
func (m RunModel) logHeight() int {
	// header(1) + gap(1) + tab bar(1) + gap(1) + log title(1) + gap(1) +
	// footer(1) = 7 chrome rows.
	h := m.height - 7
	if h < 3 {
		h = 3
	}
	return h
}

// refreshViewport re-feeds the viewport with the active tab's scrollback, but
// only when something actually changed (or force) — re-setting content every
// tick is what made it flicker. Tail-follows unless the user scrolled up.
func (m *RunModel) refreshViewport(force bool) {
	if !m.vpReady || len(m.procs) == 0 {
		return
	}
	if m.sel >= len(m.procs) {
		m.sel = len(m.procs) - 1
	}
	p := m.procs[m.sel]
	if !force && m.sel == m.shownSel && p.Seq == m.shownSeq {
		return // nothing new on the active tab — leave the frame untouched
	}
	atBottom := m.vp.AtBottom()
	m.vp.Height = m.logHeight()
	// Hard-wrap every line to the viewport width so NO line's visible width
	// exceeds the terminal — an over-wide line wraps terminal-side, desyncs
	// Bubble Tea's renderer, and full-repaints every tick (the flicker).
	// ANSI-aware: SGR colour is preserved across the wrap.
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

func (m RunModel) View() string {
	var b strings.Builder
	w := m.width
	if w <= 0 {
		w = termWidth()
	}

	// Static (non-animated) header: it changes only on a real state
	// transition, so an idle frame is byte-identical and the renderer skips
	// the repaint entirely — the core anti-flicker invariant.
	head := "run · live"
	if m.stopping {
		head = "run · tearing down…"
	}
	if m.finished {
		head = "run · stopped"
	}
	b.WriteString(section(head, colGold, w) + "\n\n")

	b.WriteString(m.tabBar(w) + "\n\n")

	if m.vpReady && len(m.procs) > 0 {
		sp := m.procs[m.sel]
		// Components are pre-styled (nested SGR); don't re-wrap the whole
		// string in another style — just compose and ANSI-truncate to w so
		// the title line can never exceed the terminal width.
		title := styleGold.Render("▌ ") +
			runName(sp.Target.Service, sp.Target.Name) +
			"  " + styleMuted.Render(runStatusWord(sp.Status))
		if m.logFocus {
			title += "  " + styleMuted.Render("(scrolling — tab to leave)")
		}
		b.WriteString(ansi.Truncate(title, w, "…") + "\n")
		b.WriteString(m.vp.View() + "\n")
	}

	keys := "←/→ tabs · 1-9 jump · tab scroll · q quit (teardown)"
	if m.logFocus {
		keys = "↑/↓ pgup/pgdn scroll · tab back to tabs · q quit (teardown)"
	}
	if m.finished {
		keys = "q quit"
	}
	b.WriteString("\n" + ansi.Truncate(styleHelp.Render(keys), w, "…"))
	return b.String()
}

// tabBar renders one fixed row of tabs (status glyph + service-qualified
// name), the active one highlighted. It is clipped to the width so it can
// never wrap and change the frame height.
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
	// ANSI-aware truncate: counts VISIBLE width (not escape bytes) so the bar
	// is bounded to the terminal and never sliced through an escape sequence
	// (rune-count clip did exactly that — the original flicker).
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
		return styleWarn.Render("■")
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
		return "exited"
	case runner.Failed:
		return "failed"
	default:
		return "?"
	}
}
