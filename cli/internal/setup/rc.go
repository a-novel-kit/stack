package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

// Block markers used to locate / replace the managed section in the
// user's shell rc. See spec §3.5 step 4 — the marker pattern is the
// industry-standard "self-locating block" used by conda, pyenv, nvm, etc.
const (
	beginMarker = "# >>> a-novel setup >>>"
	endMarker   = "# <<< a-novel setup <<<"
)

// Shell names — hoisted so the dispatch tables in resolveRC,
// shellFromPath, renderRCBlock agree on a single canonical spelling
// for each shell we detect.
const (
	shellZsh  = "zsh"
	shellBash = "bash"
	shellFish = "fish"
)

// Stack-bootstrap status outcomes. Surfaces in setup.go's summary
// render and in stack.go's classification; constants prevent typos
// from silently producing an "(unknown)" status.
const (
	statusValid   = "valid"
	statusCloned  = "cloned"
	statusSkipped = "skipped"
	statusRefused = "refused"
)

// maxRCBackups bounds the rotating `.a-novel-backup-<ts>` siblings of
// the rc file. Three is enough recovery depth without cluttering the
// user's $HOME with unbounded copies.
const maxRCBackups = 3

// resolveRC picks the shell rc file to manage. Detection priority per
// spec §3.5 step 4: explicit override → $ZDOTDIR/.zshrc → $SHELL
// basename mapping → fallback ~/.zshrc. Returns (path, shellName)
// where shellName is one of "zsh" | "bash" | "fish" so renderRCBlock
// can emit the matching completion-source line.
func resolveRC(override string) (string, string) {
	home, _ := os.UserHomeDir()
	if override != "" {
		// User-supplied path always wins. Infer shell from filename.
		p := expandHome(override)
		return p, shellFromPath(p)
	}
	if zdot := os.Getenv("ZDOTDIR"); zdot != "" {
		return filepath.Join(zdot, ".zshrc"), shellZsh
	}
	shell := filepath.Base(os.Getenv("SHELL"))
	switch shell {
	case shellBash:
		return filepath.Join(home, ".bashrc"), shellBash
	case shellFish:
		return filepath.Join(home, ".config", "fish", "config.fish"), shellFish
	default:
		return filepath.Join(home, ".zshrc"), shellZsh
	}
}

// shellFromPath maps a rc filename to its shell name. Defaults to "zsh"
// for unfamiliar names so the completion-source line still emits
// something useful (and at worst silently no-ops if the user is on a
// different shell — the `command -v` guard prevents broken sourcing).
func shellFromPath(p string) string {
	base := filepath.Base(p)
	switch {
	case strings.Contains(base, shellBash):
		return shellBash
	case strings.Contains(base, shellFish):
		return shellFish
	default:
		return shellZsh
	}
}

// renderRCBlock builds the contents of the managed block from the
// current stack list. The marker lines are emitted by upsertRCBlock;
// this function returns just the in-block content.
//
// The block:
//   - exports A_NOVEL_STACKS (if non-default or multi-stack)
//   - sources cobra-generated shell completion (so `a-novel <TAB>`
//     just works without the user installing _a-novel into fpath)
//   - calls `a-novel core start`
//
// Idempotent — re-running setup with the same stack list + shell
// produces byte-identical output.
func renderRCBlock(stk []stacks.Stack, shell string) string {
	var b strings.Builder
	b.WriteString("# Managed by `a-novel core setup` — do not edit between markers.\n")
	b.WriteString("# Re-run `a-novel core setup` to update; delete both markers to opt out.\n")
	// Only emit the export if the user has anything beyond the implicit
	// default — saves a noisy export for the common single-stack case.
	if needsExport(stk) {
		b.WriteString("export A_NOVEL_STACKS='")
		b.WriteString(serializeStacks(stk))
		b.WriteString("'\n")
	}
	// Shell completion. The `command -v` guard means a missing binary
	// (e.g., uninstalled CLI) doesn't error every shell startup.
	switch shell {
	case shellBash:
		b.WriteString("command -v a-novel >/dev/null 2>&1 && source <(a-novel completion bash)\n")
	case shellFish:
		// Fish uses pipe-to-source rather than process-substitution.
		b.WriteString("command -v a-novel >/dev/null 2>&1; and a-novel completion fish | source\n")
	default: // zsh
		b.WriteString("command -v a-novel >/dev/null 2>&1 && source <(a-novel completion zsh)\n")
	}
	b.WriteString("a-novel core start\n")
	return b.String()
}

func needsExport(stk []stacks.Stack) bool {
	if len(stk) != 1 {
		return true
	}
	// Single stack — emit only if its path differs from the implicit
	// default (~/git-projects/a-novel).
	home, _ := os.UserHomeDir()
	defaultPath := filepath.Join(home, "git-projects", "a-novel")
	return stk[0].Path != defaultPath
}

