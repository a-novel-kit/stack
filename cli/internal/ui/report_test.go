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

// TestReport pins what every batch command prints at the end: the headline,
// one pill per outcome, and the full output of exactly the selected jobs.
func TestReport(t *testing.T) {
	t.Parallel()

	ok := jobs.Result{Job: jobs.Job{Name: "a-novel-kit/golib"}, Status: "updated", Output: "master updated"}
	skipped := jobs.Result{Job: jobs.Job{Name: "a-novel/infra"}, Status: "skipped", Output: "diverged"}
	failed := jobs.Result{Job: jobs.Job{Name: "a-novel/service-x"}, Err: errors.New("fetch failed"), Output: "ssh: refused"}
	passed := jobs.Result{Job: jobs.Job{Name: "x", Group: "go"}, Output: "ok x"}

	cases := []struct {
		name    string
		batch   Batch
		show    func(jobs.Result) bool
		want    []string
		notWant []string
	}{
		{
			name:    "Success/OutcomePills",
			batch:   Batch{Results: []jobs.Result{ok, skipped, ok}, Elapsed: time.Second},
			show:    Failed,
			want:    []string{"✓ SYNC PASSED", "updated 2", "skipped 1", "failed 0", "total 3", "took 1s"},
			notWant: []string{"FAILURES", "master updated"},
		},
		{
			name:    "Success/EmptyStatusReadsPassed",
			batch:   Batch{Results: []jobs.Result{passed}},
			show:    Failed,
			want:    []string{"passed 1"},
			notWant: []string{"OUTPUT"},
		},
		{
			name:    "Error/FailedOutputInFull",
			batch:   Batch{Results: []jobs.Result{ok, failed}},
			show:    Failed,
			want:    []string{"✗ SYNC FAILED", "failed 1", "FAILURES", "error: fetch failed", "ssh: refused"},
			notWant: []string{"master updated"},
		},
		{
			name:  "Success/EveryOutputWhenSelected",
			batch: Batch{Results: []jobs.Result{ok, skipped}},
			show:  func(jobs.Result) bool { return true },
			want:  []string{"OUTPUT", "master updated", "diverged"},
		},
		{
			name:  "Error/Aborted",
			batch: Batch{Results: []jobs.Result{ok, {Job: jobs.Job{Name: "late"}, Err: jobs.ErrAborted}}, Aborted: true},
			show:  Failed,
			want:  []string{"! SYNC ABORTED", "aborted before completion"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := ansiEscape.ReplaceAllString(Report("SYNC", c.batch, c.show), "")
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Errorf("report missing %q:\n%s", want, got)
				}
			}
			for _, notWant := range c.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("report unexpectedly holds %q:\n%s", notWant, got)
				}
			}
		})
	}
}
