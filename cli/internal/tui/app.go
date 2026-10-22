package tui

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Run opens the TUI in the alternate screen and blocks until the user quits.
// It returns an error when the daemon does not answer a ping or the program
// fails.
func Run() error {
	c := rpc.New("")
	// A ping first, so a stopped daemon fails here with a clear error.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.Ping(ctx); err != nil {
		return err
	}
	m := &model{c: c}
	// Mouse capture stays off, so the terminal keeps click-and-drag selection
	// for copying log lines.
	p := tea.NewProgram(m)
	// The log follower sends its lines through the program.
	m.program = p
	_, err := p.Run()
	if m.followCancel != nil {
		m.followCancel()
	}
	return err
}

const (
	// modeContainer names container mode in palette args like ":start
	// container" and in renderRight's header label. The CLI and RPC
	// layers use the typed anovelv1.Mode_MODE_* enums; these strings
	// belong to the text surfaces.
	modeContainer = "container"
	// modeGoExec names live go-exec mode on the same text surfaces.
	modeGoExec = "go-exec"
)

// view is the discriminator for top-level screens.
type view int

const (
	viewMain view = iota
	viewHelp
	viewCommand
	viewTopology
)

// model is the Bubble Tea root model.
type model struct {
	c       *rpc.Client
	program *tea.Program // for background goroutines to Send messages
	view    view
	width   int
	height  int
	err     error
	// Discovered services, refreshed periodically.
	services []*anovelv1.Service
	// loaded flips true on the first servicesMsg, so the nav can tell
	// "not refreshed yet" from "refreshed and found nothing".
	loaded      bool
	selectedSvc int // index into services
	// selectedTab is the combined index into [infras..., targets...].
	// Indices below len(svc.Infra) address infras; the rest address
	// targets at i-len(svc.Infra). The single sequence lets `←/→` cycle
	// through both kinds, and consumers branch on activeInfra and
	// activeTarget.
	selectedTab int
	// logLines holds the newest maxLogLines lines of the followed log.
	logLines []*anovelv1.StreamLogsResponse
	// followCancel stops the current log follower.
	followCancel context.CancelFunc
	// followGen numbers the log followers. onLogLines drops the lines of an
	// older follower, so a cancelled follower's late lines never reach the
	// pane.
	followGen int
	// logScroll counts the newer lines below the visible window. Zero follows
	// the tail. A positive offset holds the window in place while lines
	// arrive. followSelectedLogs resets it.
	logScroll  int
	cmdInput   string // command-palette input buffer, leading ":" included
	topologyTx string // last GetTopology response, rendered by viewTopology
	// pageScroll is the first line shown of the help or topology page.
	// pageWindow clamps it to the page.
	pageScroll int
	// status is the line of action feedback above the footer. Busy
	// persists until the action resolves, info fades after 5s, and an
	// error stays until the next action overrides it.
	status statusEntry
	// discoveryProblems counts the problems discovery found in the stack.
	// While positive, the idle status bar points to `a-novel core status`.
	discoveryProblems int
}

// statusLevel discriminates the four status-bar visual modes.
type statusLevel int

const (
	statusIdle statusLevel = iota
	statusBusy
	statusInfo
	statusError
)

// statusEntry is one snapshot of the status bar's contents.
type statusEntry struct {
	level statusLevel
	text  string
	at    time.Time
}

// maxLogLines bounds the log buffer so a chatty target cannot grow the
// model without limit.
const maxLogLines = 500

// statusInfoTTL is how long an info status stays before it fades.
const statusInfoTTL = 5 * time.Second

// refreshInterval is how often the UI polls the daemon for services.
const refreshInterval = 2 * time.Second

// pollCmd refreshes the services now and schedules the next poll.
func (m *model) pollCmd() tea.Cmd {
	return tea.Batch(refreshServicesCmd(m.c), tickEvery(refreshInterval))
}