func serializeStacks(stk []stacks.Stack) string {
	// Stable order: keep the input order (first = default per stacks
	// package contract).
	parts := make([]string, 0, len(stk))
	for _, s := range stk {
		parts = append(parts, s.Name+":"+s.Path)
	}
	return strings.Join(parts, ",")
}

// upsertRCBlock writes a backup-then-update of the rc file. Returns
// (backupPath, changed, err). `changed` is false when the file already
// contains the exact desired block — no write happens in that case.
//
// Idempotency rules (spec §3.5):
//   - block present + content identical → no write
//   - block present + content differs → in-place replace
//   - block absent → append with leading blank line
//   - malformed markers → refuse with a clear message
func upsertRCBlock(rcPath, blockContent string) (string, bool, error) {
	// File doesn't exist yet — create with just the block.
	existing := ""
	if data, err := os.ReadFile(rcPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return "", false, fmt.Errorf("read %s: %w", rcPath, err)
	}

	desired := beginMarker + "\n" + blockContent + endMarker + "\n"

	switch loc := locateBlock(existing); loc.kind {
	case blockAbsent:
		// Append with a leading blank line if the file ends in non-empty content.
		var nu strings.Builder
		nu.WriteString(existing)
		if existing != "" && !strings.HasSuffix(existing, "\n") {
			nu.WriteString("\n")
		}
		if existing != "" {
			nu.WriteString("\n")
		}
		nu.WriteString(desired)
		newContent := nu.String()
		if newContent == existing {
			return "", false, nil // pathological: nothing to do
		}
		backup, err := writeWithBackup(rcPath, []byte(newContent))
		return backup, true, err
	case blockPresent:
		// Replace the existing block in place.
		newContent := existing[:loc.beginStart] + desired + existing[loc.endStop:]
		if newContent == existing {
			return "", false, nil
		}
		backup, err := writeWithBackup(rcPath, []byte(newContent))
		return backup, true, err
	case blockMalformed:
		return "", false, fmt.Errorf(
			"shell rc at %s has malformed a-novel markers (begin marker without matching end, or vice versa); "+
				"restore both markers around the existing managed block, or delete both markers entirely and re-run setup",
			rcPath)
	}
	return "", false, nil
}

type blockKind int

const (
	blockAbsent blockKind = iota
	blockPresent
	blockMalformed
)

type blockLoc struct {
	kind                blockKind
	beginStart, endStop int // [beginStart:endStop] covers the whole block including markers + trailing newline
}

// locateBlock scans `content` for the begin/end markers. Returns
// blockAbsent if both are missing; blockPresent with byte offsets if
// both are present in order; blockMalformed if exactly one is present.
func locateBlock(content string) blockLoc {
	begin := strings.Index(content, beginMarker)
	end := strings.Index(content, endMarker)
	if begin < 0 && end < 0 {
		return blockLoc{kind: blockAbsent}
	}
	if begin < 0 || end < 0 || begin > end {
		return blockLoc{kind: blockMalformed}
	}
	// Stop after the end marker's trailing newline (if any) so the
	// replacement doesn't leave a stray empty line.
	stop := end + len(endMarker)
	if stop < len(content) && content[stop] == '\n' {
		stop++
	}
	return blockLoc{kind: blockPresent, beginStart: begin, endStop: stop}
}

// writeWithBackup copies rcPath to a timestamped sibling before
// overwriting. Backups beyond maxRCBackups are pruned (oldest first).
// Returns the backup file path so the caller can surface it.
func writeWithBackup(rcPath string, newContent []byte) (string, error) {
	existing, err := os.ReadFile(rcPath)
	var backupPath string
	if err == nil && len(existing) > 0 {
		stamp := time.Now().UTC().Format("2006-01-02T15-04-05Z")
		backupPath = rcPath + ".a-novel-backup-" + stamp
		if err := os.WriteFile(backupPath, existing, 0o600); err != nil {
			return "", fmt.Errorf("write backup %s: %w", backupPath, err)
		}
		pruneOldRCBackups(rcPath)
	}
	// Write atomically — write to temp + rename — so a partial write
	// doesn't leave the rc in a broken state.
	tmp := rcPath + ".a-novel-tmp"
	if err := os.WriteFile(tmp, newContent, 0o600); err != nil {
		return backupPath, fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, rcPath); err != nil {
		return backupPath, fmt.Errorf("rename tmp: %w", err)
	}
	return backupPath, nil
}

// pruneOldRCBackups keeps the maxRCBackups newest backup siblings of
// rcPath.
func pruneOldRCBackups(rcPath string) {
	dir := filepath.Dir(rcPath)
	base := filepath.Base(rcPath) + ".a-novel-backup-"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type entry struct {
		path string
		mt   time.Time
	}
	var backups []entry
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), base) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		backups = append(backups, entry{path: filepath.Join(dir, e.Name()), mt: info.ModTime()})
	}
	if len(backups) <= maxRCBackups {
		return
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].mt.Before(backups[j].mt) })
	for _, b := range backups[:len(backups)-maxRCBackups] {
		_ = os.Remove(b.path)
	}
}

// expandHome turns a leading "~" into $HOME.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
