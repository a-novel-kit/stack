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
	// Discovery — refreshed periodically.
	services []*anovelv1.Service
	// Selection state.
	selectedSvc    int // index into services
	selectedTarget int // index into services[selectedSvc].Targets
	// Log streaming.
	logLines     []*anovelv1.LogLine
	followCancel context.CancelFunc // cancels the current follow goroutine on target switch
	// Command palette.
	cmdInput   string
	cmdHint    string // last command result / error
	topologyTx string // last GetTopology response (for viewTopology)
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
		m.err = msg.err
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