func (m *model) Init() tea.Cmd {
	return m.pollCmd()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case servicesMsg:
		m.discoveryProblems = msg.discoveryProblems
		m.onServices(msg.services)
		return m, nil

	case errMsg:
		// Connection and refresh failures land in m.err, which the nav
		// renders when the service list is empty. The status bar is
		// reserved for action results.
		m.err = msg.err
		return m, nil

	case statusMsg:
		m.status = msg.entry
		return m, nil

	case actionResultMsg:
		return m, m.onActionResult(msg)

	case statusFadeMsg:
		// Only info fades: busy is still in-flight, and an error is sticky
		// until the user acts again.
		if m.status.level == statusInfo && time.Since(m.status.at) >= statusInfoTTL {
			m.status = statusEntry{}
		}
		return m, nil

	case logLinesMsg:
		m.onLogLines(msg)
		return m, nil

	case tickMsg:
		return m, m.pollCmd()

	case topologyMsg:
		m.topologyTx = msg.rendered
		m.view = viewTopology
		m.pageScroll = 0
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// onServices takes a fresh service list. It clears a transient error, such as
// a daemon-restart blip, so the nav stops showing it after recovery, and
// clamps the selection to the new list. onServices then follows the selected
// tab's log when that log changed, as on the first list or after the list
// shifts under the selection.
func (m *model) onServices(services []*anovelv1.Service) {
	prevLogID := m.activeLogID()
	m.services = services
	m.loaded = true
	m.err = nil
	if m.selectedSvc >= len(m.services) {
		m.selectedSvc = 0
	}
	if m.selectedTab >= m.tabCount() {
		m.selectedTab = 0
	}
	if m.activeLogID() != prevLogID {
		m.followSelectedLogs()
	}
}

// onActionResult shows an action's outcome in the status bar and refreshes
// the services, to pick up even a failed action's partial state. An error
// stays until the next command. A success fades, and reattaches the log
// follower, since the action may have replaced the log file: a restart
// truncates it, and even a plain start opens a fresh one.
func (m *model) onActionResult(msg actionResultMsg) tea.Cmd {
	if msg.err != nil {
		m.status = newStatus(statusError, msg.actionLabel+": "+msg.err.Error())
		return refreshServicesCmd(m.c)
	}
	m.status = newStatus(statusInfo, msg.successText)
	m.followSelectedLogs()
	fade := tea.Tick(statusInfoTTL, func(time.Time) tea.Msg { return statusFadeMsg{} })
	return tea.Batch(refreshServicesCmd(m.c), fade)
}

// onLogLines appends a follower's batch. While the pane is scrolled back, the
// offset grows with each line, so the window keeps its lines. Once the buffer
// is full, trimming drops the oldest lines and the clamp keeps the window
// inside the buffer.
func (m *model) onLogLines(msg logLinesMsg) {
	if msg.gen != m.followGen {
		return
	}
	m.logLines = append(m.logLines, msg.lines...)
	if over := len(m.logLines) - maxLogLines; over > 0 {
		m.logLines = m.logLines[over:]
	}
	if m.logScroll > 0 {
		m.logScroll = m.clampLogScroll(m.logScroll + len(msg.lines))
	}
}

// handleKey routes a key press to the active view.
func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.view {
	case viewHelp, viewTopology:
		// ↑/↓ scroll the page, and any other key returns to main.
		_, from, _ := m.pageWindow()
		switch msg.Key().Code {
		case tea.KeyUp, 'k':
			m.pageScroll = max(from-1, 0)
		case tea.KeyDown, 'j':
			m.pageScroll = from + 1
		default:
			m.view = viewMain
		}
		return m, nil
	case viewCommand:
		return m.handleCommandKey(msg)
	}
	// viewMain key handling.
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.view = viewHelp
		m.pageScroll = 0
	case "esc":
		m.view = viewCommand
		m.cmdInput = ":"
	case "j", "down":
		m.moveService(1)
	case "k", "up":
		m.moveService(-1)
	case "l", "right", "tab":
		m.moveTab(1)
	case "h", "left", "shift+tab":
		m.moveTab(-1)
	// Log-pane scrolling. Ctrl+↑/↓ step one line and PgUp/PgDn half a page.
	// Home/g jumps to the oldest buffered line and End/G back to the tail.
	case "pgup":
		m.logScroll = m.clampLogScroll(m.logScroll + m.logViewportHeight()/2)
	case "pgdown", "pgdn":
		m.logScroll = m.clampLogScroll(m.logScroll - m.logViewportHeight()/2)
	case "ctrl+up":
		m.logScroll = m.clampLogScroll(m.logScroll + 1)
	case "ctrl+down":
		m.logScroll = m.clampLogScroll(m.logScroll - 1)
	case "home", "g":
		m.logScroll = m.clampLogScroll(len(m.logLines))
	case "end", "G":
		m.logScroll = 0
	}
	return m, nil
}

