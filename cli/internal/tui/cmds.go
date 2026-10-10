package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Validation errors shared by the palette verbs that need a selection.
const (
	noServiceText = "no active service — select one with ↑/↓ first"
	noTabText     = "no active tab — select one with ←/→ first"
)

// servicesMsg carries a fresh ListServices snapshot from the daemon.
type servicesMsg struct{ services []*anovelv1.Service }

// errMsg surfaces a non-fatal error, such as a transient RPC failure
// during refresh. It lands in m.err, which the nav panel renders when
// the service list is empty.
type errMsg struct{ err error }

// statusMsg sets the status bar to a given entry: a busy status, or a
// validation error that involves no RPC round trip ("no active target").
type statusMsg struct{ entry statusEntry }

// actionResultMsg carries the outcome of a daemon-backed action, so
// Update can render info or error in the status bar and trigger a state
// refresh. actionLabel is the short verb-phrase that prefixes the error
// message ("start service-template/grpc").
type actionResultMsg struct {
	actionLabel string
	successText string
	err         error
}

// statusFadeMsg is the tick that times out an info-level status entry.
type statusFadeMsg struct{}

// setStatusCmd is a Cmd that emits one statusMsg.
func setStatusCmd(level statusLevel, text string) tea.Cmd {
	return func() tea.Msg {
		return statusMsg{entry: statusEntry{level: level, text: text, at: time.Now()}}
	}
}

// runAction runs a daemon-backed palette action. It shows busyText while
// do runs under timeout, then emits an actionResultMsg that Update renders
// as successText, or as the error prefixed by actionLabel.
func runAction(busyText, successText, actionLabel string, timeout time.Duration, do func(context.Context) error) tea.Cmd {
	return tea.Batch(
		setStatusCmd(statusBusy, busyText),
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			return actionResultMsg{actionLabel: actionLabel, successText: successText, err: do(ctx)}
		},
	)
}

// logLineMsg carries one log line from a follower goroutine into the
// model. gen is the follower-generation tag: Update drops any message
// whose gen does not match m.followGen, so only the current follower's
// lines reach the view across the cancel-then-start race.
type logLineMsg struct {
	line *anovelv1.LogLine
	gen  int
}

// tickMsg is the periodic refresh trigger.
type tickMsg struct{}

// refreshServicesCmd asks the daemon for a snapshot of every service in
// the default stack. The UI is scoped to a single stack.
func refreshServicesCmd(c *rpc.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		resp, err := c.ListServices(ctx, "")
		if err != nil {
			return errMsg{err: err}
		}
		return servicesMsg{services: resp.GetServices()}
	}
}

// tickEvery emits a tickMsg after d.
func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{} })
}

// followSelectedLogs restarts the log pane on the selected tab. The
// server's follow mode streams the whole log before tailing it, so the
// buffer and scroll restart empty. A background goroutine feeds the lines
// in through program.Send until the next call cancels it.
func (m *model) followSelectedLogs() {
	m.logLines = nil
	m.logScroll = 0
	// Cancel the prior follower and bump the generation tag, so any of
	// its late messages carry a stale gen and Update drops them.
	if m.followCancel != nil {
		m.followCancel()
		m.followCancel = nil
	}
	m.followGen++
	id := m.activeLogID()
	if id == "" || m.program == nil {
		return
	}
	gen := m.followGen
	ctx, cancel := context.WithCancel(context.Background())
	m.followCancel = cancel
	go func() {
		stream, err := m.c.StreamLogs(ctx, id, "", true /* follow */, anovelv1.LogStream_LOG_STREAM_UNSPECIFIED)
		if err != nil {
			m.program.Send(errMsg{err: err})
			return
		}
		for ln, err := range stream {
			if err != nil || ctx.Err() != nil {
				return
			}
			m.program.Send(logLineMsg{line: ln, gen: gen})
		}
	}()
}

