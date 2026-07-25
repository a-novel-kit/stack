// Command a-novel is the A-Novel storyverse build tool: a single CLI for local
// development. It runs standalone capabilities such as test and build, and
// fronts a long-lived background daemon that supervises run targets. The
// daemon-backed verbs reach it over a unix socket via connect-rpc.
//
// Dispatch is Cobra-based: every subcommand is a *cobra.Command attached to the
// root. The standalone `test` and `build` commands are wrapped via
// internal/cli's LegacyHandlers because they run their own flag parsers.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/a-novel-kit/stack/cli/internal/build"
	anovelcli "github.com/a-novel-kit/stack/cli/internal/cli"
	"github.com/a-novel-kit/stack/cli/internal/detect"
	"github.com/a-novel-kit/stack/cli/internal/ui"
	"github.com/a-novel-kit/stack/cli/internal/update"
	"github.com/a-novel-kit/stack/cli/internal/version"
)

// Exit codes — distinct so CI and wrapper scripts can react precisely.
const (
	exitOK      = 0
	exitFailure = 1   // at least one build failed
	exitUsage   = 2   // bad invocation
	exitAborted = 130 // user interrupted before completing a run (128+SIGINT)
)

// cmdTest is the test subcommand name, shared with the `go test` subcommand
// token.
const cmdTest = "test"

// coverFlag is go test's coverage flag, added to every Go target in coverage mode.
const coverFlag = "-cover"

func main() {
	// Pin the compose provider for every `podman compose` this CLI runs:
	// `podman compose` prefers docker-compose when it is installed, which drags
	// in the Docker-compatible socket. The setting stays inside this process
	// tree — the daemon re-exec runs main() too, and every compose call builds
	// its env from os.Environ. The second variable silences the provider banner
	// so it stays out of captured compose output.
	_ = os.Setenv("PODMAN_COMPOSE_PROVIDER", "podman-compose")
	_ = os.Setenv("PODMAN_COMPOSE_WARNING_LOGS", "false")

	args, sandbox, sandboxErr := anovelcli.SandboxArgs(os.Args[1:])
	if sandboxErr != nil {
		fmt.Fprintf(os.Stderr, "a-novel: %v\n", sandboxErr)
		os.Exit(exitUsage)
	}
	if sandbox {
		sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		err := anovelcli.RunSandbox(sigCtx, args, os.Stdin, os.Stdout, os.Stderr)
		stop()
		if err != nil {
			var exitErr *anovelcli.ExitError
			if errors.As(err, &exitErr) {
				if message := strings.TrimSpace(err.Error()); message != "" {
					fmt.Fprintln(os.Stderr, message)
				}
				os.Exit(exitErr.Code)
			}
			fmt.Fprintf(os.Stderr, "a-novel --sandbox: %v\n", err)
			os.Exit(exitFailure)
		}
		os.Exit(exitOK)
	}

	root := anovelcli.NewRoot(anovelcli.LegacyHandlers{
		Test:  legacyTest,
		Build: legacyBuild,
	})
	err := root.Execute()

	// Best-effort "newer release available" notice, skipped for the detached
	// daemon re-exec, which has no user to notify. It goes to stderr so it never
	// corrupts stdout that callers parse or eval (e.g. `run env`).
	if !anovelcli.IsDaemonReexec(os.Args) {
		update.Notify(os.Stderr, version.String())
	}

	if err != nil {
		// ExitError carries a legacy command's explicit exit code through
		// Cobra's error path.
		var exitErr *anovelcli.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		// Cobra already printed the error, and SilenceUsage keeps the usage
		// dump out of every failure.
		os.Exit(exitFailure)
	}
	os.Exit(exitOK)
}

// legacyTest runs the `test` capability path, intercepting -h/--help before it
// reaches runCapability's own flag parsing.
func legacyTest(args []string) int {
	if wantsHelp(args) {
		fmt.Println(ui.CommandHelpView(version.String(), cmdTest))
		return exitOK
	}
	return runCapability(args, ui.VerbTest, detect.DetectTests)
}

func legacyBuild(args []string) int {
	if wantsHelp(args) {
		fmt.Println(ui.CommandHelpView(version.String(), "build"))
		return exitOK
	}
	return runCapability(args, ui.VerbBuild, detect.Detect)
}

