package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/a-novel-kit/stack/cli/internal/jobs"
)

// Batch is a finished run of jobs.
type Batch struct {
	Results []jobs.Result
	Aborted bool
	Elapsed time.Duration
}

// RunJobs runs list with at most limit jobs at once and prints a status line as
// each one finishes. live keeps a view of the running jobs under those lines,
// which needs a terminal; without it each job also prints a line as it starts.
func RunJobs(ctx context.Context, list []jobs.Job, limit int, live bool) Batch {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	start := time.Now()
	var b Batch
	if live {
		b = runLive(ctx, cancel, list, limit)
	} else {
		var mu sync.Mutex
		say := func(s string) {
			mu.Lock()
			defer mu.Unlock()
			fmt.Println(s)
		}
		b.Results = jobs.Run(ctx, list, limit, &jobs.Progress{
			OnStart: func(i int) { say(startLine(list[i])) },
			OnDone:  func(_ int, r jobs.Result) { say(statusLine(r)) },
		})
	}
	b.Aborted = b.Aborted || ctx.Err() != nil
	b.Elapsed = time.Since(start)
	return b
}

// doneMsg delivers a finished job; finishedMsg follows the last one.
type (
	doneMsg struct {
		i int
		r jobs.Result
	}
	finishedMsg struct{}
)

// runLive renders the running jobs inline under the finished ones. Quitting
// cancels the run and returns at once, the unfinished jobs reported aborted.
func runLive(ctx context.Context, cancel context.CancelFunc, list []jobs.Job, limit int) Batch {
	progress := &jobs.Progress{}
	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(lipgloss.NewStyle().Foreground(colBrand)))
	p := tea.NewProgram(liveModel{list: list, progress: progress, results: make([]*jobs.Result, len(list)), spinner: sp, cancel: cancel})

	// Events relay through one goroutine, so status lines print in completion
	// order and the last one lands before finishedMsg quits the program. The
	// buffer keeps a job from ever blocking on a program that already quit.
	events := make(chan tea.Msg, len(list)+1)
	progress.OnDone = func(i int, r jobs.Result) { events <- doneMsg{i, r} }
	go func() {
		jobs.Run(ctx, list, limit, progress)
		events <- finishedMsg{}
		close(events)
	}()
	go func() {
		for msg := range events {
			if done, ok := msg.(doneMsg); ok {
				p.Println(statusLine(done.r))
			}
			p.Send(msg)
		}
	}()

	final, err := p.Run()
	m, ok := final.(liveModel)
	if err != nil || !ok {
		cancel()
		return Batch{Results: abortedResults(list, nil), Aborted: true}
	}
	return Batch{Results: abortedResults(list, m.results), Aborted: m.aborted}
}

// abortedResults fills the jobs that never reported with an aborted result.
func abortedResults(list []jobs.Job, results []*jobs.Result) []jobs.Result {
	out := make([]jobs.Result, len(list))
	for i, job := range list {
		if i < len(results) && results[i] != nil {
			out[i] = *results[i]
		} else {
			out[i] = jobs.Result{Job: job, Err: jobs.ErrAborted}
		}
	}
	return out
}

type liveModel struct {
	list     []jobs.Job
	progress *jobs.Progress
	results  []*jobs.Result
	done     int
	spinner  spinner.Model
	width    int
	aborted  bool
	quitting bool
	cancel   context.CancelFunc
}

func (m liveModel) Init() tea.Cmd { return m.spinner.Tick }

func (m liveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case doneMsg:
		m.results[msg.i] = &msg.r
		m.done++
	case finishedMsg:
		m.quitting = true
		return m, tea.Quit
	case tea.InterruptMsg:
		return m.abort()
	case tea.KeyPressMsg:
		if k := msg.String(); k == "ctrl+c" || k == "q" || k == "esc" {
			return m.abort()
		}
	}
	return m, nil
}

// abort cancels the run and quits without waiting for the running jobs.
func (m liveModel) abort() (tea.Model, tea.Cmd) {
	m.aborted, m.quitting = true, true
	m.cancel()
	return m, tea.Quit
}

func (m liveModel) View() tea.View {
	// The last frame stays on screen in inline mode; an empty one leaves only
	// the finished jobs' lines behind.
	if m.quitting {
		return tea.NewView("")
	}
	w := m.width
	if w <= 0 {
		w = termWidth()
	}
	var b strings.Builder
	for i, job := range m.list {
		start, ok := m.progress.Running(i)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "%s %s %s\n", m.spinner.View(), jobLabel(job),
			Muted.Render("("+time.Since(start).Round(time.Second).String()+")"))
		// The latest output line, dimmed under the job, makes a stall visible.
		if last := m.progress.Last(i); last != "" {
			b.WriteString("    " + Muted.Render(Truncate(last, w-4)) + "\n")
		}
	}
	b.WriteString(Gold.Render(fmt.Sprintf("%d / %d done · %d running", m.done, len(m.list), m.progress.Count())) +
		Muted.Render(" · q abort"))
	return tea.NewView(b.String())
}

// jobLabel is a job's group tag, name and detail.
func jobLabel(job jobs.Job) string {
	label := job.Name
	if job.Group != "" {
		label = groupTag(job.Group) + " " + label
	}
	if job.Detail != "" {
		label += " " + Muted.Render(relLabel(job.Detail))
	}
	return label
}

// startLine announces a job in the plain output.
func startLine(job jobs.Job) string {
	return Muted.Render("▸") + " " + jobLabel(job)
}

// statusLine is a finished job: its mark, label, outcome and duration.
func statusLine(r jobs.Result) string {
	mark, outcome := OK.Render(glyphOK), Muted.Render(r.Status)
	if r.Err != nil {
		mark, outcome = Err.Render(glyphFail), Err.Render(firstLine(r.Err.Error()))
	}
	line := mark + " " + jobLabel(r.Job)
	if r.Status != "" || r.Err != nil {
		line += " " + outcome
	}
	return line + " " + Muted.Render("("+r.Duration.Round(10*time.Millisecond).String()+")")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
