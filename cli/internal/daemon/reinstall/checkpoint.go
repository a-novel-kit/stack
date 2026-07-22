// Package reinstall implements the daemon's reinstall-handoff
// checkpoint.
//
// Lifecycle:
//
//  1. PrepareReinstall RPC writes a JSON checkpoint listing every running
//     go-exec target. Containers survive the daemon's death on their own, so
//     they stay out of it.
//  2. Daemon exits cleanly; go-exec children receive SIGTERM via the
//     normal Kill path.
//  3. Install script overwrites the binary.
//  4. New daemon starts, observes the checkpoint at well-known path,
//     re-launches the listed go-exec targets, deletes the checkpoint.
//
// The checkpoint is handoff state only: losing it degrades behavior without
// breaking it, which keeps the daemon's recovery stateless.
package reinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

// SchemaVersion is bumped when Checkpoint's shape changes. A daemon reading a
// schema it does not recognize discards the checkpoint, so handoff degrades
// gracefully.
const SchemaVersion = 1

// Checkpoint is the JSON document persisted between daemon
// invocations.
type Checkpoint struct {
	Schema    int                `json:"schema"`
	WrittenAt time.Time          `json:"written_at"`
	GoExec    []GoExecCheckpoint `json:"go_exec"`
}

// GoExecCheckpoint is one running go-exec target's relaunch payload. Container
// targets stay out: podman keeps them alive across a daemon restart, and orphan
// adoption picks them up.
type GoExecCheckpoint struct {
	TargetID string   `json:"target_id"`
	Env      []string `json:"env"`
}

// Path returns the canonical checkpoint location.
func Path() string { return paths.ReinstallCheckpoint() }

// Write serializes cp to Path() and fsyncs it, writing to a temporary file and
// renaming so a crash never leaves a corrupt checkpoint. It stamps the schema
// and timestamp itself.
func Write(cp Checkpoint) error {
	cp.Schema = SchemaVersion
	cp.WrittenAt = time.Now()
	dir := filepath.Dir(Path())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp := Path() + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cp); err != nil {
		_ = f.Close()
		return fmt.Errorf("encode: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("fsync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmp, Path()); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Read returns the checkpoint at Path(), or (nil, nil) when none exists, the
// common case at a fresh start. A schema mismatch or malformed file also yields
// (nil, nil), degrading handoff to "containers survive, go-exec targets are
// lost" rather than failing the daemon's startup.
func Read() (*Checkpoint, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", Path(), err)
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		// A malformed checkpoint is dropped rather than surfaced.
		return nil, nil //nolint:nilerr // intentional: malformed checkpoint is non-fatal
	}
	if cp.Schema != SchemaVersion {
		return nil, nil
	}
	return &cp, nil
}

// Delete removes the checkpoint. Idempotent.
func Delete() error {
	if err := os.Remove(Path()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Exists reports whether the checkpoint file is present. Status uses it to
// surface "reinstall pending".
func Exists() bool {
	_, err := os.Stat(Path())
	return err == nil
}

// EnsureSinglePending guards against a double checkpoint. A checkpoint already
// present when PrepareReinstall arrives yields an error instead of an
// overwrite, leaving the caller to resolve it, usually by aborting the second
// install.
func EnsureSinglePending() error {
	if Exists() {
		return errors.New("reinstall checkpoint already present — another install in flight or a previous one didn't complete")
	}
	return nil
}
