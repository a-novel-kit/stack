package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/a-novel-kit/stack/cli/internal/runner"
)

// RunModel is the live `run` dashboard: a global status list of every
// long-lived process, with j/k to select one and a scrollable viewport of its
// full output. Tearing down (q, or a failure) is shown until the runner
// confirms everything is gone, then it quits.
type RunModel struct {
	version string
	run     *runner.Runner
	cancel  func() // cancels the runner (triggers full teardown)

	spinner spinner.Model
	vp      viewport.Model
	vpReady bool

	procs    []runner.ProcView
	sel      int  // selected process index
	logFocus bool // true: keys drive the viewport; false: j/k select

	width, height int
	stopping      bool // user asked to quit; waiting for teardown
	finished      bool // runner.Done() fired
	quitting      bool // ready to tea.Quit
}

type runDoneMsg struct{}

// NewRun builds the dashboard. cancel must cancel the context the runner was
// started with, so q triggers a full scoped teardown.
func NewRun(version string, r *runner.Runner, cancel func()) RunModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colBrand)
	return RunModel{version: version, run: r, cancel: cancel, spinner: sp}
}

func (m RunModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.waitDone())
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
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.procs = m.run.Snapshot()
		if m.sel >= len(m.procs) {
			m.sel = max(0, len(m.procs)-1)
		}
		m.refreshViewport()
		return m, cmd

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
	switch msg.String() {
	case "ctrl+c", "q":
		if m.finished {
			m.quitting = true
			return m, tea.Quit
		}
		if !m.stopping {
			m.stopping = true
			m.cancel() // trigger scoped teardown of every env + kill all procs
		}
		return m, nil
	case "tab":
		m.logFocus = !m.logFocus
		return m, nil
	case "up", "k":
		if m.logFocus {
			break
		}
		if m.sel > 0 {
			m.sel--
			m.refreshViewport()
		}
		return m, nil
	case "down", "j":
		if m.logFocus {
			break
		}
		if m.sel < len(m.procs)-1 {
			m.sel++
			m.refreshViewport()
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

// logHeight is the rows available to the focused-process viewport: total minus
// the header, the process list, and the footer.
func (m RunModel) logHeight() int {
	h := m.height - (len(m.procs) + 7)
	if h < 3 {
		h = 3
	}
	return h
}

// refreshViewport keeps the viewport showing the selected process's full
// output, auto-following the tail unless the user has scrolled up.
func (m *RunModel) refreshViewport() {
	if !m.vpReady || len(m.procs) == 0 {
		return
	}
	atBottom := m.vp.AtBottom()
	m.vp.Height = m.logHeight()
	m.vp.SetContent(strings.TrimRight(m.procs[m.sel].Output, "\n"))
	if atBottom {
		m.vp.GotoBottom()
	}
}

func (m RunModel) View() string {
	var b strings.Builder
	w := m.width
	if w <= 0 {
		w = termWidth()
	}

	head := "run · " + m.spinner.View() + " live"
	if m.stopping {
		head = "run · tearing down…"
	}
	if m.finished {
		head = "run · stopped"
	}
	b.WriteString(section(head, colGold, w) + "\n\n")

	for i, p := range m.procs {
		cursor := "  "
		if i == m.sel && !m.logFocus {
			cursor = styleSel.Render(glyphCursor) + " "
		}
		fmt.Fprintf(&b, "%s%s %s %s %s\n",
			cursor,
			runStatus(p.Status),
			runName(p.Target.Service, p.Target.Name),
			styleMuted.Render("("+p.Elapsed.Round(1e7).String()+")"),
			styleMuted.Render(clip(p.Last, w-44)),
		)
	}

	if m.vpReady && len(m.procs) > 0 {
		sp := m.procs[m.sel]
		bar := "▌ " + runName(sp.Target.Service, sp.Target.Name) + " log"
		if m.logFocus {
			bar += "  " + styleMuted.Render("(scrolling — tab to leave)")
		}
		b.WriteString("\n" + styleGold.Render(bar) + "\n")
		b.WriteString(m.vp.View() + "\n")
	}

	keys := "↑/↓ select · tab focus log · q quit (teardown)"
	if m.finished {
		keys = "q quit"
	}
	b.WriteString("\n" + styleHelp.Render(keys) + "\n")
	return b.String()
}

// runName is the service-qualified process name, so identically-named
// entrypoints across services are unambiguous in the dashboard.
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
