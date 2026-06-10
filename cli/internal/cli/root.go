// Package cli wires every subcommand the a-novel binary exposes. The
// existing `test` / `build` / `run` commands are wrapped from
// cmd/a-novel/main.go's legacy implementations; new daemon-backed verbs
// (core, ps, start, kill, logs, env, volume, ...) are implemented here
// directly against internal/client/rpc.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/version"
)

// LegacyHandlers carries the entrypoints for the standalone capabilities
// (test, build) that predate the daemon redesign and have their own flag
// parsers. main.go in cmd/a-novel injects these so we don't have to move the
// pre-existing implementation code into this package — the daemon redesign
// only touches the `run` namespace, which lives entirely here.
//
// Note the absence of `Run` — the old `a-novel run` was the legacy
// per-repo runner; the new top-level `run` is the daemon proxy parent, so
// the legacy form had to go to avoid namespace collision.
type LegacyHandlers struct {
	Test  func(args []string) int
	Build func(args []string) int
}

func NewRoot(legacy LegacyHandlers) *cobra.Command {
	root := &cobra.Command{
		Use:   "a-novel",
		Short: "the A-Novel storyverse build tool",
		Long: `a-novel is the storyverse build tool: a single, branded CLI that
replaces the per-repo bash scripts and Makefiles.

Commands fall into three groups:

  Standalone capabilities — operate on the local working tree, no daemon
  required:
    test, build, publish

  Daemon control — manage the long-lived a-novel daemon (one per user)
  and the workspace it serves:
    core start, core setup, core kill, core status, core sync,
    core prepare-reinstall; install (rebuild + reinstall, state-preserving)

  Daemon-backed verbs — interact with the daemon over its unix socket,
  always nested under 'run':
    run ui, run ps, run start <target>, run kill <target>, run restart,
    run logs <target>, run env [<service>], run watch, run topology,
    run stacks, run service infra start|kill, run service status,
    run volume list|backup|restore|clear, run exec <target>, run debug

Run 'a-novel help <command>' or 'a-novel <command> --help' for any
command's flags and examples.

See docs at https://github.com/a-novel-kit/stack/blob/master/cli/README.md
for the full design.`,
		SilenceUsage:  true, // don't dump usage on every error — too noisy for the daemon-backed verbs
		SilenceErrors: false,
		Version:       version.String(),
	}

	// Standalone capabilities. These don't need the daemon — they scan the
	// working tree, run go test / pnpm test / podman build, report results
	// and exit. Kept verbatim through every phase; never deleted.
	root.AddCommand(newLegacyCmd("test",
		"Run tests (Go and pnpm) interactively or by selector",
		`Discovers Go test packages and pnpm test scripts under the working tree, lets
you pick which to run through an interactive menu (or a CLI selector), executes
them, and prints a pass/fail report.`,
		legacy.Test))
	root.AddCommand(newLegacyCmd("build",
		"Build Go binaries, pnpm bundles, and Podman images interactively or by selector",
		`Discovers Go modules, pnpm build scripts, and Podman images under the working
tree, lets you pick what to build through an interactive menu (or a CLI
selector), runs the selection, and prints a pass/fail report.`,
		legacy.Build))

	// `a-novel publish` — cut a release locally (bump, commit, tag, push).
	// Standalone like test/build: operates on the working tree, no daemon.
	root.AddCommand(newPublishCmd())

	// Version (compatibility with legacy `a-novel version` invocation —
	// Cobra also exposes --version on root automatically).
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the a-novel CLI version",
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println(version.String())
		},
	})

	// Daemon control. Stays at the top level — clear separation between
	// "manage the daemon itself" and "operate on things the daemon
	// supervises" (which is under `run`).
	root.AddCommand(newCoreCmd())

	// `a-novel install` — the canonical reinstall path. Wraps the
	// prepare-reinstall + go install + core start sequence so the
	// running daemon is always replaced with one running the new
	// binary, preserving live state across the upgrade.
	root.AddCommand(newInstallCmd())

	// Daemon-backed verbs, ALL under `run`. The namespace makes the
	// daemon-touching surface visually distinct from the standalone
	// capabilities, and replaces the now-removed legacy `a-novel run` so
	// there's no command-name collision.
	root.AddCommand(newRunCmd())

	return root
}

// newRunCmd is the parent for every daemon-backed verb. By itself it just
// prints help and lists its subcommands; users always invoke a child like
// `a-novel run ps` or `a-novel run start <target>`.
func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Operate on services and targets via the a-novel daemon",
		Long: `Every verb under 'run' talks to the long-lived a-novel daemon over its
unix socket. The daemon must be running (see 'a-novel core start') —
'run' verbs refuse with a clear error if it isn't.

Group is exhaustive: starting, killing, observing, log-streaming, env
inspection, volume management, exec/debug, and the TUI all live under
'run'. The exception is daemon-control itself, which lives under
'a-novel core' (start/setup/kill/status/prepare-reinstall).`,
	}
	// TUI first so it surfaces prominently in help.
	cmd.AddCommand(newUICmd())
	// Discovery / state.
	cmd.AddCommand(newPsCmd())
	cmd.AddCommand(newStacksCmd())
	cmd.AddCommand(newTopologyCmd())
	cmd.AddCommand(newServiceCmd())
	// Target lifecycle.
	cmd.AddCommand(newStartCmd())
	cmd.AddCommand(newKillCmd())
	cmd.AddCommand(newRestartCmd())
	// Observability.
	cmd.AddCommand(newLogsCmd())
	cmd.AddCommand(newEnvCmd())
	cmd.AddCommand(newWatchCmd())
	// Volumes.
	cmd.AddCommand(newVolumeCmd())
	// Exec / debug.
	cmd.AddCommand(newExecCmd())
	cmd.AddCommand(newDebugCmd())
	return cmd
}

// newLegacyCmd wraps a non-Cobra handler so the existing test/build/run
// can be invoked via 'a-novel <verb> [args...]'. DisableFlagParsing means
// Cobra hands args straight to the legacy handler — which has its own
// flag parser — without interpreting flags itself. Important: this means
// `-h` / `--help` are NOT intercepted by Cobra; the legacy handler must
// handle them itself (the existing wantsHelp() in main.go does).
func newLegacyCmd(verb, short, long string, fn func([]string) int) *cobra.Command {
	return &cobra.Command{
		Use:                verb,
		Short:              short,
		Long:               long,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			code := fn(args)
			if code != 0 {
				// Cobra wraps non-zero RunE errors in exit code 1 by
				// default. We need the exact legacy exit code, so we
				// use a typed error that the executor in main.go
				// unwraps for os.Exit.
				return &ExitError{Code: code}
			}
			return nil
		},
	}
}

// ExitError signals an explicit os.Exit code. Cobra's exec returns the
// error; main.go inspects it for the precise code.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return "" }
