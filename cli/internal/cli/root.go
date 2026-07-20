// Package cli wires every subcommand the a-novel binary exposes. The
// standalone `test` and `build` commands are wrapped from cmd/a-novel/main.go,
// which owns their flag parsing; the daemon-backed verbs under `run` are
// implemented here directly against the daemon's rpc client.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/version"
)

// LegacyHandlers carries the entrypoints for the standalone capabilities
// (test, build), which run their own flag parsers rather than Cobra's.
// cmd/a-novel injects them so that flag-parsing code stays in that package;
// the daemon-backed verbs under `run` live entirely in this one.
//
// There is no `Run` handler: `run` is the parent namespace for the
// daemon-backed verbs, so a standalone run capability would collide with it.
type LegacyHandlers struct {
	Test  func(args []string) int
	Build func(args []string) int
}

// NewRoot builds the root a-novel command with every subcommand attached. The
// caller supplies the standalone test/build handlers, since their flag parsing
// lives in the cmd/a-novel package.
func NewRoot(legacy LegacyHandlers) *cobra.Command {
	root := &cobra.Command{
		Use:   "a-novel",
		Short: "the A-Novel storyverse build tool",
		Long: `a-novel is the storyverse build tool: a single, branded CLI that
replaces the per-repo bash scripts and Makefiles.

Commands fall into three groups:

  Standalone capabilities — operate on the local working tree, no daemon
  required:
    test, build, publish, secrets, claude

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
	// and exit.
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
	root.AddCommand(newRepoCmd())

	// `a-novel secrets` — local, encrypted secrets manager. Standalone like
	// publish/repo: operates on the local key + store, no daemon. Its secrets
	// are auto-injected into test/run/ui via a value-free per-repo mapping.
	root.AddCommand(newSecretsCmd())

	// `a-novel claude` — launch Claude Code from the stack root. Standalone
	// and terminal: it execve's into claude, so it never returns here.
	root.AddCommand(newClaudeCmd())

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

	// Daemon-backed verbs, all under `run`. The namespace keeps the
	// daemon-touching surface visually distinct from the standalone
	// capabilities (test, build, publish).
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

// newLegacyCmd wraps a non-Cobra handler so test/build can be invoked via
// 'a-novel <verb> [args...]'. DisableFlagParsing hands args straight to the
// handler, which runs its own flag parser. As a result Cobra does not
// intercept `-h` / `--help`; the handler must recognize them itself.
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
