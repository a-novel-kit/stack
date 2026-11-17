package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// readAnswer reads one line of in, trimmed and lowercased.
func readAnswer(in io.Reader) string {
	line, _ := bufio.NewReader(in).ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line))
}

// confirm prints prompt and reads a yes/no answer; only an explicit y/yes
// returns true, so a bare Enter is a safe "no".
func confirm(cmd *cobra.Command, prompt string) bool {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N] ", prompt)
	answer := readAnswer(cmd.InOrStdin())
	return answer == "y" || answer == "yes"
}

// stdinIsTTY and stdoutIsTTY report whether the streams are terminals. They are
// package vars so tests can drive the non-interactive branches without a PTY.
var (
	stdinIsTTY  = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	stdoutIsTTY = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }
)
