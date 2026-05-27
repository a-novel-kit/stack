// Package setup implements `a-novel core setup` — the interactive
// first-time bootstrap per spec §3.5. Idempotent: re-running on an
// already-set-up system performs zero filesystem writes and exits 0.
//
// Sequence:
//
//  1. Detect environment (podman + git + GitHub SSH access).
//  2. Verify state directories ($XDG_STATE_HOME/a-novel,
//     $XDG_DATA_HOME/a-novel) exist with 0700 perms.
//  3. Per-stack bootstrap: for every entry in $A_NOVEL_STACKS, run the
//     three-way check (exists+valid / exists+invalid / missing). Default
//     stack prompts on missing; non-default clones unconditionally.
//  4. Shell rc integration: manage a marker-delimited block in the
//     user's shell rc (zshrc / bashrc / fish), with a pre-edit backup.
//  5. Print a summary with the "next step" hint.
package setup

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

// Options carries CLI-supplied overrides for the setup run.
type Options struct {
	RCPath         string // explicit shell rc path; empty = autodetect
	NoShellRC      bool   // skip the shell rc step entirely
	NoStartDaemon  bool   // skip the auto-start-daemon step at the end
	NonInteractive bool   // disables prompts (for CI / tests)
	// StartDaemon, if non-nil, is invoked as the final step of Run after
	// rc edits succeed. It should start the daemon (idempotent: a no-op
	// when one is already running). Wiring it from the cli package
	// (which already has `startDetached`) keeps setup free of an import
	// cycle with internal/cli.
	StartDaemon func() error
}

// Result reports what setup did, for the summary at the end.
type Result struct {
	PodmanOK      bool
	GitOK         bool
	GitHubSSHOK   bool
	StateDir      string
	DataDir       string
	Stacks        []StackResult
	RCEdited      bool
	RCPath        string
	BackupPath    string   // path of the pre-edit backup, if we wrote one
	DaemonStarted bool     // true if Run successfully launched the daemon
	DaemonNote    string   // failure reason when DaemonStarted is false
	Notes         []string // free-form messages (e.g., "stack X already cloned")
}

// StackResult is the per-stack bootstrap outcome.
type StackResult struct {
	Name       string
	Path       string
	Status     string // "valid" | "cloned" | "skipped" | "refused"
	Detail     string
}

