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
// failure during refresh).
type errMsg struct{ err error }

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
	t := m.activeTarget()
	if t == nil {
		return nil
	}
	id := t.GetId()
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
		if t == nil {
			m.cmdHint = "no active target"
			return nil
		}
		mode := anovelv1.Mode_MODE_GO_EXEC
		if len(args) > 0 && args[0] == "container" {
			mode = anovelv1.Mode_MODE_CONTAINER
		}
		return paletteRPC(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			_, err := m.c.StartTarget(ctx, t.GetId(), mode)
			return err
		}, m.c, "started "+t.GetName())
	case "kill":
		if t == nil {
			m.cmdHint = "no active target"
			return nil
		}
		return paletteRPC(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err := m.c.KillTarget(ctx, t.GetId(), 10*time.Second)
			return err
		}, m.c, "killed "+t.GetName())
	case "restart":
		if t == nil {
			m.cmdHint = "no active target"
			return nil
		}
		return paletteRPC(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			_, err := m.c.RestartTarget(ctx, t.GetId(), anovelv1.Mode_MODE_UNSPECIFIED)
			return err
		}, m.c, "restarted "+t.GetName())
	case "infra-start":
		if svc == nil {
			m.cmdHint = "no active service"
			return nil
		}
		return paletteRPC(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			_, err := m.c.StartInfra(ctx, "", svc.GetName(), anovelv1.Mode_MODE_GO_EXEC)
			return err
		}, m.c, "infra-up for "+svc.GetName())
	case "infra-kill":
		if svc == nil {
			m.cmdHint = "no active service"
			return nil
		}
		force := len(args) > 0 && args[0] == "force"
		return paletteRPC(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err := m.c.KillInfra(ctx, "", svc.GetName(), force)
			return err
		}, m.c, "infra-down for "+svc.GetName())
	case "volume-backup":
		if svc == nil {
			m.cmdHint = "no active service"
			return nil
		}
		return paletteRPC(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			_, err := m.c.BackupVolume(ctx, "", svc.GetName(), "", false)
			return err
		}, m.c, "backed up volumes for "+svc.GetName())
	case "topology":
		// :topology — fetches the GetTopology RPC for the active
		// service (or all if none selected) and stashes the result in
		// m.cmdHint for the renderer. Phase-13 polish: dedicated
		// topology view mode that replaces the main panes; for now
		// the rendered text shows in the footer.
		svcName := ""
		if svc != nil {
			svcName = svc.GetName()
		}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			resp, err := m.c.GetTopology(ctx, "", svcName)
			if err != nil {
				return errMsg{err: err}
			}
			return topologyMsg{rendered: resp.GetRendered()}
		}
	default:
		m.cmdHint = "unknown command: " + verb
		return nil
	}
}

// topologyMsg carries a GetTopology RPC response into the model so
// the renderer can swap the right pane to topology view.
type topologyMsg struct{ rendered string }

// paletteRPC wraps an RPC call as a tea.Cmd that refreshes services on
// completion (so the user sees the new state immediately).
func paletteRPC(do func() error, c *rpc.Client, successMsg string) tea.Cmd {
	return func() tea.Msg {
		if err := do(); err != nil {
			return errMsg{err: err}
		}
		// Trigger a refresh.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		resp, err := c.ListServices(ctx, "")
		if err != nil {
			return errMsg{err: err}
		}
		_ = successMsg // could surface in cmdHint via a custom msg; deferred
		return servicesMsg{services: resp.GetServices()}
	}
}
