package ui

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/jobs"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// TestReport pins what every batch command prints at the end, in order: the
// full output of exactly the selected jobs, the job status lines again when
// that output pushed them out of view, then the headline and one pill per
// outcome.
func TestReport(t *testing.T) {
	t.Parallel()

	ok := jobs.Result{Job: jobs.Job{Name: "a-novel-kit/golib"}, Status: "updated", Output: "master updated"}
	skipped := jobs.Result{Job: jobs.Job{Name: "a-novel/infra"}, Status: "skipped", Output: "diverged"}
	failed := jobs.Result{Job: jobs.Job{Name: "a-novel/service-x"}, Err: errors.New("fetch failed"), Output: "ssh: refused"}
	passed := jobs.Result{Job: jobs.Job{Name: "x", Group: "go"}, Output: "ok x"}

	testCases := []struct {
		name    string
		batch   Batch
		show    func(jobs.Result) bool
		want    []string // in the order they print
		notWant []string
	}{
		{
			name:    "Success/OutcomePills",
			batch:   Batch{Results: []jobs.Result{ok, skipped, ok}, Elapsed: time.Second},
			show:    Failed,
			want:    []string{"✓ SYNC PASSED", "updated 2", "skipped 1", "failed 0", "total 3", "took 1s"},
			notWant: []string{"FAILURES", "master updated", "a-novel-kit/golib"},
		},
		{
			name:    "Success/EmptyStatusReadsPassed",
			batch:   Batch{Results: []jobs.Result{passed}},
			show:    Failed,
			want:    []string{"passed 1"},
			notWant: []string{"OUTPUT"},
		},
		{
			name:  "Error/FailedOutputInFull",
			batch: Batch{Results: []jobs.Result{ok, failed}},
			show:  Failed,
			want: []string{
				"FAILURES", "error: fetch failed", "ssh: refused",
				"✓ a-novel-kit/golib updated", "✗ a-novel/service-x fetch failed",
				"✗ SYNC FAILED", "failed 1",
			},
			notWant: []string{"master updated"},
		},
		{
			name:  "Success/EveryOutputWhenSelected",
			batch: Batch{Results: []jobs.Result{ok, skipped}},
			show:  func(jobs.Result) bool { return true },
			want: []string{
				"OUTPUT", "master updated", "diverged",
				"✓ a-novel-kit/golib updated", "✓ a-novel/infra skipped",
				"✓ SYNC PASSED", "total 2",
			},
		},
		{
			name:  "Error/Aborted",
			batch: Batch{Results: []jobs.Result{ok, {Job: jobs.Job{Name: "late"}, Err: jobs.ErrAborted}}, Aborted: true},
			show:  Failed,
			want:  []string{"error: aborted before completion", "✗ late aborted before completion", "! SYNC ABORTED"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := ansiEscape.ReplaceAllString(Report("SYNC", testCase.batch, testCase.show), "")
			rest := got
			for _, want := range testCase.want {
				_, after, found := strings.Cut(rest, want)
				if !found {
					t.Errorf("report missing %q, or out of order:\n%s", want, got)
					continue
				}
				rest = after
			}
			for _, notWant := range testCase.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("report unexpectedly holds %q:\n%s", notWant, got)
				}
			}
		})
	}
}
