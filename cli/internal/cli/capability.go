package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/batchview"
	"github.com/a-novel-kit/stack/cli/internal/build"
	"github.com/a-novel-kit/stack/cli/internal/detect"
	"github.com/a-novel-kit/stack/cli/internal/jobs"
)

// capability is `test` or `build`. Both discover the targets under a
// directory, let the user pick them or run them all, then report, and differ
// only in wording and discovery.
type capability struct {
	name    string
	looks   string // what discovery scans for, quoted when nothing is found
	detect  func(ctx context.Context, dir string) ([]detect.Target, error)
	short   string
	long    string
	example string
}

// capabilityOpts holds the flags `test` and `build` share, and the test-only
// --no-cover and --keep.
type capabilityOpts struct {
	dir     string
	types   string
	yes     bool
	jobs    int
	timeout time.Duration
	dryRun  bool
	noCover bool
	keep    bool
}

func newCapabilityCmd(c capability) *cobra.Command {
	var o capabilityOpts
	cmd := &cobra.Command{
		Use:     c.name,
		Short:   c.short,
		Long:    c.long,
		Example: c.example,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCapability(cmd, c, o)
		},
	}
	f := cmd.Flags()
	f.SortFlags = false
	f.StringVarP(&o.dir, "dir", "C", ".", "directory to scan")
	kinds := make([]string, len(detect.Kinds))
	for i, kind := range detect.Kinds {
		kinds[i] = string(kind)
	}
	addEnumFlag(cmd, &enumFlag{value: &o.types, allowed: kinds, list: true}, "type", "t",
		"comma-separated kinds to keep: go,pnpm,podman (default: all)")
	f.BoolVarP(&o.yes, "yes", "y", false, "skip the picker and run every target (CI-safe)")
	f.VarP(countFlag{&o.jobs}, "jobs", "j", "max targets run at once (default: NumCPU/4, min 2; 1 with -y or no terminal)")
	f.DurationVarP(&o.timeout, "timeout", "T", 10*time.Minute, "per-target deadline, e.g. 10m, 30s, or 0 to disable")
	f.BoolVar(&o.dryRun, "dry-run", false, "list the detected targets and their envs, then exit")
	if c.name == testCapability.name {
		f.BoolVar(&o.noCover, "no-cover", false, "skip coverage (collected and reported by default)")
		f.BoolVar(&o.keep, "keep", false, "leave the test env up afterwards; the next run reuses it (skips postgres init)")
	}
	return cmd
}

// runCapability detects, picks and runs c's targets, then reports, on the
// command's own streams. An unset --jobs lets jobLimit decide.
func runCapability(cmd *cobra.Command, c capability, o capabilityOpts) error {
	out := cmd.OutOrStdout()
	if o.timeout < 0 {
		return usageError(fmt.Errorf("--timeout: want a duration like 10m / 30s / 0, got %s", o.timeout))
	}
	targets, err := detectTargets(cmd.Context(), c, o)
	if err != nil {
		return err
	}
	if o.dryRun {
		_, _ = fmt.Fprint(out, batchview.DryRunView(c.name, targets))
		return nil
	}

	// SIGINT cancels in-flight subprocesses cleanly; once cancelled, a second
	// one falls through to the default handler and exits.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, stop)

	release, err := build.ClaimEnvs(targets)
	if err != nil {
		return err
	}
	defer release()

	interactive := !o.yes && stdoutIsTTY()
	if err := preflight(ctx, cmd, c.name, targets, interactive && stdinIsTTY(), o.keep); err != nil {
		return err
	}
	if interactive {
		chosen, ok, err := batchview.Pick(c.name, targets)
		if err != nil {
			return fmt.Errorf("picker: %w", err)
		}
		if !ok {
			return &ExitError{Code: exitAborted}
		}
		targets = chosen
	}

	limit := jobLimit(o.jobs, interactive)
	// Concurrent go-test targets split the cores between them; a lone target
	// keeps the whole machine.
	var procs int
	if limit > 1 {
		procs = max(1, runtime.NumCPU()/limit)
	}
	list := make([]jobs.Job, len(targets))
	for i, t := range targets {
		list[i] = build.Job(t, o.timeout, procs, o.keep)
	}

	_, _ = fmt.Fprintln(out, batchview.Banner()+"\n")
	batch := batchview.RunJobs(ctx, out, list, limit, interactive)
	_, _ = fmt.Fprint(out, batchview.Report(strings.ToUpper(c.name), batch, batchview.Failed))
	return batchError(batch)
}

