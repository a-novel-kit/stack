package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// servicesMsg carries a fresh ListServices snapshot from the daemon.
type servicesMsg struct{ services []*anovelv1.Service }

// errMsg surfaces a non-fatal error to the model (e.g., transient RPC
// failure during refresh). Lands in m.err; rendered in the nav panel
// when the service list is empty.
type errMsg struct{ err error }

// statusMsg sets the status bar to a given entry. Used for transient
// state changes (busy → info → idle) and validation errors that don't
// involve an RPC round trip ("no active target").
type statusMsg struct{ entry statusEntry }

// actionResultMsg carries the outcome of a daemon-backed action so
// Update can render the right info/error in the status bar AND trigger
// a state refresh. actionLabel is the short verb-phrase used to
// prefix the error message ("start service-template/grpc").
type actionResultMsg struct {
	actionLabel string
	successText string
	err         error
}

// statusFadeMsg is the tick that times out an info-level status entry.
type statusFadeMsg struct{}

// fadeStatusAfter returns a Cmd that emits statusFadeMsg after d.
func fadeStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return statusFadeMsg{} })
}

// setStatusCmd is a Cmd that emits one statusMsg. Used inline by
// palette commands to set busy / show validation errors without
// involving the action pipeline.
func setStatusCmd(level statusLevel, text string) tea.Cmd {
	return func() tea.Msg {
		return statusMsg{entry: statusEntry{level: level, text: text, at: time.Now()}}
	}
}

// runAction is the canonical pattern for any daemon-backed palette
// action: emit a "busy" status, run the RPC, then emit
// actionResultMsg with the outcome. Update converts that into either
// info or error status and triggers a state refresh.
//
//	busyText      — what shows while the RPC is in flight
//	   ("Starting service-template/grpc...")
//	successText   — what shows on completion ("Started ...")
//	actionLabel   — verb-phrase prefix for the error message
//	   ("start service-template/grpc")
func runAction(busyText, successText, actionLabel string, do func() error) tea.Cmd {
	return tea.Batch(
		setStatusCmd(statusBusy, busyText),
		func() tea.Msg {
			if err := do(); err != nil {
				return actionResultMsg{actionLabel: actionLabel, err: err}
			}
			return actionResultMsg{actionLabel: actionLabel, successText: successText}
		},
	)
}

// logsMsg carries log lines to the model. `replace=true` means
// overwrite the buffer (snapshot); `append=true` means append (live
// follow). Defaults to append-only when both flags are zero so older
// callers stay correct.
type logsMsg struct {
	lines   []*anovelv1.LogLine
	replace bool
	append  bool
}

// tickMsg is the periodic refresh trigger.
type tickMsg struct{}

// refreshServicesCmd asks the daemon for a snapshot of every service in
// the default stack (the UI is per-stack for the first cut; phase-13
// adds --stack flag if needed).
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

// followSelectedLogs starts a log-streaming subscription on the
// currently-selected target. Returns a one-shot tea.Cmd that fetches
// the snapshot, AND spawns a background goroutine that follows new
// lines and pushes them via p.Send. The goroutine is bound to
// m.followCancel so a target-switch cleanly cancels the previous
// follower.
func (m *model) followSelectedLogs() tea.Cmd {
	id := m.activeLogID()
	if id == "" {
		return nil
	}
	// Cancel any prior follower so two simultaneous streams don't fan
	// into the same logLines buffer.
	if m.followCancel != nil {
		m.followCancel()
		m.followCancel = nil
	}
	// Snapshot tea.Cmd — runs synchronously on the bubble tea event
	// loop, returns a logsMsg with the full current.log contents.
	snapshot := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		stream, err := m.c.StreamLogs(ctx, id, "", false, anovelv1.LogStream_LOG_STREAM_UNSPECIFIED)
		if err != nil {
			return errMsg{err: err}
		}
		defer func() { _ = stream.Close() }()
		var lines []*anovelv1.LogLine
		for stream.Receive() {
			lines = append(lines, stream.Msg())
			if len(lines) > 500 {
				lines = lines[len(lines)-500:]
			}
		}
		return logsMsg{lines: lines, replace: true}
	}
	// Background follower goroutine — pushes logsMsg{append: true} for
	// each new line via the program's Send. Lives until ctx is
	// cancelled (target switch / quit).
	if m.program != nil {
		ctx, cancel := context.WithCancel(context.Background())
		m.followCancel = cancel
		go func() {
			stream, err := m.c.StreamLogs(ctx, id, "", true /* follow */, anovelv1.LogStream_LOG_STREAM_UNSPECIFIED)
			if err != nil {
				m.program.Send(errMsg{err: err})
				return
			}
			defer func() { _ = stream.Close() }()
			for stream.Receive() {
				if ctx.Err() != nil {
					return
				}
				m.program.Send(logsMsg{lines: []*anovelv1.LogLine{stream.Msg()}, append: true})
			}
		}()
	}
	return snapshot
}

