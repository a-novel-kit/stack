package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/jobs"
)

// TestRun pins the runner's contract: results in list order whatever the
// completion order, the concurrency cap, and queued jobs reported aborted once
// the run is cancelled.
func TestRun(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")

	cases := []struct {
		name  string
		limit int
		jobs  int
		// run is each job's body, given its index.
		run func(i int, out io.Writer) (string, error)
		// cancelAfter cancels the run once this many jobs finished; 0 never.
		cancelAfter int
		check       func(t *testing.T, results []jobs.Result, peak int)
	}{
		{
			name:  "Success/ResultsInListOrder",
			limit: 0,
			jobs:  4,
			run: func(i int, out io.Writer) (string, error) {
				// Later jobs finish first.
				time.Sleep(time.Duration(4-i) * 5 * time.Millisecond)
				_, _ = fmt.Fprintf(out, "job %d\n", i)
				if i == 2 {
					return "", errBoom
				}
				return fmt.Sprintf("status-%d", i), nil
			},
			check: func(t *testing.T, results []jobs.Result, _ int) {
				for i, r := range results {
					if want := fmt.Sprintf("job-%d", i); r.Name != want {
						t.Errorf("results[%d].Name = %q, want %q", i, r.Name, want)
					}
					if want := fmt.Sprintf("job %d", i); r.Output != want {
						t.Errorf("results[%d].Output = %q, want %q", i, r.Output, want)
					}
				}
				if !errors.Is(results[2].Err, errBoom) || results[1].Status != "status-1" {
					t.Errorf("results lost their outcome: %+v", results)
				}
			},
		},
		{
			name:  "Success/LimitCapsConcurrency",
			limit: 2,
			jobs:  6,
			run: func(int, io.Writer) (string, error) {
				time.Sleep(10 * time.Millisecond)
				return "", nil
			},
			check: func(t *testing.T, _ []jobs.Result, peak int) {
				if peak != 2 {
					t.Errorf("peak concurrency = %d, want 2", peak)
				}
			},
		},
		{
			name:        "Error/CancelAbortsQueuedJobs",
			limit:       1,
			jobs:        4,
			cancelAfter: 1,
			run:         func(int, io.Writer) (string, error) { return "", nil },
			check: func(t *testing.T, results []jobs.Result, _ int) {
				if results[0].Err != nil {
					t.Errorf("the job run before the cancellation failed: %v", results[0].Err)
				}
				// The cancelling job's slot may already have let one more
				// start; everything after it never runs.
				for _, r := range results[2:] {
					if !errors.Is(r.Err, jobs.ErrAborted) {
						t.Errorf("%s: err = %v, want ErrAborted", r.Name, r.Err)
					}
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			var running, peak atomic.Int32
			list := make([]jobs.Job, c.jobs)
			for i := range list {
				list[i] = jobs.Job{
					Name: fmt.Sprintf("job-%d", i),
					Run: func(_ context.Context, out io.Writer) (string, error) {
						n := running.Add(1)
						defer running.Add(-1)
						for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
						}
						return c.run(i, out)
					},
				}
			}

			var mu sync.Mutex
			var done int
			progress := &jobs.Progress{OnDone: func(int, jobs.Result) {
				mu.Lock()
				defer mu.Unlock()
				if done++; done == c.cancelAfter {
					cancel()
				}
			}}

			results := jobs.Run(ctx, list, c.limit, progress)
			if len(results) != c.jobs {
				t.Fatalf("got %d results, want %d", len(results), c.jobs)
			}
			if done != c.jobs {
				t.Errorf("OnDone ran %d times, want once per job (%d)", done, c.jobs)
			}
			c.check(t, results, int(peak.Load()))
		})
	}
}

// TestProgress pins the live state a view polls: a running job, since when,
// and its latest line as a terminal would show it.
func TestProgress(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		write []string
		want  string
	}{
		{name: "LastCompleteLine", write: []string{"one\ntwo\n"}, want: "two"},
		{name: "LineInProgress", write: []string{"one\n", "tw"}, want: "tw"},
		{name: "CarriageReturnRedraw", write: []string{"10%\r50%\r90%"}, want: "90%"},
		{name: "AnsiStripped", write: []string{"\x1b[32mok\x1b[0m\n"}, want: "ok"},
		{name: "BlankLineKeepsPrevious", write: []string{"real\n", "\n"}, want: "real"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			progress := &jobs.Progress{}
			written, release := make(chan struct{}), make(chan struct{})
			job := jobs.Job{Run: func(_ context.Context, out io.Writer) (string, error) {
				for _, w := range c.write {
					_, _ = io.WriteString(out, w)
				}
				close(written)
				<-release
				return "", nil
			}}
			go func() {
				<-written
				defer close(release)
				if _, ok := progress.Running(0); !ok || progress.Count() != 1 {
					t.Errorf("job not reported running")
				}
				if got := progress.Last(0); got != c.want {
					t.Errorf("Last = %q, want %q", got, c.want)
				}
			}()
			jobs.Run(t.Context(), []jobs.Job{job}, 1, progress)
			if _, ok := progress.Running(0); ok || progress.Count() != 0 {
				t.Errorf("finished job still reported running")
			}
		})
	}
}
