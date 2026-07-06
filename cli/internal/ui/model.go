// Package ui is the Bubble Tea front-end shared by `a-novel build`, `test`, and
// `run`: a branded, keyboard-driven flow of three phases — select targets, run
// them, read the report. lipgloss handles all presentation; this file owns the
// state machine.
package ui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/a-novel-kit/stack/cli/internal/build"
	"github.com/a-novel-kit/stack/cli/internal/detect"
)

type phase int

const (
	phaseSelect phase = iota
	phaseRun
	phaseReport
)

// rowKind distinguishes a group heading (toggles the whole kind) from an
// individual target line.
type rowKind int

const (
	rowGroup rowKind = iota
	rowTarget
)

type row struct {
	kind rowKind
	// groupKey is the opaque group identifier this row belongs to. For
	// kind-grouping (build/test) it is the Kind string ("go"/"pnpm"/...). For
	// service-grouping (run, multi-service) it is the service name. Both
	// rowGroup (heading) and rowTarget (member) carry it, so toggling /
	// counting / labeling all key off the same value regardless of mode.
	groupKey string
	target   int // index into Model.targets when kind == rowTarget
}

// buildDoneMsg is delivered when one target's subprocess has finished.
type buildDoneMsg struct{ res build.Result }

// Model is the root Bubble Tea model. Construct it with [New].
type Model struct {
	ctx     context.Context
	version string
	verb    Verb // build / test — drives all user-facing wording

	targets  []detect.Target
	selected map[string]bool // keyed by detect.Target.ID()
	rows     []row
	cursor   int

	phase phase

	spinner spinner.Model
	queue   []detect.Target // resolved selection, dispatch order
	results []build.Result  // completion order (as each finishes)

	// Parallel run state. maxPar bounds how many builds run at once; procs is
	// the GOMAXPROCS handed to each spawned target (NumCPU/maxPar) so the
	// concurrent go-test targets share the machine rather than each grabbing
	// every core; nextIdx is the next queue entry to dispatch; running maps an
	// in-flight target's ID to when it started (for its live timer).
	maxPar  int
	procs   int
	keep    bool           // leave env-backed targets' compose envs up after the run
	timeout time.Duration  // per-target deadline (0 = none)
	live    *build.LiveLog // shared latest-line tail (pointer; survives value copies)
	nextIdx int
	running map[string]time.Time

	// Wall-clock run timing: runStart is stamped when the first build is
	// dispatched; runElapsed is frozen when the last one finishes. The report
	// shows runElapsed as "took" — under parallelism the sum of per-target
	// durations would badly overstate how long the user actually waited.
	runStart   time.Time
	runElapsed time.Duration

	width   int
	aborted bool // true if the user ctrl+c'd mid-run

	// selectOnly makes the model a pure target picker: `enter` records the
	// selection and quits instead of starting the build pool. `run` uses this
	// so it shares build/test's exact selection UI, then hands the chosen
	// targets to its own long-lived dashboard.
	selectOnly bool

	// runModeLabel is the resolved run mode ("container" / "live"), surfaced
	// in the SELECT TARGETS title so it is unambiguous which list the user
	// is looking at. Empty for build/test pickers.
	runModeLabel string
}

// New builds a Model from discovered targets. Every target starts selected —
// the common case is "build everything", and opting out is one keystroke.
//
// jobs is the maximum number of builds to run concurrently once the user
// confirms; jobs <= 0 means the resource-aware default (see defaultJobs).
// Parallelism is interactive-only; the non-interactive path stays strictly
// sequential by design.
func New(ctx context.Context, version string, verb Verb, targets []detect.Target, jobs int, timeout time.Duration, keep bool) Model {
	if jobs <= 0 {
		jobs = defaultJobs()
	}
	if jobs < 1 {
		jobs = 1
	}
	// Split the machine's cores across the concurrent targets so N parallel
	// `go test` runs use ≈ NumCPU threads in total instead of N × NumCPU.
	procs := max(1, runtime.NumCPU()/jobs)
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colBrand)

	// Default selection: everything on, minus a small hand-curated set of
	// services that start off in a global (multi-service) run — see
	// defaultOffByService.
	distinctSvc := map[string]bool{}
	for _, t := range targets {
		distinctSvc[t.Service] = true
	}
	multiService := len(distinctSvc) > 1
	selected := make(map[string]bool, len(targets))
	for _, t := range targets {
		selected[t.ID()] = !defaultOffInGlobal(verb, multiService, t)
	}

	m := Model{
		ctx:      ctx,
		version:  version,
		verb:     verb,
		targets:  targets,
		selected: selected,
		spinner:  sp,
		phase:    phaseSelect,
		maxPar:   jobs,
		procs:    procs,
		keep:     keep,
		timeout:  timeout,
		live:     build.NewLiveLog(),
		running:  map[string]time.Time{},
	}
	m.rows = m.buildRows()
	return m
}

