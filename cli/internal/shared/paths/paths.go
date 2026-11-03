package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Socket returns the absolute path of the daemon's Unix domain socket. It
// prefers $XDG_RUNTIME_DIR, then the systemd user runtime directory
// /run/user/<uid>, and falls back to /tmp. The chain is deterministic so the
// daemon and clients agree on the location without coordinating through env
// vars or config files.
func Socket() string {
	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		return filepath.Join(v, "a-novel.sock")
	}
	uid := os.Getuid()
	if uid >= 0 {
		runDir := fmt.Sprintf("/run/user/%d", uid)
		if _, err := os.Stat(runDir); err == nil {
			return filepath.Join(runDir, "a-novel.sock")
		}
	}
	return fmt.Sprintf("/tmp/a-novel-%d.sock", uid)
}

// ErrForeignOwner reports a file another user created at a path the daemon and its clients use. The
// /tmp fallback of [Socket] is shared by every local user, so a socket or lock file found there is
// trusted only when this user owns it.
var ErrForeignOwner = errors.New("owned by another user")

// CheckOwner fails with [ErrForeignOwner] when path exists and belongs to another user. It reads the
// path itself, not a symlink's target, so a link another user planted fails too. A missing path
// passes: nothing answers there yet.
func CheckOwner(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if !OwnedByUser(info) {
		return fmt.Errorf("%s: %w", path, ErrForeignOwner)
	}
	return nil
}

// OwnedByUser reports whether info, from os.Lstat or File.Stat, belongs to the current user.
func OwnedByUser(info fs.FileInfo) bool {
	return ownedBy(info, os.Getuid())
}

// ownedBy reports whether info belongs to uid. A FileInfo without a unix owner belongs to no one.
func ownedBy(info fs.FileInfo, uid int) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == uid
}

// State is $XDG_STATE_HOME/a-novel/ (default ~/.local/state/a-novel/).
// Logs and the reinstall checkpoint live here.
func State() string {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "a-novel")
	}
	return filepath.Join(Home(), ".local", "state", "a-novel")
}

// Data is $XDG_DATA_HOME/a-novel/ (default ~/.local/share/a-novel/).
// Volume backups live here.
func Data() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "a-novel")
	}
	return filepath.Join(Home(), ".local", "share", "a-novel")
}

// LogsRoot is the per-stack logs root (State()/logs/).
func LogsRoot() string { return filepath.Join(State(), "logs") }

// BackupsRoot is the per-stack volume-backups root (Data()/backups/).
func BackupsRoot() string { return filepath.Join(Data(), "backups") }

// SecretsRoot is the local secrets store root (Data()/secrets/, so it follows
// $XDG_DATA_HOME). It holds the AES-256-GCM local key and the encrypted store,
// which the secrets package writes as owner-only files (0600) inside a 0700
// directory.
func SecretsRoot() string { return filepath.Join(Data(), "secrets") }

// ReinstallCheckpoint is the path of the reinstall handoff file. Its presence
// tells the next daemon to relaunch the targets it lists.
func ReinstallCheckpoint() string { return filepath.Join(State(), "reinstall.json") }

// DaemonLog is where a detached daemon writes its own output (State()/daemon.log),
// apart from the output of the targets it supervises, under LogsRoot().
func DaemonLog() string { return filepath.Join(State(), "daemon.log") }

// Home returns $HOME, or "/" when it is unset, so path computation always
// yields a path.
func Home() string {
	if v := os.Getenv("HOME"); v != "" {
		return v
	}
	return "/"
}

// ExpandHome turns a leading ~ of p into Home(). Only that prefix expands, so a
// shell-expansion mistake elsewhere in p surfaces as a broken path.
func ExpandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(Home(), rest)
	}
	if p == "~" {
		return Home()
	}
	return p
}
