// `a-novel test` and `a-novel build`: discover the targets under a directory,
// let the user pick them or run them all, then report. Both share one flow and
// differ only in wording and discovery.

package cli

import (
	"bufio"
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

	"github.com/a-novel-kit/stack/cli/internal/build"
	"github.com/a-novel-kit/stack/cli/internal/detect"
	"github.com/a-novel-kit/stack/cli/internal/jobs"
	"github.com/a-novel-kit/stack/cli/internal/ui"
)

// capability is `test` or `build`.
type capability struct {
	name    string
	looks   string // what discovery scans for, quoted when nothing is found
	detect  func(dir string) ([]detect.Target, error)
	short   string
	long    string
	example string
}

var (
	testCapability = capability{
		name:   "test",
		looks:  "a go.mod (Go tests) or a package.json with test* scripts (pnpm)",
		detect: detect.DetectTests,
		short:  "Run Go and pnpm tests, spinning up their podman-compose envs",
		long: `Discovers a Go test target per module and every pnpm "test"/"test:*" script
under the working directory, lets you pick which to run, runs them in parallel
and prints a report with coverage and the full output of every failure.

A Go module shipping builds/podman-compose.go[.<path>].test.yaml gets one target
per file, scoped to that path. A target with a compose env has it brought up
(host ports allocated, health awaited) around the run and torn down after, so
independent envs run concurrently.

With -y or without a terminal, every target runs without the picker,
sequentially unless --jobs says otherwise.`,
		example: `  a-novel test                  # pick, then run in parallel
  a-novel test -t go            # only Go test targets
  a-novel test -y               # run all tests, no prompt (CI-safe)
  a-novel test --dry-run        # show targets and their envs, run nothing`,
	}
	buildCapability = capability{
		name:   "build",
		looks:  "a go.mod (Go), a package.json with build* scripts (pnpm), or a root Dockerfile / builds/*.Dockerfile (Podman)",
		detect: detect.Detect,
		short:  "Build Go modules, pnpm scripts and Podman images",
		long: `Discovers a Go module per go.mod, every pnpm "build"/"build:*" script, a root
Dockerfile and each builds/*.Dockerfile under the working directory, lets you
pick what to build, builds it in parallel and prints a report.

With -y or without a terminal, every target builds without the picker,
sequentially unless --jobs says otherwise.`,
		example: `  a-novel build                 # pick, then build in parallel
  a-novel build -t go,podman    # only Go and Podman targets
  a-novel build -j 4            # cap parallelism at 4
  a-novel build -y              # build everything, no prompt (CI-safe)
  a-novel build --dry-run       # show what would build, run nothing`,
	}
)

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
			if cmd.Flags().Changed("jobs") && o.jobs < 1 {
				return usageError(fmt.Errorf("--jobs: want a positive integer, got %d", o.jobs))
			}
			if o.timeout < 0 {
				return usageError(fmt.Errorf("--timeout: want a duration like 10m / 30s / 0, got %s", o.timeout))
			}
			return runCapability(cmd.Context(), c, o)
		},
	}
	f := cmd.Flags()
	f.SortFlags = false
	f.StringVarP(&o.dir, "dir", "C", ".", "directory to scan")
	f.StringVarP(&o.types, "type", "t", "", "comma-separated kinds to keep: go,pnpm,podman (default: all)")
	f.BoolVarP(&o.yes, "yes", "y", false, "skip the picker and run every target (CI-safe)")
	f.IntVarP(&o.jobs, "jobs", "j", 0, "max targets run at once (default: NumCPU/4, min 2; 1 with -y or no terminal)")
	f.DurationVarP(&o.timeout, "timeout", "T", 10*time.Minute, "per-target deadline, e.g. 10m, 30s, or 0 to disable")
	f.BoolVar(&o.dryRun, "dry-run", false, "list the detected targets and their envs, then exit")
	if c.name == testCapability.name {
		f.BoolVar(&o.noCover, "no-cover", false, "skip coverage (collected and reported by default)")
		f.BoolVar(&o.keep, "keep", false, "leave the test env up afterwards; the next run reuses it (skips postgres init)")
	}
	return cmd
}

