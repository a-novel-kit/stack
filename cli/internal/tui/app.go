// Package tui implements `a-novel run ui` — the daemon-backed
// terminal UI per spec §14. Built on Bubble Tea + Lip Gloss; every
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
// Three-layer palette (spec §14.3) — first cut implements layers 1
// (footer hint) and 3 (dedicated :help screen). Layer 2 (Esc
// autocomplete palette) is a phase-13 polish addition; basic Esc opens
// a single-line command input for now.
package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	// Hand the program back into the model so background log-follow
	// goroutines can Send messages via p.Send (Bubble Tea's
	// cross-goroutine event-injection primitive).
	m.program = p
	_, err := p.Run()
	return err
}

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
	c        *rpc.Client
	program  *tea.Program // for background goroutines to Send messages
	view     view
	width    int
	height   int
	err      error
	// Discovery — refreshed periodically. `loaded` flips true on the
	// first servicesMsg so the nav can distinguish "haven't refreshed
	// yet" (loading…) from "refresh succeeded with zero results"
	// (genuinely no services discovered).
	services []*anovelv1.Service
	loaded   bool
	// Selection state.
	selectedSvc    int // index into services
	selectedTarget int // index into services[selectedSvc].Targets
	// Log streaming.
	logLines     []*anovelv1.LogLine
	followCancel context.CancelFunc // cancels the current follow goroutine on target switch
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
			if m.selectedTarget >= len(m.services[m.selectedSvc].Targets) {
				m.selectedTarget = 0
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
		return m, tea.Batch(refreshServicesCmd(m.c), fadeStatusAfter(5*time.Second))

	case statusFadeMsg:
		// Only fade info — busy is still in-flight, error is sticky
		// until the user acts again.
		if m.status.level == statusInfo && time.Since(m.status.at) >= 5*time.Second {
			m.status = statusEntry{}
		}
		return m, nil

	case logsMsg:
		if msg.replace {
			m.logLines = append(m.logLines[:0], msg.lines...)
		} else {
			m.logLines = append(m.logLines, msg.lines...)
		}
		// Bound to last 500 lines so the model doesn't grow unbounded.
		if len(m.logLines) > 500 {
			m.logLines = m.logLines[len(m.logLines)-500:]
		}
		return m, nil

	case tickMsg:
		return m, tea.Batch(refreshServicesCmd(m.c), tickEvery(2*time.Second))

	case topologyMsg:
		m.topologyTx = msg.rendered
		m.view = viewTopology
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			m.selectedTarget = 0
			m.logLines = nil
		}
		return m, m.followSelectedLogs()
	case "k", "up":
		if len(m.services) > 0 {
			m.selectedSvc--
			if m.selectedSvc < 0 {
				m.selectedSvc = len(m.services) - 1
			}
			m.selectedTarget = 0
			m.logLines = nil
		}
		return m, m.followSelectedLogs()
	case "l", "right", "tab":
		if m.activeServiceHasTargets() {
			m.selectedTarget = (m.selectedTarget + 1) % len(m.services[m.selectedSvc].Targets)
			m.logLines = nil
		}
		return m, m.followSelectedLogs()
	case "h", "left", "shift+tab":
		if m.activeServiceHasTargets() {
			m.selectedTarget--
			if m.selectedTarget < 0 {
				m.selectedTarget = len(m.services[m.selectedSvc].Targets) - 1
			}
			m.logLines = nil
		}
		return m, m.followSelectedLogs()
	}
	return m, nil
}

func (m *model) handleCommandKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
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
	case tea.KeyRunes, tea.KeySpace:
		m.cmdInput += msg.String()
		return m, nil
	}
	return m, nil
}

// activeServiceHasTargets is a clamp-helper for selection key handlers.
func (m *model) activeServiceHasTargets() bool {
	if m.selectedSvc >= len(m.services) {
		return false
	}
	return len(m.services[m.selectedSvc].Targets) > 0
}

func (m *model) activeTarget() *anovelv1.Target {
	if !m.activeServiceHasTargets() {
		return nil
	}
	return m.services[m.selectedSvc].Targets[m.selectedTarget]
}
