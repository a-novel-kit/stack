// Package setup implements `a-novel core setup`, the interactive first-time
// bootstrap. It checks the host tools, creates the state directories, clones
// missing stacks, manages a marker-delimited block in the user's shell rc and
// starts the daemon.
//
// Re-running it on a system that is already set up changes nothing and exits 0.
package setup

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

// Options carries CLI-supplied overrides for the setup run.
type Options struct {
	RCPath        string // explicit shell rc path; empty = autodetect
	NoShellRC     bool   // skip the shell rc step entirely
	NoStartDaemon bool   // skip the auto-start-daemon step at the end
	// StartDaemon, if non-nil, is invoked as the final step of Run after
	// rc edits succeed. It should start the daemon, and do nothing when one
	// is already running. The cli package wires it from its own
	// startDetached, which keeps setup free of an import cycle.
	StartDaemon func() error
}

// Run executes the full bootstrap sequence with the given Options, writing
// step-by-step progress to w. The prompter confirms cloning a missing default
// stack. A nil prompter makes the run non-interactive, and that stack is then
// skipped.
func Run(opts Options, w io.Writer, prompter Prompter) error {
	// 1. Environment checks.
	_, _ = fmt.Fprintln(w, "▸ Checking environment...")
	if err := checkPodman(); err != nil {
		return fmt.Errorf("podman check: %w", err)
	}
	_, _ = fmt.Fprintln(w, "  ✓ podman")
	if err := checkGit(); err != nil {
		return fmt.Errorf("git check: %w", err)
	}
	_, _ = fmt.Fprintln(w, "  ✓ git")
	// GitHub SSH only matters when a stack needs cloning, so a failure is a
	// warning.
	if err := checkGitHubSSH(); err != nil {
		_, _ = fmt.Fprintln(w, "  ⚠ GitHub SSH ("+err.Error()+")")
	} else {
		_, _ = fmt.Fprintln(w, "  ✓ GitHub SSH")
	}

	// 2. State directories.
	_, _ = fmt.Fprintln(w, "▸ Verifying state directories...")
	stateDir, dataDir := paths.State(), paths.Data()
	if err := ensureDir(stateDir); err != nil {
		return err
	}
	if err := ensureDir(dataDir); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "  ✓ %s\n  ✓ %s\n", stateDir, dataDir)

	// 3. Stack bootstrap.
	stk, err := stacks.ParseEnv()
	if err != nil {
		return fmt.Errorf("parse %s: %w", stacks.EnvVar, err)
	}
	_, _ = fmt.Fprintf(w, "▸ Bootstrapping %d stack(s)...\n", len(stk))
	for _, s := range stk {
		status, detail := bootstrapStack(s, prompter)
		_, _ = fmt.Fprintf(w, "  %s %s (%s)\n", statusGlyph(status), s.Name, detail)
	}

	// 4. Shell rc integration.
	var rcPath string
	var rcEdited bool
	if !opts.NoShellRC {
		_, _ = fmt.Fprintln(w, "▸ Managing shell rc block...")
		var shell string
		rcPath, shell = resolveRC(opts.RCPath)
		backupPath, changed, err := upsertRCBlock(rcPath, renderRCBlock(stk, shell))
		if err != nil {
			return fmt.Errorf("update %s: %w", rcPath, err)
		}
		rcEdited = changed
		if changed {
			_, _ = fmt.Fprintf(w, "  ✓ updated %s (backup: %s)\n", rcPath, backupPath)
		} else {
			_, _ = fmt.Fprintf(w, "  ✓ %s already up-to-date\n", rcPath)
		}
	} else {
		_, _ = fmt.Fprintln(w, "▸ Shell rc step skipped (--no-shell-rc)")
	}

	// 5. Start the daemon now so the CLI is usable without a new shell or a
	// manual `a-novel core start`. A nil callback skips it, as in a test
	// harness that calls Run without the CLI bridge.
	var daemonStarted bool
	if !opts.NoStartDaemon && opts.StartDaemon != nil {
		_, _ = fmt.Fprintln(w, "▸ Starting the a-novel daemon...")
		if err := opts.StartDaemon(); err != nil {
			_, _ = fmt.Fprintf(w, "  ⚠ daemon start failed: %v\n", err)
		} else {
			daemonStarted = true
			_, _ = fmt.Fprintln(w, "  ✓ daemon running")
		}
	}

	// 6. Summary and next step. With the daemon up, `a-novel run ui` works
	// right away; otherwise the hint points at the manual start. An edited
	// rc only loads in future shells, so the hint also suggests sourcing it
	// for completion in the current one.
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Setup complete.")
	switch {
	case daemonStarted:
		_, _ = fmt.Fprintln(w, "  → Daemon is up. Try `a-novel run ui` or `a-novel run ps`.")
		if rcEdited {
			_, _ = fmt.Fprintf(w, "  → For tab-completion in THIS shell: `source %s`\n", rcPath)
			_, _ = fmt.Fprintln(w, "    (future shells load it automatically.)")
		}
	case rcEdited:
		_, _ = fmt.Fprintf(w, "  → Open a new shell, or `source %s`, then `a-novel core start`.\n", rcPath)
	default:
		_, _ = fmt.Fprintln(w, "  → Run `a-novel core start` to bring the daemon up.")
	}
	return nil
}

// ensureDir creates dir with 0700 perms. An existing dir is tightened to 0700
// on a best-effort basis.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	_ = os.Chmod(dir, 0o700) // best effort: MkdirAll keeps an existing dir's perms
	return nil
}

// Prompter asks the user a yes/no question. Setup uses it to confirm cloning a
// missing default stack.
type Prompter interface {
	YesNo(prompt string) (bool, error)
}

// StdinPrompter is the Prompter backed by os.Stdin.
type StdinPrompter struct {
	Out io.Writer // where the prompt is written; nil means os.Stderr
}

// YesNo writes prompt to Out and reads one line from stdin. Only "y" or
// "yes", in any case, counts as yes.
func (p *StdinPrompter) YesNo(prompt string) (bool, error) {
	w := p.Out
	if w == nil {
		w = os.Stderr
	}
	_, _ = fmt.Fprintf(w, "%s [y/N]: ", prompt)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false, sc.Err()
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes", nil
}

// statusGlyph returns the summary glyph for a stack-bootstrap status.
func statusGlyph(status string) string {
	switch status {
	case statusValid, statusCloned:
		return "✓"
	case statusSkipped:
		return "○"
	case statusRefused:
		return "✗"
	default:
		return "•"
	}
}
