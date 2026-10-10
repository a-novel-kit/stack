// Package tui implements the terminal UI behind `a-novel run ui`, the
// daemon-backed dashboard for the local service stack. Every action
// routes through the same RPC client the CLI uses, so the UI and CLI
// always observe the same daemon state.
//
// The screen is a two-column dashboard: a service navigator on the
// left and a per-target detail-and-log pane on the right, with a
// status bar and a footer hint below.
//
//	┌──────────────┬──────────────────────────────────────────────────┐
//	│ Services     │ Tabs: [t1] [t2] [t3]                             │
//	│              │ ┌──────────────────────────────────────────────┐ │
//	│ ● svc-X 2/4  │ │ <target detail header>                       │ │
//	│ ○ svc-Y 0/4  │ │ ────────────────────────────────────────── │ │
//	│              │ │ <log lines, follow-end>                      │ │
//	│              │ │                                              │ │
//	│              │ └──────────────────────────────────────────────┘ │
//	│              │ <footer hint>                          [Esc] cmd │
//	└──────────────┴──────────────────────────────────────────────────┘
//
// Commands reach the daemon through three surfaces: the always-visible
// footer hint, an Esc-triggered command input with autocomplete, and a
// dedicated help screen.
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

// Run launches the TUI in the alt-screen and blocks until the user
// quits. Returns nil on clean exit, error on unrecoverable failure
// (typically: daemon unreachable).
func Run() error {
	c := rpc.New("")
	// Pre-flight ping so a down daemon surfaces clearly.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.Ping(ctx); err != nil {
		return err
	}
	m := &model{c: c}
	// Mouse-cell-motion capture stays off, so the terminal keeps its own
	// click-drag text selection. This keyboard-driven UI needs it for
	// copying log lines out.
	p := tea.NewProgram(m)
	// Background log-follow goroutines inject events through p.Send, so
	// the model needs the program.
	m.program = p
	_, err := p.Run()
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
	// Log streaming.
	logLines     []*anovelv1.LogLine
	followCancel context.CancelFunc // cancels the current follow goroutine on target switch
	// followGen tags each follower-goroutine generation; messages
	// from previous followers are dropped on arrival so the
	// cancel-then-start race window can't mix streams.
	followGen int
	// logScroll is the offset from the tail of logLines. Zero auto-follows
	// the bottom, so new lines push the view forward. A positive value
	// means the user paged up: the view holds at
	// `logLines[len-h-scroll : len-scroll]` while new lines accumulate in
	// the buffer. followSelectedLogs resets it along with the buffer.
	logScroll  int
	cmdInput   string // command-palette input buffer, leading ":" included
	topologyTx string // last GetTopology response, rendered by viewTopology
	// status is the line of action feedback above the footer. Busy
	// persists until the action resolves, info fades after 5s, and an
	// error stays until the next action overrides it.
	status statusEntry
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

func (m *model) Init() tea.Cmd {
	return tea.Batch(refreshServicesCmd(m.c), tickEvery(2*time.Second))
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case servicesMsg:
		m.services = msg.services
		m.loaded = true
		// A successful refresh clears a transient error such as a
		// daemon-restart blip, so the nav stops showing it after recovery.
		m.err = nil
		// Clamp selection.
		if m.selectedSvc >= len(m.services) {
			m.selectedSvc = 0
		}
		if m.selectedTab >= m.tabCount() {
			m.selectedTab = 0
		}
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
		if msg.err != nil {
			m.status = statusEntry{level: statusError, text: msg.actionLabel + ": " + msg.err.Error(), at: time.Now()}
			// Errors persist until the next command. Refresh anyway to
			// pick up partial state.
			return m, refreshServicesCmd(m.c)
		}
		m.status = statusEntry{level: statusInfo, text: msg.successText, at: time.Now()}
		// A successful action may have replaced the log file: kill+restart
		// truncates via O_TRUNC, and even a plain start opens a fresh one.
		// Reattaching the follower restarts the buffer from that file.
		m.followSelectedLogs()
		return m, tea.Batch(
			refreshServicesCmd(m.c),
			tea.Tick(statusInfoTTL, func(time.Time) tea.Msg { return statusFadeMsg{} }),
		)

	case statusFadeMsg:
		// Only info fades: busy is still in-flight, and an error is sticky
		// until the user acts again.
		if m.status.level == statusInfo && time.Since(m.status.at) >= statusInfoTTL {
			m.status = statusEntry{}
		}
		return m, nil

	case logLineMsg:
		// Drop messages from a stale follower generation. They arrive
		// during the cancel-then-start race window on a tab or service
		// switch, so only the current generation reaches the view.
		if msg.gen != m.followGen {
			return m, nil
		}
		m.logLines = append(m.logLines, msg.line)
		if len(m.logLines) > maxLogLines {
			m.logLines = m.logLines[len(m.logLines)-maxLogLines:]
		}
		// While paused, the offset moves past the new line so the window
		// keeps showing the same lines. Clamping snaps it back when trimming
		// pushed that window past the buffer.
		if m.logScroll > 0 {
			m.logScroll = m.clampLogScroll(m.logScroll + 1)
		}
		return m, nil

	case tickMsg:
		return m, tea.Batch(refreshServicesCmd(m.c), tickEvery(2*time.Second))

	case topologyMsg:
		m.topologyTx = msg.rendered
		m.view = viewTopology
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.view {
	case viewHelp, viewTopology:
		// Any key returns to main.
		m.view = viewMain
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
	// Log-pane scrolling: ctrl+up/down step one line, pgup/pgdn jump a
	// half-page, end/G returns to the tail (auto-follow) and home/g to the
	// top of the buffered window. clampLogScroll bounds the offset.
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

// clampLogScroll keeps logScroll inside [0, maxScroll], where maxScroll
// is the largest offset that still leaves the visible window full. That
// ceiling stops the user paging back until a single log line shows above
// the "logs paused" indicator, which takes one row of the pane whenever
// logScroll > 0.
func (m *model) clampLogScroll(n int) int {
	if n <= 0 {
		return 0
	}
	rows := max(m.logViewportHeight()-1, 1)
	return min(n, max(len(m.logLines)-rows, 0))
}

// contentHeight is the height of the nav and right frames: the terminal
// height less the rows reserved for the status bar, the footer hint and the
// frame borders.
func (m *model) contentHeight() int {
	return m.height - 4
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
	// Printable input carries its characters in Key.Text, which is populated
	// for runes and space and empty for modifier combos and special keys, so
	// appending it captures typed text while ignoring chords.
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
