package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

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
	rejectUnknownSubcommands(root)
	return root
}

// rejectUnknownSubcommands makes every command group below cmd fail on an
// argument that names none of its subcommands. Cobra rejects an unknown command
// only at the root, and checks arguments only for a command it can run. Each
// group therefore gets an argument check and a run function that prints its
// help.
func rejectUnknownSubcommands(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		if child.HasSubCommands() && !child.Runnable() {
			child.Args = unknownSubcommand
			child.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
			// Cobra suggests commands within this edit distance at the root.
			child.SuggestionsMinimumDistance = 2
		}
		rejectUnknownSubcommands(child)
	}
}

// unknownSubcommand rejects any argument given to a command group, with the
// error and suggestions cobra prints for an unknown command at the root.
func unknownSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}

	msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
		msg += "\n\nDid you mean this?\n\t" + strings.Join(suggestions, "\n\t")
	}
	return errors.New(msg)
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
