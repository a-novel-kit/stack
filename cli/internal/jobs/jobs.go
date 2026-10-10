// Package jobs runs named units of work concurrently and tracks the live state a
// progress view needs: which jobs run, since when, and the last line each wrote.
package jobs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ErrAborted marks a job the run was cancelled before starting.
var ErrAborted = errors.New("aborted before completion")

// Job is one named unit of work.
type Job struct {
	// Name identifies the job in progress lines and reports.
	Name string
	// Group is an optional category rendered as a tag, such as a build kind.
	Group string
	// Detail is optional secondary text, such as the job's directory.
	Detail string
	// Run does the work and writes its log to out. The status names a
	// successful outcome ("updated", "skipped"); empty reads as "passed".
	Run func(ctx context.Context, out io.Writer) (string, error)
}

// Result is a finished job.
type Result struct {
	Job

	Status   string
	Err      error
	Output   string
	Duration time.Duration
}

// Progress is the live state of a run. Its methods are safe for concurrent use.
type Progress struct {
	// OnStart and OnDone, when set, observe each job from the goroutine that
	// runs it. A job aborted before starting only reaches OnDone.
	OnStart func(i int)
	OnDone  func(i int, r Result)

	mu      sync.Mutex
	started map[int]time.Time
	last    map[int]string
}

// Running reports whether job i runs, and since when.
func (p *Progress) Running(i int) (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	start, ok := p.started[i]
	return start, ok
}

// Count is the number of jobs running.
func (p *Progress) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.started)
}

// Last is the latest line job i wrote, cleaned for a one-line display.
func (p *Progress) Last(i int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last[i]
}

func (p *Progress) start(i int) {
	p.mu.Lock()
	p.started[i] = time.Now()
	p.mu.Unlock()
	if p.OnStart != nil {
		p.OnStart(i)
	}
}

func (p *Progress) finish(i int, r Result) {
	p.mu.Lock()
	delete(p.started, i)
	p.mu.Unlock()
	if p.OnDone != nil {
		p.OnDone(i, r)
	}
}

func (p *Progress) write(i int, line string) {
	if line == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last[i] = line
}

// Run executes list with at most limit jobs at once, or all at once when limit
// is below one, and returns the results in list order. p, which may be nil,
// tracks and observes the run.
//
// Cancelling ctx stops dispatch: jobs still queued finish with [ErrAborted],
// and running jobs see the cancellation through their own context.
func Run(ctx context.Context, list []Job, limit int, p *Progress) []Result {
	if limit < 1 || limit > len(list) {
		limit = len(list)
	}
	if p == nil {
		p = &Progress{}
	}
	p.mu.Lock()
	p.started, p.last = map[int]time.Time{}, map[int]string{}
	p.mu.Unlock()

	results := make([]Result, len(list))
	slots := make(chan struct{}, max(limit, 1))
	var wg sync.WaitGroup
	for i, job := range list {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
		}
		// A slot won in a race with the cancellation is never needed again.
		if ctx.Err() != nil {
			results[i] = Result{Job: job, Err: ErrAborted}
			p.finish(i, results[i])
			continue
		}
		wg.Go(func() {
			defer func() { <-slots }()
			p.start(i)
			results[i] = run(ctx, job, func(line string) { p.write(i, line) })
			p.finish(i, results[i])
		})
	}
	wg.Wait()
	return results
}

// run executes one job, capturing its output and reporting its latest line.
func run(ctx context.Context, job Job, report func(string)) Result {
	start := time.Now()
	var buf bytes.Buffer
	status, err := job.Run(ctx, io.MultiWriter(&buf, &tailWriter{report: report}))
	return Result{
		Job:      job,
		Status:   status,
		Err:      err,
		Output:   strings.TrimRight(buf.String(), "\n"),
		Duration: time.Since(start),
	}
}

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// tailWriter reports the latest output line, complete or in progress, reduced
// to what a terminal would show: text after the last carriage return, without
// ANSI escapes.
type tailWriter struct {
	report func(string)
	rest   []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.rest = append(w.rest, p...)
	if i := bytes.LastIndexByte(w.rest, '\n'); i >= 0 {
		w.report(lastLine(w.rest[:i]))
		w.rest = w.rest[i+1:]
	}
	w.report(lastLine(w.rest))
	return len(p), nil
}

func lastLine(b []byte) string {
	s := string(b)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndexByte(s, '\r'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(ansiSeq.ReplaceAllString(s, ""))
}