// defaultJobs is the resource-aware parallelism used when the user passes no
// -j. NumCPU/4 (min 2) keeps a multi-target run from serializing on a big box
// while leaving headroom: paired with the per-target GOMAXPROCS cap
// (NumCPU/jobs) the concurrent go-test targets stay within ≈ NumCPU threads,
// and only a handful boot their container constellations at once — a large
// drop from the old NumCPU default (e.g. 8 not 32 on a 32-core machine).
func defaultJobs() int {
	return max(2, runtime.NumCPU()/4)
}

// defaultOffByService lists the run-mode services that start UNSELECTED when
// the picker opens in global mode. Kept small: service-template is boilerplate
// / fixture, not something a stack-root run should include unless asked for
// explicitly.
var defaultOffByService = map[string]bool{
	"service-template": true,
}

// defaultOffInGlobal reports whether a target should be DESELECTED by default
// — only fires for the run verb in multi-service (global) scope; single-repo
// runs and build/test runs always start with everything selected.
func defaultOffInGlobal(verb Verb, multiService bool, t detect.Target) bool {
	if verb.Base != VerbRun.Base || !multiService {
		return false
	}
	return defaultOffByService[t.Service]
}

// NewSelect builds a target picker that reuses build/test's exact selection
// UI, then quits on `enter` returning the chosen targets via [Model.Selected].
// It carries no run state (no ctx/jobs/timeout) — `run` drives the actual
// processes itself. `runMode` ("container" / "live" / "") is displayed in
// the picker title so the user can tell at a glance which list this is.
func NewSelect(version string, verb Verb, runMode string, targets []detect.Target) Model {
	m := New(context.Background(), version, verb, targets, 1, 0, false)
	m.selectOnly = true
	m.runModeLabel = runMode
	return m
}

// Selected is the targets the user confirmed in select-only mode (empty if
// aborted or nothing was picked). Pair with [Model.Aborted].
func (m Model) Selected() []detect.Target { return m.queue }

// groupByService is the picker grouping mode for the run verb — multiple
// services in scope (global run) means the user wants services as the
// top-level grouping, not kinds. build/test keep kind-grouping.
func (m Model) groupByService() bool {
	return m.verb.Base == VerbRun.Base
}

// groupOfTarget returns the opaque group key for a target under the current
// grouping mode (service name or Kind string).
func (m Model) groupOfTarget(t detect.Target) string {
	if m.groupByService() {
		return t.Service
	}
	return string(t.Kind)
}

// buildRows flattens the sorted targets into displayable rows, inserting a
// group heading whenever the group key changes. The slice is already sorted
// such that group members are consecutive (service-first sort for run,
// kind-first for build/test — see detect's sort).
func (m Model) buildRows() []row {
	var rows []row
	last := ""
	for i, t := range m.targets {
		key := m.groupOfTarget(t)
		if key != last {
			rows = append(rows, row{kind: rowGroup, groupKey: key})
			last = key
		}
		rows = append(rows, row{kind: rowTarget, groupKey: key, target: i})
	}
	return rows
}

// Results / Aborted expose terminal state so the caller can print an
// authoritative plain-text report (with full logs) after the TUI tears down.
// Results are returned in queue (dispatch) order, not completion order, so the
// report is deterministic regardless of which parallel build finished first.
func (m Model) Results() []build.Result { return m.orderedResults() }
func (m Model) Aborted() bool           { return m.aborted }

// Elapsed is the real wall-clock run time: frozen when the run completed, or
// measured to now if it was aborted mid-flight. Zero if no build ever started.
func (m Model) Elapsed() time.Duration {
	switch {
	case m.runElapsed > 0:
		return m.runElapsed
	case !m.runStart.IsZero():
		return time.Since(m.runStart)
	default:
		return 0
	}
}

