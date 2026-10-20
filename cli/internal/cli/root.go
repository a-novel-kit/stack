package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/a-novel-kit/stack/cli/internal/version"
)

const (
	commandCore    = "core"
	commandInstall = "install"
	commandRun     = "run"
	commandSync    = "sync"
	stackLabel     = "stack"
)

// Exit codes beyond the generic failure, distinct so scripts can react.
const (
	exitUsage   = 2   // bad invocation
	exitAborted = 130 // interrupted before completing (128+SIGINT)
)

// stdinIsTTY and stdoutIsTTY report whether the streams are terminals. They are
// package vars so tests can drive the non-interactive branches without a PTY.
var (
	stdinIsTTY  = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	stdoutIsTTY = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }
)

// ExitError carries an exit status through Cobra's error path. main prints Err,
// when set, before exiting with Code; a nil Err exits silently, as when the
// status is a child's own.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

// NewRoot builds the root a-novel command with every subcommand attached.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "a-novel",
		Short: "The command-line tool for developing A-Novel",
		Long: `a-novel works on every repository of the A-Novel workspace the same way. Most
commands run on their own in the working tree. The commands under 'run' talk to
the background daemon, which runs your services, and the commands under 'core'
manage the daemon and the workspace.

See https://a-novel-kit.github.io/stack/ for the everyday workflows.`,
		// main prints errors itself, so a status-only ExitError stays silent.
		SilenceErrors:     true,
		SilenceUsage:      true,
		Version:           version.String(),
		PersistentPreRunE: rejectLateSandbox,
	}
	root.PersistentFlags().Bool("sandbox", false,
		"run one standalone command in a fresh temporary stack (must be first)")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError(err) })

	root.AddCommand(
		newCapabilityCmd(testCapability),
		newCapabilityCmd(buildCapability),
		newPublishCmd(),
		newRepoCmd(),
		newSecretsCmd(),
		newClaudeCmd(),
		newVersionCmd(),
		newCoreCmd(),
		newInstallCmd(),
		newRunCmd(),
	)
	return root
}

// rejectLateSandbox refuses a --sandbox that reached a subcommand. main acts on
// the flag only as the first argument, so anywhere else it would do nothing.
func rejectLateSandbox(cmd *cobra.Command, _ []string) error {
	if sandbox, _ := cmd.Flags().GetBool("sandbox"); sandbox {
		return usageError(fmt.Errorf("%s must be the first argument", sandboxFlag))
	}
	return nil
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the a-novel CLI version",
		Run:   func(cmd *cobra.Command, _ []string) { cmd.Println(version.String()) },
	}
}

// newRunCmd is the parent of every daemon-backed verb. Each verb is a thin
// client of the daemon's RPC holding no supervision state: the daemon owns the
// running targets, their phases and their logs.
func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   commandRun,
		Short: "Operate on services and targets via the a-novel daemon",
		Long: `Every verb under 'run' talks to the long-lived a-novel daemon over its unix
socket, and refuses with a clear error when it is not running (see 'a-novel
core start'). Daemon control itself lives under 'a-novel core'.`,
	}
	cmd.AddCommand(
		newUICmd(),
		newPsCmd(),
		newStacksCmd(),
		newTopologyCmd(),
		newServiceCmd(),
		newStartCmd(),
		newKillCmd(),
		newRestartCmd(),
		newLogsCmd(),
		newEnvCmd(),
		newWatchCmd(),
		newVolumeCmd(),
		newExecCmd(),
		newDebugCmd(),
	)
	return cmd
}
