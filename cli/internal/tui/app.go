// Package tui implements `a-novel run ui` — the daemon-backed
// terminal UI. Built on Bubble Tea + Lip Gloss; every
// action routes through the same RPC client the CLI uses, so the UI
// and CLI are always observably consistent.
//
// First-cut layout (this chunk):
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
// Three-layer palette — first cut implements layers 1
// (footer hint) and 3 (dedicated :help screen). Layer 2 (Esc
// autocomplete palette) is a phase-13 polish addition; basic Esc opens
// a single-line command input for now.
package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Run launches the TUI in the alt-screen and blocks until the user
// quits. Returns nil on clean exit, error on unrecoverable failure
// (typically: daemon unreachable).
func Run() error {
	c := rpc.New("")
	// Pre-flight ping so we surface daemon-down clearly instead of
	// dropping into an empty UI.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.Ping(ctx); err != nil {
		return err
	}
	m := newModel(c)
	// NOTE: no tea.WithMouseCellMotion(). Mouse-cell capture would put
	// the terminal into a mode where it forwards every mouse event to
	// us — useful for click-to-focus, but it disables the terminal's
	// own click-drag-to-select. Since this UI is keyboard-driven, the
	// trade we want is "native text selection works" over "we could
	// handle click events"; users routinely copy log lines out for
	// grepping / pasting into issues.
	p := tea.NewProgram(m)
	// Hand the program back into the model so background log-follow
	// goroutines can Send messages via p.Send (Bubble Tea's
	// cross-goroutine event-injection primitive).
	m.program = p
	_, err := p.Run()
	return err
}

