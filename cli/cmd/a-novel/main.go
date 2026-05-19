// Command a-novel is the A-Novel storyverse build tool: a single, branded CLI
// that replaces the per-repo bash scripts. This first iteration ships one
// capability — `build` — which detects Go modules, pnpm build scripts and
// Podman images under the working tree, lets you pick what to build through an
// interactive menu, runs the selection, and prints a pass/fail report.
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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"github.com/a-novel-kit/stack/cli/internal/build"
	"github.com/a-novel-kit/stack/cli/internal/detect"
	"github.com/a-novel-kit/stack/cli/internal/ui"
	"github.com/a-novel-kit/stack/cli/internal/version"
)

// Exit codes — distinct so CI and wrapper scripts can react precisely.
const (
	exitOK      = 0
	exitFailure = 1   // at least one build failed
	exitUsage   = 2   // bad invocation
	exitAborted = 130 // user interrupted before completing a run (128+SIGINT)
)

// cmdHelp is the help subcommand name (also the bare/empty-command default).
const cmdHelp = "help"

func main() {
	os.Exit(run(os.Args[1:]))
}

// run is main's testable core: it returns the process exit code instead of
// calling os.Exit, so behaviour can be exercised without tearing down the test
// binary.
func run(args []string) int {
	cmd := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}

	switch cmd {
	case "", cmdHelp:
		// `a-novel help <command>` → that command's help; otherwise (no
		// subcommand, bare `help`, or `a-novel -h`) the generic command list.
		if cmd == cmdHelp && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			fmt.Println(ui.CommandHelpView(version.String(), args[0]))
		} else {
			fmt.Println(ui.HelpView(version.String()))
		}
		return exitOK
	case "version":
		fmt.Println(version.String())
		return exitOK
	case "build":
		if wantsHelp(args) {
			fmt.Println(ui.CommandHelpView(version.String(), cmd))
			return exitOK
		}
		return runCapability(args, ui.VerbBuild, detect.Detect)
	case "test":
		if wantsHelp(args) {
			fmt.Println(ui.CommandHelpView(version.String(), cmd))
			return exitOK
		}
		return runCapability(args, ui.VerbTest, detect.DetectTests)
	default:
		fmt.Fprintf(os.Stderr, "a-novel: unknown command %q\n\n", cmd)
		fmt.Println(ui.HelpView(version.String()))
		return exitUsage
	}
}

// wantsHelp reports whether a build/test invocation is a request for that
// command's help. Help is always the -h/--help flag (the universal CLI
// convention); the bare word form is intentionally NOT accepted — use
// `a-novel help <command>` for the command-style entry point.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// buildOpts is the parsed `build` invocation.
type buildOpts struct {
	dir     string
	types   map[detect.Kind]bool // nil means "all kinds"
	yes     bool
	dryRun  bool
	help    bool
	jobs    int           // max parallel builds (interactive only); 0 = NumCPU
	timeout time.Duration // per-target deadline; 0 = none, default 10m
}

// parseBuildArgs hand-parses the small, fixed `build` flag set. A bespoke
// parser (vs the stdlib flag package) keeps the short/long pairs — -C/--dir,
// -t/--type, -y/--yes — first-class without flag's awkward dual registration.
func parseBuildArgs(args []string) (buildOpts, error) {
	opts := buildOpts{dir: ".", timeout: 10 * time.Minute}

	// next consumes the value for a flag, supporting both "--flag val" and
	// "--flag=val" forms.
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

	// The scan directory must exist and be a directory — fail clearly rather
	// than silently finding nothing in a path that isn't there.
	absDir, _ := filepath.Abs(opts.dir)
	if info, statErr := os.Stat(opts.dir); statErr != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "a-novel %s: cannot scan %q: not an accessible directory\n",
			verb.Base, absDir)
		return exitUsage
	}

	// Fail fast (no filesystem walk) when not inside an a-novel / a-novel-kit
	// git repository — this is what makes an invalid directory error almost
	// instantly instead of triggering a deep scan.
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

	// Nothing to do is an error, not a silent success: a caller (or CI) that
	// expected targets here needs to know none were found and why.
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

	// Wrap with a cancel that fires on every return path. Quitting the TUI
	// with q/esc tears down the program but not the build goroutines; cancel()
	// here propagates into exec.CommandContext so no subprocess outlives the
	// CLI (Copilot review, #36). On a clean finish builds are already done, so
	// this is a no-op.
	ctx, cancel := context.WithCancel(sigCtx)
	defer cancel()

	// The TUI needs a real terminal. Without one (CI, pipes), or with -y,
	// fall back to the non-interactive runner — same builds, plain report.
	interactive := !opts.yes && term.IsTerminal(os.Stdout.Fd())

	// Fail-safe: never run on top of a test env left up by an aborted run.
	if code := preflight(ctx, verb, targets, interactive && term.IsTerminal(os.Stdin.Fd())); code >= 0 {
		return code
	}

	if !interactive {
		return runNonInteractive(ctx, verb, targets, opts.timeout)
	}

	// Run the whole interactive flow in the alternate screen buffer. On quit
	// the alt-screen is torn down and its last frame (the in-TUI report) is
	// discarded — leaving exactly one report: the authoritative full-log one
	// we print below to the normal buffer. Without alt-screen, Bubble Tea
	// leaves its final frame on screen and we'd print the report a second
	// time underneath it (the "results appear twice" bug).
	model := ui.New(ctx, version.String(), verb, targets, opts.jobs, opts.timeout)
	final, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
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

	// The in-TUI report only shows a tail; this is the single, full-log copy
	// that survives in scrollback.
	if len(results) > 0 {
		fmt.Print(ui.RenderTextReport(results, m.Aborted(), m.Elapsed(), verb))
	}
	return exitCodeFor(results, m.Aborted())
}

// runNonInteractive runs every target sequentially, streaming a terse progress
// line per target, then prints the full text report. Used for -y and for any
// non-TTY context.
func runNonInteractive(ctx context.Context, verb ui.Verb, targets []detect.Target, timeout time.Duration) int {
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
		res := build.Run(ctx, t, timeout, nil) // non-interactive: no live tail
		results = append(results, res)
		if res.Success {
			fmt.Printf("      ok   %-6s %s\n", kind, t.Name)
		} else {
			fmt.Printf("      FAIL %-6s %s\n", kind, t.Name)
		}
	}

	// On abort the remaining targets never ran: surface them as failures so
	// the report accounts for every selected target, not just the ones that
	// finished before the interruption.
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

// preflight is the fail-safe against running on top of a test env left up by
// an aborted command. It returns -1 to proceed, or an exit code to return.
// "Clean" and "abort" both tear down ONLY the conflicting projects (scoped
// `compose down --volume`, never a global podman wipe); clean continues,
// abort stops. Non-interactive can't ask, so it self-cleans and aborts.
func preflight(ctx context.Context, verb ui.Verb, targets []detect.Target, canPrompt bool) int {
	conflicts := build.EnvConflicts(ctx, targets)
	if len(conflicts) == 0 {
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
		fmt.Fprintln(os.Stderr, ui.EnvNote(
			"Tearing down the stale environment (scoped to this project only) and "+
				"aborting. Re-run `a-novel "+verb.Base+"` once it is clear."))
		clean()
		return exitAborted
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