// wantsHelp reports whether a build/test invocation asks for that command's
// help. Only the -h/--help flags count; `a-novel help <command>` is the
// command-style entry point.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// buildOpts is the parsed `build` / `test` invocation.
type buildOpts struct {
	dir      string
	types    map[detect.Kind]bool // nil means "all kinds"
	yes      bool
	dryRun   bool
	help     bool
	jobs     int           // max parallel builds (interactive only); 0 = resource-aware default
	timeout  time.Duration // per-target deadline; 0 = none, default 10m
	coverage bool          // `test` only: coverage on by default; --no-cover disables
	keep     bool          // reuse a still-healthy test env across runs (skip teardown, no re-initdb)
}

// parseBuildArgs hand-parses the small, fixed `build` flag set, keeping each
// short/long flag pair first-class.
func parseBuildArgs(args []string) (buildOpts, error) {
	opts := buildOpts{dir: ".", timeout: 10 * time.Minute, coverage: true}

	for i := 0; i < len(args); i++ {
		a := args[i]
		name, inlineVal, hasInline := a, "", false
		switch {
		case strings.HasPrefix(a, "--"):
			// --flag=value
			if eq := strings.IndexByte(a, '='); eq >= 0 {
				name, inlineVal, hasInline = a[:eq], a[eq+1:], true
			}
		case len(a) > 2 && a[0] == '-' && strings.ContainsRune("CtjT", rune(a[1])):
			// attached short-flag value: -j1, -tgo, -C/path
			name, inlineVal, hasInline = a[:2], a[2:], true
		}

		// takeVal returns the flag's value from either "--flag=val" or
		// "--flag val".
		takeVal := func() (string, error) {
			if hasInline {
				return inlineVal, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag %s needs a value", name)
			}
			i++
			return args[i], nil
		}

		switch name {
		case "-C", "--dir":
			v, err := takeVal()
			if err != nil {
				return opts, err
			}
			opts.dir = v
		case "-t", "--type":
			v, err := takeVal()
			if err != nil {
				return opts, err
			}
			opts.types = parseTypes(v)
			if len(opts.types) == 0 {
				return opts, fmt.Errorf("--type: no valid kinds in %q (want go,pnpm,podman)", v)
			}
		case "-y", "--yes":
			opts.yes = true
		case "--no-cover":
			opts.coverage = false
		case "--keep":
			opts.keep = true
		case "-j", "--jobs":
			v, err := takeVal()
			if err != nil {
				return opts, err
			}
			n, convErr := strconv.Atoi(v)
			if convErr != nil || n < 1 {
				return opts, fmt.Errorf("--jobs: want a positive integer, got %q", v)
			}
			opts.jobs = n
		case "-T", "--timeout":
			v, err := takeVal()
			if err != nil {
				return opts, err
			}
			d, convErr := time.ParseDuration(v)
			if convErr != nil || d < 0 {
				return opts, fmt.Errorf("--timeout: want a duration like 10m / 30s / 0, got %q", v)
			}
			opts.timeout = d
		case "--dry-run":
			opts.dryRun = true
		case "-h", "--help":
			opts.help = true
		default:
			return opts, fmt.Errorf("unknown flag %q", a)
		}
	}
	return opts, nil
}

// parseTypes turns "go,podman" into a kind set, ignoring blanks/unknowns.
func parseTypes(v string) map[detect.Kind]bool {
	set := map[detect.Kind]bool{}
	for _, p := range strings.Split(v, ",") {
		switch detect.Kind(strings.TrimSpace(strings.ToLower(p))) {
		case detect.KindGo:
			set[detect.KindGo] = true
		case detect.KindPnpm:
			set[detect.KindPnpm] = true
		case detect.KindPodman:
			set[detect.KindPodman] = true
		}
	}
	return set
}