// tabKindInfra / tabKindTarget are the discriminator values
// activeTabKind() returns. Hoisted into named constants so the dozen
// string-equality sites across the TUI agree on a single source.
const (
	tabKindInfra  = "infra"
	tabKindTarget = "target"
	// modeContainer / modeGoExec match palette args like ":start
	// container" and the user-facing labels in renderRight's header.
	// CLI/RPC use the typed anovelv1.Mode_MODE_* enums; these strings
	// live in the palette-args and label-display layers where
	// everything is text.
	modeContainer = "container"
	modeGoExec    = "go-exec"
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
	// Discovery — refreshed periodically. `loaded` flips true on the
	// first servicesMsg so the nav can distinguish "haven't refreshed
	// yet" (loading…) from "refresh succeeded with zero results"
	// (genuinely no services discovered).
	services []*anovelv1.Service
	loaded   bool
	// Selection state.
	selectedSvc int // index into services
	// selectedTab is the combined index into [targets..., infras...].
	// Indices < len(svc.Targets) address targets; the rest address
	// infras at i-len(svc.Targets). This unification lets `←/→`
	// cycle through both kinds and lets every consumer ask
	// activeTabKind() to branch.
	selectedTab int
	// Log streaming.
	logLines     []*anovelv1.LogLine
	followCancel context.CancelFunc // cancels the current follow goroutine on target switch
	// followGen tags each follower-goroutine generation; messages
	// from previous followers are dropped on arrival so the
	// cancel-then-start race window can't mix streams.
	followGen int
	// logScroll is the offset from the tail of logLines. 0 means
	// "auto-follow the bottom" — new lines push the view forward.
	// Any positive value means the user has paged up; the view shows
	// `logLines[len-h-scroll : len-scroll]` and new lines accumulate
	// in the buffer without scrolling the view. Reset to 0 on
	// tab/service switch and on successful actions (where the
	// underlying log file may have been truncated).
	logScroll int
	// Command palette.
	cmdInput   string
	topologyTx string // last GetTopology response (for viewTopology)
	// Status bar — single line of action feedback above the footer.
	// busy persists until the action resolves; info auto-fades after
	// 5s; error persists until next action overrides it. See
	// renderStatus + statusMsg / actionResultMsg / statusFadeMsg.
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

func newModel(c *rpc.Client) *model {
	return &model{c: c, view: viewMain}
}

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
		// A successful refresh clears any prior transient error (e.g., a
		// daemon-restart blip). Keeps the nav from showing a stale
		// error after recovery.
		m.err = nil
		// Clamp selection.
		if m.selectedSvc >= len(m.services) {
			m.selectedSvc = 0
		}
		if m.selectedSvc < len(m.services) {
			if m.selectedTab >= m.tabCount() {
				m.selectedTab = 0
			}
		}
		return m, nil

	case errMsg:
		// Connection / refresh failures land in m.err (rendered in the
		// nav when the service list is empty). Don't route these into
		// the status bar — that's reserved for action results.
		m.err = msg.err
		return m, nil

	case statusMsg:
		m.status = msg.entry
		if msg.entry.level == statusInfo {
			return m, fadeStatusAfter(5 * time.Second)
		}
		return m, nil

	case actionResultMsg:
		if msg.err != nil {
			m.status = statusEntry{level: statusError, text: msg.actionLabel + ": " + msg.err.Error(), at: time.Now()}
			// Errors persist; user dismisses by issuing the next
			// command. Still refresh to pick up partial state.
			return m, refreshServicesCmd(m.c)
		}
		m.status = statusEntry{level: statusInfo, text: msg.successText, at: time.Now()}
		// Any successful action may have replaced the underlying
		// log file (kill+restart truncates via O_TRUNC; even a plain
		// start opens a fresh file). The buffered logLines are now
		// stale — drop them, reset the scroll, and reattach the
		// follower so it streams the fresh history-then-tail. The
		// follower-generation counter inside followSelectedLogs
		// drops any in-flight messages from the prior follower.
		m.logLines = nil
		m.logScroll = 0
		return m, tea.Batch(refreshServicesCmd(m.c), fadeStatusAfter(5*time.Second), m.followSelectedLogs())

	case statusFadeMsg:
		// Only fade info — busy is still in-flight, error is sticky
		// until the user acts again.
		if m.status.level == statusInfo && time.Since(m.status.at) >= 5*time.Second {
			m.status = statusEntry{}
		}
		return m, nil

	case logsMsg:
		// Drop messages from a stale follower generation — happens
		// during the cancel-then-start race window on tab/service
		// switches. Without this, the prior follower's last few
		// lines would mix into the new tab's view.
		if msg.gen != m.followGen {
			return m, nil
		}
		m.logLines = append(m.logLines, msg.lines...)
		// Pause-anchor: when paused (logScroll > 0), shift the offset
		// forward by however many lines arrived so the visible window
		// continues to show the same absolute lines instead of
		// drifting toward the tail. When logScroll == 0 we're in
		// auto-follow mode; nothing to anchor.
		if m.logScroll > 0 {
			m.logScroll += len(msg.lines)
		}
		// Bound to last 500 lines so the model doesn't grow unbounded.
		if len(m.logLines) > 500 {
			m.logLines = m.logLines[len(m.logLines)-500:]
		}
		// If the paused view's target line fell off the front of the
		// trimmed buffer (or off the viewport-aware max), snap scroll
		// to the most-back position that still shows a full window.
		if m.logScroll > 0 {
			m.logScroll = m.clampLogScroll(m.logScroll)
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
		return m, nil
	case "esc":
		m.view = viewCommand
		m.cmdInput = ":"
		return m, nil
	case "j", "down":
		if len(m.services) > 0 {
			m.selectedSvc = (m.selectedSvc + 1) % len(m.services)
			m.selectedTab = 0
			m.logLines = nil
			m.logScroll = 0
		}
		return m, m.followSelectedLogs()
	case "k", "up":
		if len(m.services) > 0 {
			m.selectedSvc--
			if m.selectedSvc < 0 {
				m.selectedSvc = len(m.services) - 1
			}
			m.selectedTab = 0
			m.logLines = nil
			m.logScroll = 0
		}
		return m, m.followSelectedLogs()
	case "l", "right", "tab":
		if m.tabCount() > 0 {
			m.selectedTab = (m.selectedTab + 1) % m.tabCount()
			m.logLines = nil
			m.logScroll = 0
		}
		return m, m.followSelectedLogs()
	case "h", "left", "shift+tab":
		if m.tabCount() > 0 {
			m.selectedTab--
			if m.selectedTab < 0 {
				m.selectedTab = m.tabCount() - 1
			}
			m.logLines = nil
			m.logScroll = 0
		}
		return m, m.followSelectedLogs()
	// Log-pane scrolling. Stepping units chosen so a single key feels
	// responsive (ctrl+up/down step one line) while pgup/pgdn jump a
	// half-page each. End/g returns to the tail (auto-follow on); home
	// jumps to the top of the buffered window. logScroll is clamped to
	// [0, len(logLines)] by clampLogScroll.
	case "pgup":
		m.logScroll = m.clampLogScroll(m.logScroll + m.logViewportHeight()/2)
		return m, nil
	case "pgdown", "pgdn":
		m.logScroll = m.clampLogScroll(m.logScroll - m.logViewportHeight()/2)
		return m, nil
	case "ctrl+up":
		m.logScroll = m.clampLogScroll(m.logScroll + 1)
		return m, nil
	case "ctrl+down":
		m.logScroll = m.clampLogScroll(m.logScroll - 1)
		return m, nil
	case "home", "g":
		m.logScroll = m.clampLogScroll(len(m.logLines))
		return m, nil
	case "end", "G":
		m.logScroll = 0
		return m, nil
	}
	return m, nil
}