// detectTargets scans o.dir for c's targets of the kinds --type keeps. A
// missing directory or one outside an a-novel repository fails before any
// walk, so a wrong path never looks like an empty scan.
func detectTargets(ctx context.Context, c capability, o capabilityOpts) ([]detect.Target, error) {
	absDir, _ := filepath.Abs(o.dir)
	if info, statErr := os.Stat(o.dir); statErr != nil || !info.IsDir() {
		return nil, usageError(fmt.Errorf("cannot scan %q: not an accessible directory", absDir))
	}
	if err := detect.RepoGuard(ctx, o.dir); err != nil {
		return nil, usageError(err)
	}
	targets, err := c.detect(ctx, o.dir)
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}
	if o.types != "" {
		kinds := strings.Split(o.types, ",")
		targets = slices.DeleteFunc(targets, func(t detect.Target) bool { return !slices.Contains(kinds, string(t.Kind)) })
	}
	if c.name == testCapability.name && !o.noCover {
		targets = withCoverage(targets)
	}
	if len(targets) == 0 {
		var scope string
		if o.types != "" {
			scope = " matching --type"
		}
		return nil, usageError(fmt.Errorf("no %s targets%s found under %s\n  Looked for %s.\n"+
			"  Run from a project root, or pass --dir <path>", c.name, scope, absDir, c.looks))
	}
	return targets, nil
}

// jobLimit is how many targets run at once: --jobs when set, a quarter of the
// CPUs (at least two) behind the picker, otherwise one.
func jobLimit(jobs int, interactive bool) int {
	switch {
	case jobs > 0:
		return jobs
	case interactive:
		return max(2, runtime.NumCPU()/4)
	default:
		return 1
	}
}

// withCoverage rewrites every test target for coverage mode. A Go target keeps
// its package selectors and gains -cover, so every test package runs. The
// report leaves generated and test-support trees out (see batchview.CoverageView). A
// pnpm target gains `-- --coverage`, whose vitest table the report shows
// verbatim.
func withCoverage(targets []detect.Target) []detect.Target {
	for i := range targets {
		t := &targets[i]
		if len(t.Args) == 0 {
			continue
		}
		switch {
		case t.Kind == detect.KindGo && t.Args[0] == testCapability.name:
			t.Args = append([]string{testCapability.name, "-cover"}, t.Args[1:]...)
			t.Detail = "go test -cover " + strings.Join(t.Args[2:], " ")
		case t.Kind == detect.KindPnpm:
			t.Args = append(slices.Clone(t.Args), "--", "--coverage")
			t.Detail += "  ·  --coverage"
		}
	}
	return targets
}

// preflight refuses to run on top of a test env left up by an earlier run. The
// caller holds every env's claim (see build.ClaimEnvs), so a conflicting env
// belongs to this checkout and to a run that has ended, never to a live one.
// Both answers to its prompt tear down only the conflicting projects; clean
// then continues and abort stops. Without a terminal it cleans and continues,
// so piped and agent runs self-heal. With keep, a leftover env is the point:
// the run adopts its warm containers and data.
func preflight(ctx context.Context, cmd *cobra.Command, verb string, targets []detect.Target, canPrompt, keep bool) error {
	errOut := cmd.ErrOrStderr()
	conflicts := build.EnvConflicts(ctx, targets)
	switch {
	case len(conflicts) == 0:
		return nil
	case keep:
		_, _ = fmt.Fprintln(errOut, batchview.Warn.Render("Reusing the existing test environment (--keep); "+
			"its data and schema persist from the previous run."))
		return nil
	}

	_, _ = fmt.Fprintln(errOut, batchview.EnvConflictView(verb, conflicts))
	proceed := true
	if canPrompt {
		_, _ = fmt.Fprint(errOut, batchview.Gold.Render("Clean it and continue, or abort? [c]lean / [a]bort: "))
		answer := readAnswer(cmd.InOrStdin())
		proceed = answer == "c" || answer == "clean"
	} else {
		_, _ = fmt.Fprintln(errOut, batchview.Warn.Render("No prompt available: cleaning the stale environment "+
			"(scoped to this checkout only) and continuing."))
	}
	for _, c := range conflicts {
		_, _ = fmt.Fprintln(errOut, batchview.Muted.Render("  cleaning "+c.Env.ID+" …"))
		// Detached: teardown must complete even if the run is aborting.
		_ = build.TearDown(context.WithoutCancel(ctx), c.Env)
	}
	if !proceed {
		return &ExitError{Code: exitAborted}
	}
	return nil
}

// batchError turns a finished batch into the command's exit status: 130 when
// interrupted, 1 when a job failed. The report already explains either.
func batchError(b batchview.Batch) error {
	switch {
	case b.Aborted:
		return &ExitError{Code: exitAborted}
	case slices.ContainsFunc(b.Results, func(r jobs.Result) bool { return r.Err != nil }):
		return &ExitError{Code: 1}
	}
	return nil
}