// moveService steps the service selection by delta, wrapping at both ends,
// and follows the first tab of the newly selected service.
func (m *model) moveService(delta int) {
	if n := len(m.services); n > 0 {
		m.selectedSvc = (m.selectedSvc + delta + n) % n
		m.selectedTab = 0
		m.followSelectedLogs()
	}
}

// moveTab steps the tab selection by delta, wrapping at both ends, and
// follows the newly selected tab.
func (m *model) moveTab(delta int) {
	if n := m.tabCount(); n > 0 {
		m.selectedTab = (m.selectedTab + delta + n) % n
		m.followSelectedLogs()
	}
}

// clampLogScroll bounds a scroll offset to [0, max]. At max, the window still
// fills the pane above the paused indicator.
func (m *model) clampLogScroll(n int) int {
	if n <= 0 {
		return 0
	}
	rows := max(m.logViewportHeight()-1, 1)
	return min(n, max(len(m.logLines)-rows, 0))
}

// contentHeight is the height of the nav and right frames, borders included:
// the terminal height less the status bar and the footer hint.
func (m *model) contentHeight() int {
	return m.height - 2
}

// logViewportHeight is the row count of the log pane. renderRight sizes the
// pane with it, and pgup/pgdn page by half of it. Inside the right frame,
// the two borders, the divider, the detail header and each tab strip take
// one row. The pane keeps at least four.
func (m *model) logViewportHeight() int {
	h := m.contentHeight() - 4
	if svc := m.activeService(); svc != nil {
		if len(svc.GetInfra()) > 0 {
			h--
		}
		if len(svc.GetTargets()) > 0 {
			h--
		}
	}
	return max(h, 4)
}

// handleCommandKey edits the palette input. Enter runs it and Esc discards it.
func (m *model) handleCommandKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.Key().Code {
	case tea.KeyEsc:
		m.view = viewMain
		m.cmdInput = ""
		return m, nil
	case tea.KeyEnter:
		cmd := strings.TrimSpace(m.cmdInput)
		m.view = viewMain
		m.cmdInput = ""
		return m, m.runPaletteCommand(cmd)
	case tea.KeyBackspace:
		_, size := utf8.DecodeLastRuneInString(m.cmdInput)
		m.cmdInput = m.cmdInput[:len(m.cmdInput)-size]
		return m, nil
	}
	// Key.Text holds the characters a printable key types. Chords and special
	// keys leave it empty.
	if txt := msg.Key().Text; txt != "" {
		m.cmdInput += txt
	}
	return m, nil
}

// activeService returns the currently selected service, or nil if
// there is none (empty stack or out-of-range index).
func (m *model) activeService() *anovelv1.Service {
	if m.selectedSvc < 0 || m.selectedSvc >= len(m.services) {
		return nil
	}
	return m.services[m.selectedSvc]
}

// tabCount is the total number of selectable tabs for the active
// service, or zero when none is active.
func (m *model) tabCount() int {
	svc := m.activeService()
	if svc == nil {
		return 0
	}
	return len(svc.GetTargets()) + len(svc.GetInfra())
}

// activeInfra returns the selected infra entry, or nil when no tab or a
// target tab is selected. Infras come first in the combined tab index.
func (m *model) activeInfra() *anovelv1.Infra {
	infras := m.activeService().GetInfra()
	if m.selectedTab >= len(infras) {
		return nil
	}
	return infras[m.selectedTab]
}

// activeTarget returns the selected target, or nil when no tab or an infra
// tab is selected.
func (m *model) activeTarget() *anovelv1.Target {
	svc := m.activeService()
	i := m.selectedTab - len(svc.GetInfra())
	if i < 0 || i >= len(svc.GetTargets()) {
		return nil
	}
	return svc.GetTargets()[i]
}

// activeLogID returns the ID StreamLogs expects for the selected tab, or ""
// when none is selected. Targets use the runner's <stack>/<svc>/<tgt> form
// and infras the <stack>/<svc>/infra/<name> sentinel.
func (m *model) activeLogID() string {
	if in := m.activeInfra(); in != nil {
		return in.GetStack() + "/" + in.GetService() + "/infra/" + in.GetName()
	}
	return m.activeTarget().GetId()
}
