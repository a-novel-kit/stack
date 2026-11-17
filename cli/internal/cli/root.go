package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/version"
)

const (
	commandCore    = "core"
	commandInstall = "install"
	commandRun     = "run"
	commandSync    = "sync"
	stackLabel     = "stack"
)

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
		newSecretsCmd(),
		newClaudeCmd(),
		newVersionCmd(),
		newCoreCmd(),
		newInstallCmd(),
		newRunCmd(),
	)
	checkArgs(root)
	return root
}

// checkArgs makes every argument check of cmd and the commands below it fail
// with a usage error, exit status 2. It first gives each command group an
// argument check that rejects an argument naming none of its subcommands, and
// a run function that prints its help: cobra checks arguments only for a
// command it can run.
func checkArgs(cmd *cobra.Command) {
	if cmd.HasSubCommands() && !cmd.Runnable() {
		cmd.Args = unknownSubcommand
		cmd.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
		// Cobra suggests commands within this edit distance at the root.
		cmd.SuggestionsMinimumDistance = 2
	}
	if check := cmd.Args; check != nil {
		cmd.Args = func(cmd *cobra.Command, args []string) error {
			if err := check(cmd, args); err != nil {
				return usageError(err)
			}
			return nil
		}
	}
	for _, child := range cmd.Commands() {
		checkArgs(child)
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
		Long: `Print the version of this a-novel binary. A daemon started before an upgrade
keeps running its own version, which 'core status' prints; 'a-novel install'
restarts it on the new binary.`,
		Run: func(cmd *cobra.Command, _ []string) { cmd.Println(version.String()) },
	}
}
