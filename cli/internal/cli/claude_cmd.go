// `a-novel claude` — launch Claude Code from the stack root.
//
// The stack root is the one directory from which `app/`, `kit/` and `cli/` are
// all reachable, and the `a-novel` verbs an agent reaches for (`test`, `build`,
// `run …`) resolve their targets relative to the working tree. Starting a
// session anywhere else silently loses half the workspace, so this verb makes
// the correct working directory the only possible one.

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
)

// claudeBin is the Claude Code executable, resolved through $PATH.
const claudeBin = "claude"

func newClaudeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "claude [args...]",
		Short: "Launch Claude Code from the stack root",
		Long: `Starts Claude Code with the stack root as its working directory, so a
session always opens on the whole workspace (app/, kit/, cli/) instead of
whichever checkout you happened to be standing in.

The stack root is the default stack — the first entry of $A_NOVEL_STACKS,
falling back to ~/git-projects/a-novel when that variable is unset. This is
the same resolution 'a-novel install' uses to find the CLI source.

Every argument is forwarded verbatim, so the full Claude Code flag set works
unchanged. That includes '--help', which reaches claude rather than this
command: use 'a-novel help claude' for this text.

The a-novel process is REPLACED by claude (execve), not left wrapping it —
claude owns the terminal directly, so resize, signals and the exit code are
its own with nothing in between.`,
		Example: `  a-novel claude
  a-novel claude --continue
  a-novel claude -p "summarize what changed on this branch"`,
		// All args belong to claude, including flags this command would
		// otherwise be asked to interpret. Cobra's own -h/--help handling
		// goes with them — deliberate, see Long.
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := defaultStackPath()
			if err != nil {
				return fmt.Errorf("resolve stack root: %w", err)
			}
			// Resolve before chdir, so a missing binary is reported from the
			// PATH lookup and no directory change is left half-applied.
			bin, err := exec.LookPath(claudeBin)
			if err != nil {
				return fmt.Errorf(
					"claude not found in $PATH — install Claude Code (https://claude.com/claude-code): %w", err)
			}
			if err := os.Chdir(root); err != nil {
				return fmt.Errorf("enter stack root %s: %w", root, err)
			}

			// Point of no return: on success Exec never returns, so nothing
			// after this line runs — including main's update-notice and exit-
			// code handling, which now belong to claude. argv[0] is the
			// resolved path by convention.
			return syscall.Exec(bin, append([]string{bin}, args...), os.Environ())
		},
	}
}
