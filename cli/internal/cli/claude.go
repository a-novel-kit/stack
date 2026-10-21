package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

// claudeBin is the Claude Code executable, resolved through $PATH.
const claudeBin = "claude"

// newClaudeCmd launches Claude Code from the stack root, the one directory from
// which app/, kit/ and cli/ are all reachable. The a-novel verbs an agent
// reaches for resolve their targets relative to the working tree, so a session
// started anywhere else loses half the workspace.
func newClaudeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "claude [args...]",
		Short: "Launch Claude Code from the stack root",
		Long: `Start Claude Code in the default stack's root, so a session opens on the whole
workspace wherever you run it. Every argument goes to claude, --help included;
'a-novel help claude' prints this text. claude replaces this process, so the
terminal, its signals and the exit status are claude's own.`,
		Example: `  a-novel claude
  a-novel claude --continue
  a-novel claude -p "summarize what changed on this branch"`,
		// Every arg belongs to claude, flags included. Cobra's own -h/--help
		// handling travels with them, as Long explains.
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return launchClaude(args)
		},
	}
}

// launchClaude replaces this process with claude, run with args from the
// default stack's root.
func launchClaude(args []string) error {
	stack, err := stacks.Default()
	if err != nil {
		return fmt.Errorf("resolve stack root: %w", err)
	}
	// Resolve claude first, so a missing binary fails before the directory
	// changes.
	bin, err := exec.LookPath(claudeBin)
	if err != nil {
		return fmt.Errorf(
			"claude not found in $PATH — install Claude Code (https://claude.com/claude-code): %w", err)
	}
	if err := os.Chdir(stack.Path); err != nil {
		return fmt.Errorf("enter stack root %s: %w", stack.Path, err)
	}

	// On success, Exec never returns. main's update notice and exit-code
	// handling never run: they belong to claude. argv[0] is the resolved path,
	// by convention.
	return syscall.Exec(bin, append([]string{bin}, args...), os.Environ())
}
