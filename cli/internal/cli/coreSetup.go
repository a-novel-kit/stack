package cli

import (
	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/setup"
)

func newCoreSetupCmd() *cobra.Command {
	var opts setup.Options
	var nonInteractive bool
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up this machine: tools, directories, stacks and shell profile",
		Long: `Set up this machine for the workspace. Each step checks first, so a second
run changes nothing:

  1. Check that podman, git and SSH access to GitHub work.
  2. Create the state and data directories.
  3. Clone each registered stack that is missing, after asking for the
     default one.
  4. Write a marked block into your shell profile, after a backup. It exports
     $A_NOVEL_STACKS when needed, loads completion and runs
     'a-novel core start'.
  5. Start the daemon.

--rc picks the profile, and --no-shell-rc skips step 4, as a dotfile manager
wants.`,
		Example: `  a-novel core setup
  a-novel core setup --rc ~/.bashrc
  a-novel core setup --no-shell-rc`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setupMachine(cmd, opts, nonInteractive)
		},
	}
	cmd.Flags().StringVar(&opts.RCPath, "rc", "", "explicit shell rc path (overrides $SHELL detection)")
	cmd.Flags().BoolVar(&opts.NoShellRC, "no-shell-rc", false, "skip the shell rc integration step")
	cmd.Flags().BoolVar(&opts.NoStartDaemon, "no-start-daemon", false, "skip the auto-start-daemon step at the end")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "never prompt (missing default stack becomes 'skipped')")
	return cmd
}

// setupMachine runs the setup steps opts selects, prompting on stdin unless
// nonInteractive is set. Its last step starts the daemon as `core start` does,
// so a re-run beside a running daemon changes nothing.
func setupMachine(cmd *cobra.Command, opts setup.Options, nonInteractive bool) error {
	opts.StartDaemon = func() error { return startDaemon(cmd.Context(), false) }
	var prompter setup.Prompter
	if !nonInteractive {
		prompter = &setup.StdinPrompter{Out: cmd.OutOrStderr()}
	}
	return setup.Run(cmd.Context(), opts, cmd.OutOrStdout(), prompter)
}