// runCapability is the shared body of `build` and `test`: identical flags,
// discovery, selection UI and reporting — only the verb and the discovery
// function differ.
func runCapability(args []string, verb ui.Verb, detectFn func(string) ([]detect.Target, error)) int {
	opts, err := parseBuildArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "a-novel %s: %v\n\n", verb.Base, err)
		fmt.Println(ui.CommandHelpView(version.String(), verb.Base))
		return exitUsage
	}
	if opts.help {
		fmt.Println(ui.CommandHelpView(version.String(), verb.Base))
		return exitOK
	}

	// Fail clearly on a path that is missing or not a directory, so a bad --dir
	// never looks like an empty scan.
	absDir, _ := filepath.Abs(opts.dir)
	if info, statErr := os.Stat(opts.dir); statErr != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "a-novel %s: cannot scan %q: not an accessible directory\n",
			verb.Base, absDir)
		return exitUsage
	}

	// Fail before any filesystem walk when the directory sits outside an
	// a-novel / a-novel-kit git repository, so a wrong path errors instantly.
	if guardErr := detect.RepoGuard(opts.dir); guardErr != nil {
		fmt.Fprintf(os.Stderr, "a-novel %s: %v\n", verb.Base, guardErr)
		return exitUsage
	}

	targets, err := detectFn(opts.dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "a-novel %s: scan failed: %v\n", verb.Base, err)
		return exitFailure
	}
	if opts.types != nil {
		targets = filterTypes(targets, opts.types)
	}
	if opts.coverage && verb.Base == ui.VerbTest.Base {
		targets = withCoverage(targets)
	}

	// An empty selection exits non-zero: a caller expecting targets needs to
	// know none were found, and where the scan looked.
	if len(targets) == 0 {
		scope := ""
		if opts.types != nil {
			scope = " matching --type"
		}
		fmt.Fprintf(os.Stderr,
			"a-novel %s: no %s targets%s found under %s\n", verb.Base, verb.Base, scope, absDir)
		fmt.Fprintf(os.Stderr,
			"  Looked for %s.\n  Run from a project root, or pass --dir <path>.\n", verb.Looks)
		return exitUsage
	}

	if opts.dryRun {
		fmt.Print(ui.DryRunView(version.String(), verb, targets))
		return exitOK
	}

	// SIGINT cancels in-flight subprocesses cleanly in both modes.
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Quitting the TUI with q/esc tears down the program but leaves the build
	// goroutines running; cancelling on every return path propagates into
	// exec.CommandContext so no subprocess outlives the CLI.
	ctx, cancel := context.WithCancel(sigCtx)
	defer cancel()

	// The TUI needs a real terminal. Without one, or with -y, fall back to the
	// non-interactive runner.
	interactive := !opts.yes && term.IsTerminal(os.Stdout.Fd())

	// Never run on top of a test env left up by an aborted run.
	if code := preflight(ctx, verb, targets, interactive && term.IsTerminal(os.Stdin.Fd()), opts.keep); code >= 0 {
		return code
	}

	if !interactive {
		return runNonInteractive(ctx, verb, targets, opts.timeout, opts.keep)
	}

	// The interactive flow runs in the alternate screen buffer, so quitting
	// discards its last frame and the full-log report printed below to the
	// normal buffer is the only one left. Without alt-screen Bubble Tea keeps
	// its final frame on screen and the results appear twice.
	model := ui.New(ctx, version.String(), verb, targets, opts.jobs, opts.timeout, opts.keep)
	final, err := tea.NewProgram(model).Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "a-novel %s: ui error: %v\n", verb.Base, err)
		return exitFailure
	}

	m, ok := final.(ui.Model)
	if !ok {
		fmt.Fprintf(os.Stderr, "a-novel %s: unexpected model type %T\n", verb.Base, final)
		return exitFailure
	}
	results := m.Results()

	// The in-TUI report shows only a tail; this full-log copy survives in
	// scrollback.
	if len(results) > 0 {
		fmt.Print(ui.RenderTextReport(results, m.Aborted(), m.Elapsed(), verb))
	}
	return exitCodeFor(results, m.Aborted())
}

// runNonInteractive runs every target sequentially, streaming a terse progress
// line per target, then prints the full text report. Used for -y and for any
// non-TTY context.
func runNonInteractive(ctx context.Context, verb ui.Verb, targets []detect.Target, timeout time.Duration, keep bool) int {
	fmt.Println(ui.Banner(version.String()))
	fmt.Println()

	results := make([]build.Result, 0, len(targets))
	aborted := false
	start := time.Now()
	for i, t := range targets {
		if ctx.Err() != nil {
			aborted = true
			break
		}
		kind := strings.ToUpper(string(t.Kind))
		fmt.Printf("[%d/%d] %s %-6s %s (%s)\n", i+1, len(targets), verb.Ing, kind, t.Name, t.RelDir)
		res := build.Run(ctx, t, timeout, nil, 0, keep) // sequential: full cores, no live tail
		results = append(results, res)
		if res.Success {
			fmt.Printf("      ok   %-6s %s\n", kind, t.Name)
		} else {
			fmt.Printf("      FAIL %-6s %s\n", kind, t.Name)
		}
	}

	// On abort, record the targets that never ran as failures so the report
	// accounts for every selected target.
	if aborted {
		done := make(map[string]bool, len(results))
		for _, r := range results {
			done[r.Target.ID()] = true
		}
		for _, t := range targets {
			if !done[t.ID()] {
				results = append(results, build.Result{
					Target: t, Success: false,
					ExitErr: errors.New("aborted before completion"),
				})
			}
		}
	}

	fmt.Print(ui.RenderTextReport(results, aborted, time.Since(start), verb))
	return exitCodeFor(results, aborted)
}