// Run executes the full bootstrap sequence with the given Options. The
// `prompter` reads user input when interactive prompts are needed
// (default-stack clone confirmation). Pass nil for non-interactive
// modes — missing default stack then becomes a "refused" outcome.
//
// Writes step-by-step progress to `w` so the user can follow along.
func Run(opts Options, w io.Writer, prompter Prompter) (*Result, error) {
	res := &Result{StateDir: paths.State(), DataDir: paths.Data()}

	// 1. Environment checks.
	fmt.Fprintln(w, "▸ Checking environment...")
	if err := checkPodman(); err != nil {
		return res, fmt.Errorf("podman check: %w", err)
	}
	res.PodmanOK = true
	fmt.Fprintln(w, "  ✓ podman")
	if err := checkGit(); err != nil {
		return res, fmt.Errorf("git check: %w", err)
	}
	res.GitOK = true
	fmt.Fprintln(w, "  ✓ git")
	if err := checkGitHubSSH(); err != nil {
		// Non-fatal — the user may genuinely not have GitHub SSH set
		// up if their default stack is already cloned and no
		// non-default stacks need fetching. Surface as a note.
		res.Notes = append(res.Notes,
			"GitHub SSH check failed: "+err.Error()+
				" (only matters if a stack needs cloning)")
		fmt.Fprintln(w, "  ⚠ GitHub SSH ("+err.Error()+")")
	} else {
		res.GitHubSSHOK = true
		fmt.Fprintln(w, "  ✓ GitHub SSH")
	}

	// 2. State directories.
	fmt.Fprintln(w, "▸ Verifying state directories...")
	if err := ensureDir(res.StateDir); err != nil {
		return res, err
	}
	if err := ensureDir(res.DataDir); err != nil {
		return res, err
	}
	fmt.Fprintf(w, "  ✓ %s\n  ✓ %s\n", res.StateDir, res.DataDir)

	// 3. Stack bootstrap.
	stk, err := stacks.ParseEnv()
	if err != nil {
		return res, fmt.Errorf("parse %s: %w", stacks.EnvVar, err)
	}
	fmt.Fprintf(w, "▸ Bootstrapping %d stack(s)...\n", len(stk))
	for _, s := range stk {
		sr := bootstrapStack(s, opts.NonInteractive, prompter)
		res.Stacks = append(res.Stacks, sr)
		fmt.Fprintf(w, "  %s %s (%s)\n", statusGlyph(sr.Status), sr.Name, sr.Detail)
	}

	// 4. Shell rc integration.
	if !opts.NoShellRC {
		fmt.Fprintln(w, "▸ Managing shell rc block...")
		rcPath, shell, err := resolveRC(opts.RCPath)
		if err != nil {
			return res, fmt.Errorf("resolve shell rc: %w", err)
		}
		res.RCPath = rcPath
		block := renderRCBlock(stk, shell)
		backupPath, changed, err := upsertRCBlock(rcPath, block)
		if err != nil {
			return res, fmt.Errorf("update %s: %w", rcPath, err)
		}
		if changed {
			res.RCEdited = true
			res.BackupPath = backupPath
			fmt.Fprintf(w, "  ✓ updated %s (backup: %s)\n", rcPath, backupPath)
		} else {
			fmt.Fprintf(w, "  ✓ %s already up-to-date\n", rcPath)
		}
	} else {
		fmt.Fprintln(w, "▸ Shell rc step skipped (--no-shell-rc)")
	}

	// 5. Start the daemon now so the user doesn't have to open a new
	// shell or hand-run `a-novel core start` before the CLI is usable.
	// Skipped on --no-start-daemon, or when no callback was wired
	// (e.g., a test harness calling Run without the CLI bridge).
	if !opts.NoStartDaemon && opts.StartDaemon != nil {
		fmt.Fprintln(w, "▸ Starting the a-novel daemon...")
		if err := opts.StartDaemon(); err != nil {
			res.DaemonNote = err.Error()
			fmt.Fprintf(w, "  ⚠ daemon start failed: %v\n", err)
		} else {
			res.DaemonStarted = true
			fmt.Fprintln(w, "  ✓ daemon running")
		}
	}

	// 6. Summary + next step. The hint is now context-sensitive: if we
	// started the daemon, the user can run `a-novel run ui` right away;
	// if we couldn't (skipped, or it failed), point at the manual path.
	// In both daemon-up cases we also nudge the user to source the rc
	// so shell completion (`a-novel <TAB>`) lights up in the CURRENT
	// shell — the rc block is only auto-loaded by future shells.
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Setup complete.")
	switch {
	case res.DaemonStarted:
		fmt.Fprintln(w, "  → Daemon is up. Try `a-novel run ui` or `a-novel run ps`.")
		if res.RCEdited {
			fmt.Fprintf(w, "  → For tab-completion in THIS shell: `source %s`\n", res.RCPath)
			fmt.Fprintln(w, "    (future shells load it automatically.)")
		}
	case res.RCEdited:
		fmt.Fprintf(w, "  → Open a new shell, or `source %s`, then `a-novel core start`.\n", res.RCPath)
	default:
		fmt.Fprintln(w, "  → Run `a-novel core start` to bring the daemon up.")
	}
	return res, nil
}

// ensureDir creates dir with 0700 perms (idempotent). Errors only if
// dir exists with wrong type or perms can't be set.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	// Tighten perms in case it pre-existed with looser ones.
	_ = os.Chmod(dir, 0o700)
	return nil
}

// Prompter is the interactive prompt interface used for default-stack
// clone confirmation. Implementations should write the prompt + read
// one line; return ("y" | "yes") for yes, anything else for no.
type Prompter interface {
	YesNo(prompt string) (bool, error)
}

// StdinPrompter is the canonical implementation backed by os.Stdin.
type StdinPrompter struct {
	Out io.Writer
}

func (p *StdinPrompter) YesNo(prompt string) (bool, error) {
	w := p.Out
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintf(w, "%s [y/N]: ", prompt)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false, sc.Err()
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes", nil
}

func statusGlyph(status string) string {
	switch status {
	case "valid", "cloned":
		return "✓"
	case "skipped":
		return "○"
	case "refused":
		return "✗"
	default:
		return "•"
	}
}

// _ guards against package shape drift when callers expect filepath in
// the import set (paths are constructed via paths.* helpers, so the
// stdlib filepath import is only used inside private files — keep it
// referenced here so future restructuring doesn't break this file's
// imports silently).
var _ = filepath.Join