// runPaletteCommand dispatches a `:command` from the palette. Returns a
// tea.Cmd that runs the action asynchronously and refreshes state on
// completion.
func (m *model) runPaletteCommand(input string) tea.Cmd {
	input = strings.TrimPrefix(input, ":")
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}
	verb := parts[0]
	args := parts[1:]
	t := m.activeTarget()
	var svc *anovelv1.Service
	if m.selectedSvc < len(m.services) {
		svc = m.services[m.selectedSvc]
	}
	switch verb {
	case "quit", "q":
		return tea.Quit
	case "refresh":
		return tea.Batch(refreshServicesCmd(m.c), m.followSelectedLogs())
	case "start":
		// :start only addresses targets — a single infra container
		// can't be cold-started in isolation (it might have
		// dependencies on other infra). Use :infra-start for the
		// service-level bring-up; use :restart for a single
		// already-existing infra container.
		if m.activeTabKind() == "infra" {
			return setStatusCmd(statusError,
				":start only works on targets. For infra: use :infra-start (whole service) or :restart (this container)")
		}
		if t == nil {
			return setStatusCmd(statusError, "no active target — select one with ←/→ first")
		}
		mode := anovelv1.Mode_MODE_GO_EXEC
		modeLabel := "go-exec"
		if len(args) > 0 && args[0] == "container" {
			mode = anovelv1.Mode_MODE_CONTAINER
			modeLabel = "container"
		}
		label := "start " + t.GetName()
		return runAction(
			"Starting "+t.GetName()+" ("+modeLabel+")... infra + one-shots will run first",
			"Started "+t.GetName(),
			label,
			func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				_, err := m.c.StartTarget(ctx, t.GetId(), mode)
				return err
			},
		)
	case "kill":
		// Dispatch on active tab kind — targets call KillTarget,
		// infra entries call KillInfraContainer (podman stop just
		// that container, leaves the rest of the service's infra +
		// any running targets alone).
		switch m.activeTabKind() {
		case "target":
			label := "kill " + t.GetName()
			return runAction(
				"Killing "+t.GetName()+"...",
				"Killed "+t.GetName(),
				label,
				func() error {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					_, err := m.c.KillTarget(ctx, t.GetId(), 10*time.Second)
					return err
				},
			)
		case "infra":
			in := m.activeInfra()
			label := "kill infra " + in.GetName()
			return runAction(
				"Stopping infra container "+in.GetName()+"...",
				"Stopped infra "+in.GetName(),
				label,
				func() error {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					_, err := m.c.KillInfraContainer(ctx, in.GetStack(), in.GetService(), in.GetName())
					return err
				},
			)
		default:
			return setStatusCmd(statusError, "no active tab — select one with ←/→ first")
		}
	case "restart":
		switch m.activeTabKind() {
		case "target":
			label := "restart " + t.GetName()
			return runAction(
				"Restarting "+t.GetName()+"...",
				"Restarted "+t.GetName(),
				label,
				func() error {
					ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer cancel()
					_, err := m.c.RestartTarget(ctx, t.GetId(), anovelv1.Mode_MODE_UNSPECIFIED)
					return err
				},
			)
		case "infra":
			in := m.activeInfra()
			label := "restart infra " + in.GetName()
			return runAction(
				"Restarting infra container "+in.GetName()+"...",
				"Restarted infra "+in.GetName(),
				label,
				func() error {
					ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer cancel()
					_, err := m.c.RestartInfraContainer(ctx, in.GetStack(), in.GetService(), in.GetName())
					return err
				},
			)
		default:
			return setStatusCmd(statusError, "no active tab — select one with ←/→ first")
		}
	case "infra-start":
		if svc == nil {
			return setStatusCmd(statusError, "no active service — select one with ↑/↓ first")
		}
		label := "infra-start " + svc.GetName()
		return runAction(
			"Bringing up infra + one-shots for "+svc.GetName()+"...",
			"Infra ready for "+svc.GetName(),
			label,
			func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				_, err := m.c.StartInfra(ctx, "", svc.GetName(), anovelv1.Mode_MODE_GO_EXEC)
				return err
			},
		)
	case "infra-kill":
		if svc == nil {
			return setStatusCmd(statusError, "no active service — select one with ↑/↓ first")
		}
		force := len(args) > 0 && args[0] == "force"
		label := "infra-kill " + svc.GetName()
		busyText := "Tearing down infra for " + svc.GetName() + "..."
		if force {
			busyText = "Force-tearing down infra + targets for " + svc.GetName() + "..."
		}
		return runAction(
			busyText,
			"Infra down for "+svc.GetName(),
			label,
			func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_, err := m.c.KillInfra(ctx, "", svc.GetName(), force)
				return err
			},
		)
	case "volume-backup":
		if svc == nil {
			return setStatusCmd(statusError, "no active service — select one with ↑/↓ first")
		}
		label := "volume-backup " + svc.GetName()
		return runAction(
			"Backing up volumes for "+svc.GetName()+"... (may take a while)",
			"Backed up volumes for "+svc.GetName(),
			label,
			func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				_, err := m.c.BackupVolume(ctx, "", svc.GetName(), "", false)
				return err
			},
		)
	case "topology":
		// :topology — fetches the GetTopology RPC for the active
		// service (or all if none selected) and switches to the
		// dedicated topology view.
		svcName := ""
		if svc != nil {
			svcName = svc.GetName()
		}
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