// preflight guards against running on top of a test env left up by an aborted
// command. It returns -1 to proceed, or an exit code for the caller to return.
// Both answers to the prompt tear down only the conflicting projects, through a
// scoped `compose down --volume`; clean then continues and abort stops. Without
// a TTY to prompt, it cleans and continues.
func preflight(ctx context.Context, verb ui.Verb, targets []detect.Target, canPrompt, keep bool) int {
	conflicts := build.EnvConflicts(ctx, targets)
	if len(conflicts) == 0 {
		return -1
	}

	if keep {
		// With --keep a leftover env is the point: adopt it so this run reuses
		// its warm containers and volume and skips the Postgres re-init. Bring-up
		// is idempotent and reconciles whatever drifted, while the previous
		// schema and data persist.
		fmt.Fprintln(os.Stderr, ui.EnvNote(
			"Reusing the existing test environment (--keep); its data and schema "+
				"persist from the previous run."))
		return -1
	}

	fmt.Fprintln(os.Stderr, ui.EnvConflictView(verb, conflicts))

	clean := func() {
		for _, c := range conflicts {
			fmt.Fprintln(os.Stderr, ui.EnvStep("cleaning "+c.Env.ID+" …"))
			// Detached ctx: teardown must complete even if the run is aborting.
			_ = build.TearDown(context.WithoutCancel(ctx), c.Env)
		}
	}

	if !canPrompt {
		// Without a TTY, clean automatically so CI and piped runs self-heal.
		fmt.Fprintln(os.Stderr, ui.EnvNote(
			"No prompt available — cleaning the stale environment "+
				"(scoped to this project only) and continuing."))
		clean()
		return -1
	}

	fmt.Fprint(os.Stderr, ui.EnvPrompt("Clean it and continue, or abort? [c]lean / [a]bort: "))
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "c", "clean":
		clean()
		return -1 // proceed with a clean slate
	default:
		clean() // abort still cleans its own env, as requested
		return exitAborted
	}
}

func exitCodeFor(results []build.Result, aborted bool) int {
	for _, r := range results {
		if !r.Success {
			return exitFailure
		}
	}
	if aborted {
		return exitAborted
	}
	return exitOK
}

func filterTypes(targets []detect.Target, types map[detect.Kind]bool) []detect.Target {
	out := targets[:0]
	for _, t := range targets {
		if types[t.Kind] {
			out = append(out, t)
		}
	}
	return out
}

// withCoverage rewrites every test target for coverage mode. A Go target keeps
// its own package selectors and only gains -cover, so every test package runs;
// the generated mocks, test-support and protobuf trees are dropped from the
// reported mean rather than from the run (see internal/ui.goCoverage). Filtering
// the run list instead skipped a package's tests entirely the moment its path
// held one of those segments. A pnpm target gets `-- --coverage`, and vitest's
// v8 text reporter then prints a coverage table the report extracts verbatim.
func withCoverage(targets []detect.Target) []detect.Target {
	for i := range targets {
		t := &targets[i]
		if len(t.Args) == 0 {
			continue
		}
		switch {
		case t.Kind == detect.KindGo && t.Args[0] == cmdTest:
			// The selectors and any -count=1 an env-backed target carries stay
			// as detected; only -cover is added.
			t.Args = append([]string{cmdTest, coverFlag}, t.Args[1:]...)
			t.Detail = "go test -cover " + strings.Join(t.Args[2:], " ")
		case t.Kind == detect.KindPnpm:
			// `pnpm run <script> -- --coverage` → vitest --coverage.
			t.Args = append(append([]string{}, t.Args...), "--", "--coverage")
			t.Detail += "  ·  --coverage"
		}
	}
	return targets
}