// orderedResults sorts a copy of the completion-ordered results back into the
// queue order the user saw at selection time. When the run was aborted, every
// queued target that never produced a result (in-flight when killed, or never
// started) is surfaced as a failed result — an abort leaves no work silently
// unaccounted for; its in-flight output was lost with the killed goroutine, so
// it reads as a failure with no captured output.
func (m Model) orderedResults() []build.Result {
	pos := make(map[string]int, len(m.queue))
	for i, t := range m.queue {
		pos[t.ID()] = i
	}
	out := append([]build.Result(nil), m.results...)
	if m.aborted {
		done := make(map[string]bool, len(m.results))
		for _, r := range m.results {
			done[r.Target.ID()] = true
		}
		for _, t := range m.queue {
			if !done[t.ID()] {
				out = append(out, build.Result{
					Target:  t,
					Success: false,
					ExitErr: errors.New("aborted before completion"),
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return pos[out[i].Target.ID()] < pos[out[j].Target.ID()]
	})
	return out
}

func (m Model) Init() tea.Cmd { return nil }

// buildCmd runs one target off the Bubble Tea event loop. Each runs in its own
// goroutine (Bubble Tea spawns a goroutine per Cmd), so several of these in a
// tea.Batch execute in parallel; output is captured per-result so the
// interleaving on disk never reaches the screen.
func (m Model) buildCmd(t detect.Target) tea.Cmd {
	ctx := m.ctx
	timeout := m.timeout
	live := m.live
	procs := m.procs
	keep := m.keep
	return func() tea.Msg {
		return buildDoneMsg{res: build.Run(ctx, t, timeout, live, procs, keep)}
	}
}

// dispatch starts as many queued targets as spare capacity allows (keeping
// len(running) <= maxPar) and returns their commands batched. It mutates the
// receiver, so call it via pointer on a model about to be returned from
// Update. Returns nil when nothing new could start.
func (m *Model) dispatch() tea.Cmd {
	var cmds []tea.Cmd
	for len(m.running) < m.maxPar && m.nextIdx < len(m.queue) {
		t := m.queue[m.nextIdx]
		m.nextIdx++
		m.running[t.ID()] = time.Now()
		cmds = append(cmds, m.buildCmd(t))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case spinner.TickMsg:
		if m.phase == phaseRun {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case buildDoneMsg:
		m.results = append(m.results, msg.res)
		delete(m.running, msg.res.Target.ID())
		// Backfill the freed slot, then check for overall completion.
		cmd := m.dispatch()
		if len(m.results) == len(m.queue) {
			m.runElapsed = time.Since(m.runStart)
			m.phase = phaseReport
			return m, nil
		}
		return m, cmd

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// Key names as Bubble Tea's KeyMsg.String() reports them — named so the
// literals are not repeated across the three phase handlers.
const (
	keyCtrlC = "ctrl+c"
	keyEsc   = "esc"
	keyQ     = "q"
	keyEnter = "enter"
	keyLeft  = "left"
	keyRight = "right"
	keyUp    = "up"
	keyDown  = "down"
)

// isAbortKey reports whether k is one of the "get me out" keys.
func isAbortKey(k string) bool {
	return k == keyCtrlC || k == keyEsc || k == keyQ
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.phase {
	case phaseSelect:
		return m.handleSelectKey(msg)
	case phaseRun:
		// The only interaction during a run is to bail out. Results gathered
		// so far are preserved and surfaced by the caller.
		if isAbortKey(msg.String()) {
			m.aborted = true
			return m, tea.Quit
		}
		return m, nil
	case phaseReport:
		if k := msg.String(); isAbortKey(k) || k == keyEnter {
			return m, tea.Quit
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleSelectKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyCtrlC, keyEsc, keyQ:
		m.aborted = true
		return m, tea.Quit

	case keyUp, "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case keyDown, "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}

	// Bubble Tea v2 reports the spacebar as "space" (not " ") from
	// KeyPressMsg.String(); match that so space still toggles selection.
	case "space", "x":
		m.toggleAt(m.cursor)

	case "g":
		// Toggle the entire group the cursor currently sits in.
		m.toggleGroup(m.rows[m.cursor].groupKey)

	case "a":
		m.toggleAll()

	case "enter":
		m.queue = m.selectedTargets()
		if len(m.queue) == 0 {
			return m, nil // nothing selected — ignore, keep choosing
		}
		if m.selectOnly {
			// Picker mode: hand the selection back to the caller (run).
			return m, tea.Quit
		}
		m.phase = phaseRun
		m.nextIdx = 0
		m.running = map[string]time.Time{}
		m.runStart = time.Now()
		// Fill the pool up to maxPar; the spinner tick drives the live timers.
		return m, tea.Batch(m.spinner.Tick, m.dispatch())
	}
	return m, nil
}

// deps returns the IDs of targets t requires. Every schema-touching target
// in the same module needs migrations to have run first — that's true in
// LIVE mode (Go rest/grpc/rotate-keys talking to the DB) and equally in
// CONTAINER mode (the compose service we're about to start expects an
// applied schema). migrations itself doesn't pull anything.
func (m Model) deps(t detect.Target) []string {
	if m.verb.Base != VerbRun.Base {
		return nil
	}
	if t.Kind == detect.KindGo && t.Name == "migrations" {
		return nil
	}
	var ids []string
	for _, o := range m.targets {
		if o.Kind == detect.KindGo && o.Name == "migrations" && o.RelDir == t.RelDir {
			ids = append(ids, o.ID())
		}
	}
	return ids
}

// applyDeps is the selection closure: anything selected drags its
// dependencies on too. Idempotent and cheap — run after every toggle so a
// dependency can never be left unselected under a selected dependant (the
// "always auto-select dependencies" rule).
func (m *Model) applyDeps() {
	for _, t := range m.targets {
		if m.selected[t.ID()] {
			for _, d := range m.deps(t) {
				m.selected[d] = true
			}
		}
	}
}

func (m *Model) toggleAt(idx int) {
	r := m.rows[idx]
	if r.kind == rowGroup {
		m.toggleGroup(r.groupKey)
		return
	}
	id := m.targets[r.target].ID()
	m.selected[id] = !m.selected[id]
	m.applyDeps()
}

// toggleGroup flips a whole group (kind or service): if every member is
// already selected it clears them, otherwise it selects them all
// ("ensure on, then off" — predictable regardless of mixed state).
func (m *Model) toggleGroup(key string) {
	allOn := true
	for _, t := range m.targets {
		if m.groupOfTarget(t) == key && !m.selected[t.ID()] {
			allOn = false
			break
		}
	}
	for _, t := range m.targets {
		if m.groupOfTarget(t) == key {
			m.selected[t.ID()] = !allOn
		}
	}
	m.applyDeps()
}

func (m *Model) toggleAll() {
	allOn := true
	for _, t := range m.targets {
		if !m.selected[t.ID()] {
			allOn = false
			break
		}
	}
	for _, t := range m.targets {
		m.selected[t.ID()] = !allOn
	}
	m.applyDeps()
}

func (m Model) selectedCount() int {
	n := 0
	for _, t := range m.targets {
		if m.selected[t.ID()] {
			n++
		}
	}
	return n
}

// selectedTargets returns the chosen targets in display order.
func (m Model) selectedTargets() []detect.Target {
	var out []detect.Target
	for _, t := range m.targets {
		if m.selected[t.ID()] {
			out = append(out, t)
		}
	}
	return out
}

func (m Model) View() tea.View {
	var b strings.Builder
	b.WriteString(Banner(m.version))
	b.WriteString("\n\n")

	switch m.phase {
	case phaseSelect:
		b.WriteString(m.viewSelect())
	case phaseRun:
		b.WriteString(m.viewRun())
	case phaseReport:
		b.WriteString(m.viewReport())
	}
	// Bubble Tea v2 makes the alternate screen a property of the View, not a
	// NewProgram option; every frame opts in so cmd/a-novel's
	// report-after-teardown flow still discards the TUI's last frame.
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m Model) viewSelect() string {
	var b strings.Builder

	w := m.width
	if w <= 0 {
		w = termWidth()
	}
	title := "select targets"
	if m.runModeLabel != "" {
		title += " · " + m.runModeLabel
	}
	b.WriteString(section(title, colGold, w) + "\n")
	b.WriteString(para(
		"Everything is selected by default. Toggle what to "+m.verb.Base+
			", then press enter. Group headings toggle the whole kind.", w) + "\n\n")

	for i, r := range m.rows {
		cursor := "  "
		if i == m.cursor {
			cursor = styleSel.Render(glyphCursor) + " "
		}

		if r.kind == rowGroup {
			box := m.groupCheckbox(r.groupKey)
			n := m.groupCount(r.groupKey)
			fmt.Fprintf(&b, "%s%s %s %s\n",
				cursor, box,
				styleGroup.Render(m.groupLabel(r.groupKey)),
				styleMuted.Render(fmt.Sprintf("(%d)", n)),
			)
			continue
		}

		t := m.targets[r.target]
		box := glyphUnchecked
		// No build outcome yet at selection time, so the name uses the default
		// terminal color (bold when selected, dim when not) — never a kind
		// color, and never green/red which would falsely imply a result.
		name := lipgloss.NewStyle().Bold(true).Render(t.Name)
		if m.selected[t.ID()] {
			box = styleSel.Render(glyphChecked)
		} else {
			box = styleMuted.Render(box)
			name = styleMuted.Render(t.Name)
		}
		loc := relLabel(t.RelDir)
		// cursor is 2 cols wide ("  " or "▸ "); the extra 2 spaces nest the
		// target one level under its group heading. The detail line is indented
		// to sit directly beneath the name (2+2+box+space = 6).
		fmt.Fprintf(&b, "%s  %s %s %s\n", cursor, box, name, styleMuted.Render(loc))
		fmt.Fprintf(&b, "      %s\n", styleMuted.Render(t.Detail))
	}

	b.WriteString("\n")
	b.WriteString(rule(w) + "\n")
	b.WriteString(styleGold.Render(fmt.Sprintf("%d of %d selected", m.selectedCount(), len(m.targets))))
	b.WriteString("\n")
	b.WriteString(styleHelp.Render(
		"↑/↓ move · space toggle · g toggle group · a toggle all · enter " +
			m.verb.Base + " · q quit"))
	b.WriteString("\n")
	return b.String()
}

// groupCheckbox renders a heading's tri-state checkbox: filled when every
// member of the group (kind or service) is selected, empty when none are,
// and a half-filled "partial" glyph (gold, "mixed — look here") otherwise.
func (m Model) groupCheckbox(key string) string {
	all, anySel, seen := true, false, false
	for _, t := range m.targets {
		if m.groupOfTarget(t) != key {
			continue
		}
		seen = true
		if m.selected[t.ID()] {
			anySel = true
		} else {
			all = false
		}
	}
	switch {
	case seen && all:
		return styleSel.Render(glyphChecked)
	case anySel:
		return styleGold.Render(glyphPartial)
	default:
		return styleMuted.Render(glyphUnchecked)
	}
}

func (m Model) groupCount(key string) int {
	n := 0
	for _, t := range m.targets {
		if m.groupOfTarget(t) == key {
			n++
		}
	}
	return n
}

// groupLabel is the display name of a group key. For service-grouping (run)
// it is the service name as-is; for kind-grouping (build/test) it is the
// pretty Kind label (e.g. "Go modules").
func (m Model) groupLabel(key string) string {
	if m.groupByService() {
		return key
	}
	return kindLabel(detect.Kind(key))
}

func (m Model) viewRun() string {
	var b strings.Builder

	w := m.width
	if w <= 0 {
		w = termWidth()
	}
	b.WriteString(section(m.verb.Ing, colGold, w) + "\n")
	b.WriteString(para(fmt.Sprintf(
		"Up to %d %s(s) run at once; each finishes independently. Output is "+
			"captured per target and shown in the report. Press q to abort.",
		m.maxPar, m.verb.Base), w) + "\n\n")

	for _, res := range m.results {
		b.WriteString("  " + statusLine(res) + "\n")
	}

	// In-flight targets, in queue order so the list is stable as builds come
	// and go. Each carries its own live timer (time since it started); the
	// trailing position keeps the spinner/name columns from shifting.
	for _, t := range m.queue {
		start, ok := m.running[t.ID()]
		if !ok {
			continue
		}
		elapsed := styleMuted.Render("(" + time.Since(start).Round(1e7).String() + ")")
		fmt.Fprintf(&b, "  %s %s %s %s %s %s\n",
			m.spinner.View(),
			m.verb.Ing,
			kindTag(t.Kind),
			t.Name, // in-flight: default color, outcome not yet known
			styleMuted.Render(relLabel(t.RelDir)),
			elapsed,
		)
		// Most recent output line, dimmed under the target — a brief sense of
		// what each command is doing so a stalled one is obvious early.
		if last := m.live.Line(t.ID()); last != "" {
			fmt.Fprintf(&b, "    %s\n", styleMuted.Render(clip(last, w-6)))
		}
	}

	b.WriteString("\n")
	b.WriteString(styleGold.Render(fmt.Sprintf(
		"%d / %d complete · %d running",
		len(m.results), len(m.queue), len(m.running))))
	b.WriteString("\n")
	b.WriteString(styleHelp.Render("q abort"))
	b.WriteString("\n")
	return b.String()
}

// statusLine is the one-line completed-target result in the live run view.
// The kind tag precedes the name and the duration trails the line (matching
// the in-flight timer's position), so finished rows read
// "✓ GO     service-json-keys (root) (1.98s)" with no shifting left column.
func statusLine(r build.Result) string {
	mark := styleOK.Render(glyphOK)
	if !r.Success {
		mark = styleErr.Render(glyphFail)
	}
	return fmt.Sprintf("%s %s %s %s %s",
		mark,
		kindTag(r.Target.Kind),
		targetName(r.Target.Name, r.Success),
		styleMuted.Render(r.Target.RelDir),
		styleMuted.Render("("+r.Duration.Round(1e7).String()+")"),
	)
}

func (m Model) viewReport() string {
	var b strings.Builder
	// Queue order, not the order parallel builds happened to finish, so the
	// report is identical run to run.
	ordered := m.orderedResults()
	s := build.Summarize(ordered)

	// Width budget: terminal width minus the 2-space gutter every block sits
	// under (fall back to the static default before the first WindowSizeMsg).
	w := m.width
	if w <= 0 {
		w = termWidth()
	}
	cw := w - gutter

	// A failed build is the one thing that must grab the eye immediately, so
	// the headline uses the critical color; the per-failure panels stay the
	// calmer error orange.
	headline := styleOK.Render("✓ " + m.verb.Upper + " PASSED")
	lead := "Every selected target passed."
	if s.Failed > 0 {
		headline = styleCrit.Render("✗ " + m.verb.Upper + " FAILED")
		lead = "One or more targets failed — see the panels below; full logs print on quit."
	}
	if m.aborted {
		headline = styleWarn.Render("! " + m.verb.Upper + " ABORTED")
		lead = "Interrupted — results below are partial."
	}
	b.WriteString("  " + headline + "\n")
	b.WriteString(indentBlock(para(lead, cw)) + "\n\n")

	var failColor color.Color = colMuted
	if s.Failed > 0 {
		failColor = colCrit
	}
	// indentBlock, not "  "+: the pill row is 3 lines tall, so a string
	// prefix would only shift the top border and leave the box body at
	// column 0.
	b.WriteString(indentBlock(pillRow(
		pill("passed", strconv.Itoa(s.Passed), colOK),
		pill("failed", strconv.Itoa(s.Failed), failColor),
		pill("total", strconv.Itoa(s.Total), colGold),
		pill("took", m.Elapsed().Round(1e7).String(), colAccent),
	)) + "\n\n")

	b.WriteString(indentBlock(section("results", colGold, cw)) + "\n\n")
	b.WriteString(indentBlock(resultsTable(ordered)) + "\n")

	if cv := CoverageView(ordered, cw); cv != "" {
		b.WriteString("\n" + indentBlock(cv) + "\n")
	}

	if s.Failed > 0 {
		b.WriteString("\n" + indentBlock(section("failures", colCrit, cw)) + "\n\n")
		b.WriteString(indentBlock(para(
			"Last lines of each failed build — the full, untruncated logs print to "+
				"scrollback when you quit.", cw)) + "\n")
		for _, res := range ordered {
			if res.Success {
				continue
			}
			// tail > 0 → preview; the authoritative full log is printed by
			// RenderTextReport after the TUI tears down.
			b.WriteString("\n" + indentBlock(failurePanel(res, 20, cw)) + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(indentBlock(rule(cw)) + "\n")
	b.WriteString(styleHelp.Render("  q quit"))
	b.WriteString("\n")
	return b.String()
}