// clampLogScroll keeps logScroll inside [0, maxScroll] where maxScroll
// is the largest offset that still leaves the visible window FULL — i.e.
// `len(logLines) - contentHeight`. Without that ceiling the user could
// page back until only one line of log shows above the indicator, which
// looks like "logs paused" took over the pane.
//
// Zero = auto-follow tail; positive = paged up that many lines from the
// tail. The "−1" accounts for the indicator row that's reserved at the
// bottom of the pane whenever logScroll > 0 (see renderLogs).
func (m *model) clampLogScroll(n int) int {
	if n < 0 {
		return 0
	}
	h := m.logViewportHeight()
	if n > 0 {
		// Indicator row will take one line; the actual log content fits
		// in (h-1) rows.
		h--
	}
	if h < 1 {
		h = 1
	}
	maxScroll := len(m.logLines) - h
	if maxScroll < 0 {
		maxScroll = 0
	}
	if n > maxScroll {
		return maxScroll
	}
	return n
}

// logViewportHeight mirrors the height arithmetic in renderRight so
// pgup/pgdn jump by a true half-page. The constants here MUST match
// renderRight: -4 chrome lines (status bar, footer, two borders), -5
// for header+divider inside the right frame, minus the number of tab
// rows we actually render.
func (m *model) logViewportHeight() int {
	contentHeight := m.height - 4
	rowCount := 0
	svc := m.activeService()
	if svc != nil {
		if len(svc.GetInfra()) > 0 {
			rowCount++
		}
		if len(svc.GetTargets()) > 0 {
			rowCount++
		}
	}
	h := contentHeight - 5 - (rowCount - 1)
	if h < 4 {
		h = 4
	}
	return h
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
		if len(m.cmdInput) > 0 {
			m.cmdInput = m.cmdInput[:len(m.cmdInput)-1]
		}
		return m, nil
	}
	// Printable input carries its characters in Key.Text — populated for runes
	// and space, empty for modifier combos and special keys. Appending it both
	// captures typed text and ignores chords, replacing v1's KeyRunes/KeySpace
	// cases (the KeyType enum was removed in Bubble Tea v2).
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
// service — targets first, then infras. Zero if no service active.
func (m *model) tabCount() int {
	svc := m.activeService()
	if svc == nil {
		return 0
	}
	return len(svc.GetTargets()) + len(svc.GetInfra())
}

// activeTabKind returns "infra", "target", or "" depending on which
// section the selectedTab index falls into. Infras come FIRST in the
// index so the right pane's two-row tab strip can render them on the
// top row and target-row second — and ←/→ navigation walks the
// concatenated sequence linearly.
func (m *model) activeTabKind() string {
	svc := m.activeService()
	if svc == nil || m.selectedTab < 0 || m.selectedTab >= m.tabCount() {
		return ""
	}
	if m.selectedTab < len(svc.GetInfra()) {
		return tabKindInfra
	}
	return tabKindTarget
}

// activeInfra returns the selected infra entry, or nil when the
// active tab is a target.
func (m *model) activeInfra() *anovelv1.Infra {
	if m.activeTabKind() != tabKindInfra {
		return nil
	}
	return m.services[m.selectedSvc].GetInfra()[m.selectedTab]
}

// activeTarget returns the selected target, or nil when the active
// tab is an infra entry.
func (m *model) activeTarget() *anovelv1.Target {
	if m.activeTabKind() != tabKindTarget {
		return nil
	}
	svc := m.services[m.selectedSvc]
	return svc.GetTargets()[m.selectedTab-len(svc.GetInfra())]
}

// activeLogID returns the ID format expected by StreamLogs for the
// currently selected tab — target IDs use the runner's <stack>/<svc>/<tgt>
// form, infra IDs use the <stack>/<svc>/infra/<name> sentinel.
func (m *model) activeLogID() string {
	switch m.activeTabKind() {
	case tabKindTarget:
		return m.activeTarget().GetId()
	case tabKindInfra:
		in := m.activeInfra()
		return in.GetStack() + "/" + in.GetService() + "/infra/" + in.GetName()
	}
	return ""
}
