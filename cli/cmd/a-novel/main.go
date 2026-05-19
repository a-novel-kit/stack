// Command a-novel is the A-Novel storyverse build tool: a single, branded CLI
// that replaces the per-repo bash scripts. This first iteration ships one
// capability — `build` — which detects Go modules, pnpm build scripts and
// Podman images under the working tree, lets you pick what to build through an
// interactive menu, runs the selection, and prints a pass/fail report.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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
	case "", "help":
		// No subcommand (or an explicit "help") prints help — this also covers
		// `a-novel -h`, whose leading "-" leaves cmd empty.
		fmt.Println(ui.HelpView(version.String()))
		return exitOK
	case "version":
		fmt.Println(version.String())
		return exitOK
	case "build":
		return runBuild(args)
	default:
		fmt.Fprintf(os.Stderr, "a-novel: unknown command %q\n\n", cmd)
		fmt.Println(ui.HelpView(version.String()))
		return exitUsage
	}
}

// buildOpts is the parsed `build` invocation.
type buildOpts struct {
	dir    string
	types  map[detect.Kind]bool // nil means "all kinds"
	yes    bool
	dryRun bool
	help   bool
	jobs   int // max parallel builds (interactive only); 0 = NumCPU
}

// parseBuildArgs hand-parses the small, fixed `build` flag set. A bespoke
// parser (vs the stdlib flag package) keeps the short/long pairs — -C/--dir,
// -t/--type, -y/--yes — first-class without flag's awkward dual registration.
func parseBuildArgs(args []string) (buildOpts, error) {
	opts := buildOpts{dir: "."}

	// next consumes the value for a flag, supporting both "--flag val" and
	// "--flag=val" forms.
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, inlineVal, hasInline := a, "", false
		if strings.HasPrefix(a, "--") {
			if eq := strings.IndexByte(a, '='); eq >= 0 {
				name, inlineVal, hasInline = a[:eq], a[eq+1:], true
			}
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

func runBuild(args []string) int {
	opts, err := parseBuildArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "a-novel build: %v\n\n", err)
		fmt.Println(ui.HelpView(version.String()))
		return exitUsage
	}
	if opts.help {
		fmt.Println(ui.HelpView(version.String()))
		return exitOK
	}

	targets, err := detect.Detect(opts.dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "a-novel build: scan failed: %v\n", err)
		return exitFailure
	}
	if opts.types != nil {
		targets = filterTypes(targets, opts.types)
	}

	if len(targets) == 0 {
		fmt.Println(ui.Banner(version.String()))
		fmt.Println("\nNo build targets found under " + opts.dir + ".")
		return exitOK
	}

	if opts.dryRun {
		fmt.Print(ui.DryRunView(version.String(), targets))
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
	if !interactive {
		return runNonInteractive(ctx, targets)
	}

	// Run the whole interactive flow in the alternate screen buffer. On quit
	// the alt-screen is torn down and its last frame (the in-TUI report) is
	// discarded — leaving exactly one report: the authoritative full-log one
	// we print below to the normal buffer. Without alt-screen, Bubble Tea
	// leaves its final frame on screen and we'd print the report a second
	// time underneath it (the "results appear twice" bug).
	model := ui.New(ctx, version.String(), targets, opts.jobs)
	final, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "a-novel build: ui error: %v\n", err)
		return exitFailure
	}

	m, ok := final.(ui.Model)
	if !ok {
		fmt.Fprintf(os.Stderr, "a-novel build: unexpected model type %T\n", final)
		return exitFailure
	}
	results := m.Results()

	// The in-TUI report only shows a tail; this is the single, full-log copy
	// that survives in scrollback.
	if len(results) > 0 {
		fmt.Print(ui.RenderTextReport(results, m.Aborted(), m.Elapsed()))
	}
	return exitCodeFor(results, m.Aborted())
}

// runNonInteractive builds every target in order, streaming a terse progress
// line per target, then prints the full text report. Used for -y and for any
// non-TTY context.
func runNonInteractive(ctx context.Context, targets []detect.Target) int {
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
		fmt.Printf("[%d/%d] building %-6s %s (%s)\n", i+1, len(targets), kind, t.Name, t.RelDir)
		res := build.Run(ctx, t)
		results = append(results, res)
		if res.Success {
			fmt.Printf("      ok   %-6s %s\n", kind, t.Name)
		} else {
			fmt.Printf("      FAIL %-6s %s\n", kind, t.Name)
		}
	}

	fmt.Print(ui.RenderTextReport(results, aborted, time.Since(start)))
	return exitCodeFor(results, aborted)
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
