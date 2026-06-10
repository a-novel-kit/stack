// Package paths centralizes filesystem locations the daemon and clients agree
// on. Single source of truth so a change to (e.g.) the socket path only needs
// to be made here.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// Socket returns the absolute path of the daemon's unix domain socket.
//
// Resolution priority (highest first):
//  1. $XDG_RUNTIME_DIR/a-novel.sock — the canonical XDG location.
//  2. /run/user/<uid>/a-novel.sock — the standard systemd-user runtime dir.
//  3. /tmp/a-novel-<uid>.sock — the universal fallback.
//
// The fallback chain is deterministic so the daemon and clients always agree
// without coordination through env vars or config files.
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

// State is $XDG_STATE_HOME/a-novel/ (default ~/.local/state/a-novel/).
// Logs and the reinstall checkpoint live here.
func State() string {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "a-novel")
	}
	return filepath.Join(home(), ".local", "state", "a-novel")
}

// Data is $XDG_DATA_HOME/a-novel/ (default ~/.local/share/a-novel/).
// Volume backups live here.
func Data() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "a-novel")
	}
	return filepath.Join(home(), ".local", "share", "a-novel")
}

// LogsRoot is the per-stack logs root (State()/logs/).
func LogsRoot() string { return filepath.Join(State(), "logs") }

// BackupsRoot is the per-stack volume-backups root (Data()/backups/).
func BackupsRoot() string { return filepath.Join(Data(), "backups") }

// ReinstallCheckpoint is the path of the one-shot reinstall handoff file.
// Single fixed location; presence == "new daemon should replay".
func ReinstallCheckpoint() string { return filepath.Join(State(), "reinstall.json") }

// home returns $HOME (falls back to "/" if unset, which would be a broken
// environment but avoids panicking inside path computation).
func home() string {
	if v := os.Getenv("HOME"); v != "" {
		return v
	}
	return "/"
}