// runPaletteCommand dispatches a `:command` from the palette. The returned
// tea.Cmd runs the action asynchronously and refreshes state on completion.
func (m *model) runPaletteCommand(input string) tea.Cmd {
	parts := strings.Fields(strings.TrimPrefix(input, ":"))
	if len(parts) == 0 {
		return nil
	}
	verb, args := parts[0], parts[1:]
	svc, t, in := m.activeService(), m.activeTarget(), m.activeInfra()
	switch verb {
	case "quit", "q":
		return tea.Quit
	case "refresh":
		m.followSelectedLogs()
		return refreshServicesCmd(m.c)
	case "start":
		// :start addresses targets only: a single infra container cannot
		// be cold-started in isolation, since it may depend on other
		// infra.
		if in != nil {
			return setStatusCmd(statusError,
				":start only works on targets. For infra: use :infra-start (whole service) or :restart (this container)")
		}
		if t == nil {
			return setStatusCmd(statusError, "no active target — select one with ←/→ first")
		}
		mode, modeLabel := anovelv1.Mode_MODE_GO_EXEC, modeGoExec
		if len(args) > 0 && args[0] == modeContainer {
			mode, modeLabel = anovelv1.Mode_MODE_CONTAINER, modeContainer
		}
		return runAction("Starting "+t.GetName()+" ("+modeLabel+")... infra + one-shots will run first",
			"Started "+t.GetName(), "start "+t.GetName(), time.Minute,
			func(ctx context.Context) error {
				_, err := m.c.StartTarget(ctx, t.GetId(), mode)
				return err
			})
	case "kill":
		// An infra entry stops that one container and leaves the rest of
		// the service's infra and targets alone.
		switch {
		case t != nil:
			return runAction("Killing "+t.GetName()+"...", "Killed "+t.GetName(), "kill "+t.GetName(),
				30*time.Second, func(ctx context.Context) error {
					_, err := m.c.KillTarget(ctx, t.GetId(), 10*time.Second)
					return err
				})
		case in != nil:
			return runAction("Stopping infra container "+in.GetName()+"...", "Stopped infra "+in.GetName(),
				"kill infra "+in.GetName(), 30*time.Second, func(ctx context.Context) error {
					_, err := m.c.KillInfraContainer(ctx, in.GetStack(), in.GetService(), in.GetName())
					return err
				})
		}
		return setStatusCmd(statusError, noTabText)
	case "restart":
		switch {
		case t != nil:
			return runAction("Restarting "+t.GetName()+"...", "Restarted "+t.GetName(), "restart "+t.GetName(),
				time.Minute, func(ctx context.Context) error {
					_, err := m.c.RestartTarget(ctx, t.GetId(), anovelv1.Mode_MODE_UNSPECIFIED)
					return err
				})
		case in != nil:
			return runAction("Restarting infra container "+in.GetName()+"...", "Restarted infra "+in.GetName(),
				"restart infra "+in.GetName(), time.Minute, func(ctx context.Context) error {
					_, err := m.c.RestartInfraContainer(ctx, in.GetStack(), in.GetService(), in.GetName())
					return err
				})
		}
		return setStatusCmd(statusError, noTabText)
	case "infra-start":
		if svc == nil {
			return setStatusCmd(statusError, noServiceText)
		}
		return runAction("Bringing up infra + one-shots for "+svc.GetName()+"...", "Infra ready for "+svc.GetName(),
			"infra-start "+svc.GetName(), 5*time.Minute, func(ctx context.Context) error {
				_, err := m.c.StartInfra(ctx, "", svc.GetName(), anovelv1.Mode_MODE_GO_EXEC)
				return err
			})
	case "infra-kill":
		if svc == nil {
			return setStatusCmd(statusError, noServiceText)
		}
		force := len(args) > 0 && args[0] == "force"
		busyText := "Tearing down infra for " + svc.GetName() + "..."
		if force {
			busyText = "Force-tearing down infra + targets for " + svc.GetName() + "..."
		}
		return runAction(busyText, "Infra down for "+svc.GetName(), "infra-kill "+svc.GetName(),
			30*time.Second, func(ctx context.Context) error {
				_, err := m.c.KillInfra(ctx, "", svc.GetName(), force)
				return err
			})
	case "volume-backup":
		if svc == nil {
			return setStatusCmd(statusError, noServiceText)
		}
		return runAction("Backing up volumes for "+svc.GetName()+"... (may take a while)",
			"Backed up volumes for "+svc.GetName(), "volume-backup "+svc.GetName(),
			5*time.Minute, func(ctx context.Context) error {
				_, err := m.c.BackupVolume(ctx, "", svc.GetName(), "", false)
				return err
			})
	case "topology":
		// GetName is nil-safe, so with no service selected the empty name
		// asks for the whole stack.
		svcName := svc.GetName()
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			resp, err := m.c.GetTopology(ctx, "", svcName)
			if err != nil {
				return statusMsg{entry: statusEntry{level: statusError, text: "topology: " + err.Error(), at: time.Now()}}
			}
			return topologyMsg{rendered: resp.GetRendered()}
		}
	default:
		return setStatusCmd(statusError, "unknown command: :"+verb+"  (press ? for the full list)")
	}
}

// topologyMsg carries a GetTopology RPC response into the model so
// the renderer can swap the right pane to topology view.
type topologyMsg struct{ rendered string }
