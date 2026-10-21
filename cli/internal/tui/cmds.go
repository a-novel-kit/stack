package tui

import (
	"context"
	"iter"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Status texts of the palette verbs that need a selection.
const (
	noServiceText = "no active service — select one with ↑/↓ first"
	noTabText     = "no active tab — select one with ←/→ first"
	noTargetText  = "no active target — select one with ←/→ first"
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

// setStatusCmd returns a Cmd that sets the status bar to text at level.
func setStatusCmd(level statusLevel, text string) tea.Cmd {
	return func() tea.Msg {
		return statusMsg{entry: newStatus(level, text)}
	}
}

// newStatus stamps a status entry with the current time, which the info fade
// measures from.
func newStatus(level statusLevel, text string) statusEntry {
	return statusEntry{level: level, text: text, at: time.Now()}
}

// errOf drops an RPC's response and keeps its error, so an action returns the
// call directly.
func errOf[T any](_ T, err error) error { return err }

// action is a daemon-backed palette action and the status texts around it.
type action struct {
	// busy is the status while do runs.
	busy string
	// success is the info status after do succeeds.
	success string
	// label prefixes the error status, such as "start grpc".
	label string
	// timeout bounds do.
	timeout time.Duration
	// do calls the daemon.
	do func(context.Context) error
}

// run shows a.busy, runs a.do under a.timeout, then emits an actionResultMsg
// that Update renders as a.success, or as the error prefixed by a.label.
func (a action) run() tea.Cmd {
	return tea.Batch(
		setStatusCmd(statusBusy, a.busy),
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
			defer cancel()
			return actionResultMsg{actionLabel: a.label, successText: a.success, err: a.do(ctx)}
		},
	)
}

// logLinesMsg carries the lines a log follower received since its last
// message. gen is the generation of that follower.
type logLinesMsg struct {
	lines []*anovelv1.StreamLogsResponse
	gen   int
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
	if m.followCancel != nil {
		m.followCancel()
		m.followCancel = nil
	}
	m.followGen++
	id := m.activeLogID()
	if id == "" || m.program == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.followCancel = cancel
	go m.forwardLogs(ctx, id, m.followGen)
}

// forwardLogs sends the followed log of id to the program, tagged with the
// follower generation gen, until ctx ends or the stream does. Each message
// carries every line received since the last one, so the replay of a long log
// renders once per batch, not once per line.
func (m *model) forwardLogs(ctx context.Context, id string, gen int) {
	stream, err := m.c.StreamLogs(ctx, id, "", true /* follow */, anovelv1.LogStream_LOG_STREAM_UNSPECIFIED)
	if err != nil {
		m.program.Send(errMsg{err: err})
		return
	}

	lines := make(chan *anovelv1.StreamLogsResponse, maxLogLines)
	go receiveLogs(ctx, stream, lines)

	for ln := range lines {
		batch := []*anovelv1.StreamLogsResponse{ln}
		for len(lines) > 0 {
			batch = append(batch, <-lines)
		}
		m.program.Send(logLinesMsg{lines: batch, gen: gen})
	}
}

// receiveLogs copies stream into lines until ctx ends or the stream does, then
// closes lines.
func receiveLogs(ctx context.Context, stream iter.Seq2[*anovelv1.StreamLogsResponse, error], lines chan<- *anovelv1.StreamLogsResponse) {
	defer close(lines)
	for ln, err := range stream {
		if err != nil || ctx.Err() != nil {
			return
		}
		lines <- ln
	}
}

// runPaletteCommand dispatches a `:command` from the palette. The returned
// tea.Cmd runs the action asynchronously and refreshes state on completion.
func (m *model) runPaletteCommand(input string) tea.Cmd {
	parts := strings.Fields(strings.TrimPrefix(input, ":"))
	if len(parts) == 0 {
		return nil
	}
	verb, args := parts[0], parts[1:]
	switch verb {
	case "quit", "q":
		return tea.Quit
	case "refresh":
		m.followSelectedLogs()
		return refreshServicesCmd(m.c)
	case "start":
		return m.startCmd(args)
	case "kill":
		return m.killCmd()
	case "restart":
		return m.restartCmd()
	case "infra-start":
		return m.infraStartCmd()
	case "infra-kill":
		return m.infraKillCmd(args)
	case "volume-backup":
		return m.volumeBackupCmd()
	case "topology":
		return m.topologyCmd()
	default:
		return setStatusCmd(statusError, "unknown command: :"+verb+"  (press ? for the full list)")
	}
}

// startCmd starts the active target, in container mode with a `container`
// argument. It addresses targets only: a single infra container cannot be
// cold-started in isolation, since it may depend on other infra.
func (m *model) startCmd(args []string) tea.Cmd {
	if m.activeInfra() != nil {
		return setStatusCmd(statusError,
			":start only works on targets. For infra: use :infra-start (whole service) or :restart (this container)")
	}
	t := m.activeTarget()
	if t == nil {
		return setStatusCmd(statusError, noTargetText)
	}
	mode, modeLabel := anovelv1.Mode_MODE_GO_EXEC, modeGoExec
	if len(args) > 0 && args[0] == modeContainer {
		mode, modeLabel = anovelv1.Mode_MODE_CONTAINER, modeContainer
	}
	return action{
		busy:    "Starting " + t.GetName() + " (" + modeLabel + ")... infra + one-shots will run first",
		success: "Started " + t.GetName(),
		label:   "start " + t.GetName(),
		timeout: time.Minute,
		do:      func(ctx context.Context) error { return errOf(m.c.StartTarget(ctx, t.GetId(), mode)) },
	}.run()
}

// killCmd stops the active target, or the active infra container alone,
// leaving the rest of the service's infra and targets up.
func (m *model) killCmd() tea.Cmd {
	if t := m.activeTarget(); t != nil {
		return action{
			busy:    "Killing " + t.GetName() + "...",
			success: "Killed " + t.GetName(),
			label:   "kill " + t.GetName(),
			timeout: 30 * time.Second,
			do:      func(ctx context.Context) error { return errOf(m.c.KillTarget(ctx, t.GetId(), 10*time.Second)) },
		}.run()
	}
	if in := m.activeInfra(); in != nil {
		return action{
			busy:    "Stopping infra container " + in.GetName() + "...",
			success: "Stopped infra " + in.GetName(),
			label:   "kill infra " + in.GetName(),
			timeout: 30 * time.Second,
			do: func(ctx context.Context) error {
				return errOf(m.c.KillInfraContainer(ctx, in.GetStack(), in.GetService(), in.GetName()))
			},
		}.run()
	}
	return setStatusCmd(statusError, noTabText)
}

// restartCmd restarts the active target, or the active infra container in
// place.
func (m *model) restartCmd() tea.Cmd {
	if t := m.activeTarget(); t != nil {
		return action{
			busy:    "Restarting " + t.GetName() + "...",
			success: "Restarted " + t.GetName(),
			label:   "restart " + t.GetName(),
			timeout: time.Minute,
			do: func(ctx context.Context) error {
				return errOf(m.c.RestartTarget(ctx, t.GetId(), anovelv1.Mode_MODE_UNSPECIFIED))
			},
		}.run()
	}
	if in := m.activeInfra(); in != nil {
		return action{
			busy:    "Restarting infra container " + in.GetName() + "...",
			success: "Restarted infra " + in.GetName(),
			label:   "restart infra " + in.GetName(),
			timeout: time.Minute,
			do: func(ctx context.Context) error {
				return errOf(m.c.RestartInfraContainer(ctx, in.GetStack(), in.GetService(), in.GetName()))
			},
		}.run()
	}
	return setStatusCmd(statusError, noTabText)
}

// infraStartCmd brings up the active service's infra and runs its one-shots.
func (m *model) infraStartCmd() tea.Cmd {
	svc := m.activeService()
	if svc == nil {
		return setStatusCmd(statusError, noServiceText)
	}
	return action{
		busy:    "Bringing up infra + one-shots for " + svc.GetName() + "...",
		success: "Infra ready for " + svc.GetName(),
		label:   "infra-start " + svc.GetName(),
		timeout: 5 * time.Minute,
		do: func(ctx context.Context) error {
			return errOf(m.c.StartInfra(ctx, "", svc.GetName(), anovelv1.Mode_MODE_GO_EXEC))
		},
	}.run()
}

// infraKillCmd tears down the active service's infra, cascading to its
// targets with a `force` argument.
func (m *model) infraKillCmd(args []string) tea.Cmd {
	svc := m.activeService()
	if svc == nil {
		return setStatusCmd(statusError, noServiceText)
	}
	force := len(args) > 0 && args[0] == "force"
	busyText := "Tearing down infra for " + svc.GetName() + "..."
	if force {
		busyText = "Force-tearing down infra + targets for " + svc.GetName() + "..."
	}
	return action{
		busy:    busyText,
		success: "Infra down for " + svc.GetName(),
		label:   "infra-kill " + svc.GetName(),
		timeout: 30 * time.Second,
		do:      func(ctx context.Context) error { return errOf(m.c.KillInfra(ctx, "", svc.GetName(), force)) },
	}.run()
}

// volumeBackupCmd backs up the active service's volumes.
func (m *model) volumeBackupCmd() tea.Cmd {
	svc := m.activeService()
	if svc == nil {
		return setStatusCmd(statusError, noServiceText)
	}
	return action{
		busy:    "Backing up volumes for " + svc.GetName() + "... (may take a while)",
		success: "Backed up volumes for " + svc.GetName(),
		label:   "volume-backup " + svc.GetName(),
		timeout: 5 * time.Minute,
		do: func(ctx context.Context) error {
			return errOf(m.c.BackupVolume(ctx, "", svc.GetName(), "", false))
		},
	}.run()
}

// topologyCmd fetches the dependency graph of the active service, or of the
// whole stack when none is selected: GetName is nil-safe, and an empty name
// asks for every service.
func (m *model) topologyCmd() tea.Cmd {
	svcName := m.activeService().GetName()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		resp, err := m.c.GetTopology(ctx, "", svcName)
		if err != nil {
			return statusMsg{entry: newStatus(statusError, "topology: "+err.Error())}
		}
		return topologyMsg{rendered: resp.GetRendered()}
	}
}

// topologyMsg carries a GetTopology RPC response into the model so
// the renderer can swap the right pane to topology view.
type topologyMsg struct{ rendered string }
