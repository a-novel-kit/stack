package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/jobs"
)

// TestRun pins the runner's contract: results in list order whatever the
// completion order, the concurrency cap, and queued jobs reported aborted once
// the run is cancelled.
func TestRun(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")

	testCases := []struct {
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
				t.Helper()
				for i, r := range results {
					require.Equal(t, fmt.Sprintf("job-%d", i), r.Name)
					require.Equal(t, fmt.Sprintf("job %d", i), r.Output)
				}
				require.ErrorIs(t, results[2].Err, errBoom)
				require.Equal(t, "status-1", results[1].Status)
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
				t.Helper()
				require.Equal(t, 2, peak, "peak concurrency")
			},
		},
		{
			name:        "Error/CancelAbortsQueuedJobs",
			limit:       1,
			jobs:        4,
			cancelAfter: 1,
			run:         func(int, io.Writer) (string, error) { return "", nil },
			check: func(t *testing.T, results []jobs.Result, _ int) {
				t.Helper()
				require.NoError(t, results[0].Err, "the job run before the cancellation")
				// The cancelling job's slot may already have let one more
				// start; everything after it never runs.
				for _, r := range results[2:] {
					require.ErrorIs(t, r.Err, jobs.ErrAborted, r.Name)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			var mu sync.Mutex
			var running, peak, done int
			list := make([]jobs.Job, testCase.jobs)
			for i := range list {
				list[i] = jobs.Job{
					Name: fmt.Sprintf("job-%d", i),
					Run: func(_ context.Context, out io.Writer) (string, error) {
						mu.Lock()
						running++
						peak = max(peak, running)
						mu.Unlock()
						defer func() {
							mu.Lock()
							running--
							mu.Unlock()
						}()
						return testCase.run(i, out)
					},
				}
			}

			progress := &jobs.Progress{OnDone: func(int, jobs.Result) {
				mu.Lock()
				defer mu.Unlock()
				if done++; done == testCase.cancelAfter {
					cancel()
				}
			}}

			results := jobs.Run(ctx, list, testCase.limit, progress)
			require.Len(t, results, testCase.jobs)
			require.Equal(t, testCase.jobs, done, "OnDone runs once per job")
			testCase.check(t, results, peak)
		})
	}
}

// TestProgress pins the live state a view polls: a running job, since when,
// and its latest line as a terminal would show it.
func TestProgress(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		write []string
		want  string
	}{
		{name: "Success/LastCompleteLine", write: []string{"one\ntwo\n"}, want: "two"},
		{name: "Success/LineInProgress", write: []string{"one\n", "tw"}, want: "tw"},
		{name: "Success/CarriageReturnRedraw", write: []string{"10%\r50%\r90%"}, want: "90%"},
		{name: "Success/CarriageReturnAcrossWrites", write: []string{"a\rb", "c\n"}, want: "bc"},
		{name: "Success/AnsiStripped", write: []string{"\x1b[32mok\x1b[0m\n"}, want: "ok"},
		{name: "Success/BlankLineKeepsPrevious", write: []string{"real\n", "\n"}, want: "real"},
		{name: "Success/LongLineEndsAtCap", write: []string{strings.Repeat("x", 1<<20+1), "y"}, want: "y"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			progress := &jobs.Progress{}
			written, release := make(chan struct{}), make(chan struct{})
			job := jobs.Job{Run: func(_ context.Context, out io.Writer) (string, error) {
				for _, w := range testCase.write {
					_, _ = io.WriteString(out, w)
				}
				close(written)
				<-release
				return "", nil
			}}
			// The job holds until this goroutine sampled the live state; the
			// samples are read once Run returns, past the release.
			var running bool
			var count int
			var last string
			go func() {
				<-written
				defer close(release)
				_, running = progress.Running(0)
				count = progress.Count()
				last = progress.Last(0)
			}()
			jobs.Run(t.Context(), []jobs.Job{job}, 1, progress)
			require.True(t, running, "job reported running")
			require.Equal(t, 1, count, "running job count")
			require.Equal(t, testCase.want, last)
			_, running = progress.Running(0)
			require.False(t, running, "finished job still reported running")
			require.Zero(t, progress.Count(), "running job count after the run")
		})
	}
}