func runCapability(ctx context.Context, c capability, o capabilityOpts) error {
	kinds, err := parseKinds(o.types)
	if err != nil {
		return usageError(err)
	}
	// A missing directory or one outside an a-novel repository fails before any
	// walk, so a wrong path never looks like an empty scan.
	absDir, _ := filepath.Abs(o.dir)
	if info, statErr := os.Stat(o.dir); statErr != nil || !info.IsDir() {
		return usageError(fmt.Errorf("cannot scan %q: not an accessible directory", absDir))
	}
	if err := detect.RepoGuard(o.dir); err != nil {
		return usageError(err)
	}
	targets, err := c.detect(o.dir)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}
	if kinds != nil {
		targets = slices.DeleteFunc(targets, func(t detect.Target) bool { return !slices.Contains(kinds, t.Kind) })
	}
	if c.name == testCapability.name && !o.noCover {
		targets = withCoverage(targets)
	}
	if len(targets) == 0 {
		scope := ""
		if kinds != nil {
			scope = " matching --type"
		}
		return usageError(fmt.Errorf("no %s targets%s found under %s\n  Looked for %s.\n"+
			"  Run from a project root, or pass --dir <path>", c.name, scope, absDir, c.looks))
	}
	if o.dryRun {
		fmt.Print(ui.DryRunView(c.name, targets))
		return nil
	}

	// SIGINT cancels in-flight subprocesses cleanly; once cancelled, a second
	// one falls through to the default handler and exits.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, stop)

	interactive := !o.yes && stdoutIsTTY()
	if err := preflight(ctx, c.name, targets, interactive && stdinIsTTY(), o.keep); err != nil {
		return err
	}
	if interactive {
		chosen, ok, err := ui.Pick(c.name, targets)
		if err != nil {
			return fmt.Errorf("picker: %w", err)
		}
		if !ok {
			return &ExitError{Code: exitAborted}
		}
		targets = chosen
	}

	limit := o.jobs
	switch {
	case limit > 0:
	case interactive:
		limit = max(2, runtime.NumCPU()/4)
	default:
		limit = 1
	}
	// Concurrent go-test targets split the cores between them; a lone target
	// keeps the whole machine.
	procs := 0
	if limit > 1 {
		procs = max(1, runtime.NumCPU()/limit)
	}
	list := make([]jobs.Job, len(targets))
	for i, t := range targets {
		list[i] = build.Job(t, o.timeout, procs, o.keep)
	}

	fmt.Println(ui.Banner())
	fmt.Println()
	batch := ui.RunJobs(ctx, list, limit, interactive)
	fmt.Print(ui.Report(strings.ToUpper(c.name), batch, ui.Failed))
	return batchError(batch)
}

// parseKinds turns "go,podman" into a kind list, nil meaning every kind.
func parseKinds(v string) ([]detect.Kind, error) {
	if v == "" {
		return nil, nil
	}
	var kinds []detect.Kind
	for _, part := range strings.Split(v, ",") {
		if k := detect.Kind(strings.ToLower(strings.TrimSpace(part))); slices.Contains(detect.Kinds, k) {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 {
		return nil, fmt.Errorf("--type: no valid kinds in %q (want go,pnpm,podman)", v)
	}
	return kinds, nil
}

// withCoverage rewrites every test target for coverage mode. A Go target keeps
// its own package selectors and only gains -cover, so every test package runs;
// generated and test-support trees are dropped from the reported mean, not from
// the run (see ui.CoverageView). A pnpm target gets `-- --coverage`, and
// vitest's v8 text reporter prints a table the report extracts verbatim.
func withCoverage(targets []detect.Target) []detect.Target {
	for i := range targets {
		t := &targets[i]
		switch {
		case len(t.Args) == 0:
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

// preflight refuses to run on top of a test env left up by an aborted run. Both
// answers to its prompt tear down only the conflicting projects; clean then
// continues and abort stops. Without a terminal it cleans and continues, so CI
// and piped runs self-heal. With keep, a leftover env is the point: the run
// adopts its warm containers and data.
func preflight(ctx context.Context, verb string, targets []detect.Target, canPrompt, keep bool) error {
	conflicts := build.EnvConflicts(ctx, targets)
	switch {
	case len(conflicts) == 0:
		return nil
	case keep:
		fmt.Fprintln(os.Stderr, ui.Warn.Render("Reusing the existing test environment (--keep); "+
			"its data and schema persist from the previous run."))
		return nil
	}

	fmt.Fprintln(os.Stderr, ui.EnvConflictView(verb, conflicts))
	proceed := true
	if canPrompt {
		fmt.Fprint(os.Stderr, ui.Gold.Render("Clean it and continue, or abort? [c]lean / [a]bort: "))
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		proceed = answer == "c" || answer == "clean"
	} else {
		fmt.Fprintln(os.Stderr, ui.Warn.Render("No prompt available — cleaning the stale environment "+
			"(scoped to this project only) and continuing."))
	}
	for _, c := range conflicts {
		fmt.Fprintln(os.Stderr, ui.Muted.Render("  cleaning "+c.Env.ID+" …"))
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
func batchError(b ui.Batch) error {
	switch {
	case b.Aborted:
		return &ExitError{Code: exitAborted}
	case slices.ContainsFunc(b.Results, func(r jobs.Result) bool { return r.Err != nil }):
		return &ExitError{Code: 1}
	}
	return nil
}

// usageError marks a bad invocation, exit status 2.
func usageError(err error) error {
	return &ExitError{Code: exitUsage, Err: err}
}
