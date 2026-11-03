package reinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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
	TargetID string `json:"target_id"`
	// Env holds the host ports the target's env references, as KEY=port
	// entries. A relaunch reads only those entries, so a checkpoint holding the
	// whole env relaunches the same way.
	Env []string `json:"env"`
}

// Write serializes cp to paths.ReinstallCheckpoint() and fsyncs it, writing to
// a temporary file and renaming so a crash never leaves a corrupt checkpoint.
// It stamps the schema and timestamp itself.
func Write(cp Checkpoint) error {
	cp.Schema = SchemaVersion
	cp.WrittenAt = time.Now()
	path := paths.ReinstallCheckpoint()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp := path + ".tmp"
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
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Read returns the checkpoint at paths.ReinstallCheckpoint(), or (nil, nil)
// when none exists, the common case at a fresh start. A malformed file or an
// unknown schema is an error. The daemon logs it and discards the checkpoint,
// so the handoff degrades to "containers survive, go-exec targets are lost".
func Read() (*Checkpoint, error) {
	path := paths.ReinstallCheckpoint()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if cp.Schema != SchemaVersion {
		return nil, fmt.Errorf("%s has schema %d, want %d", path, cp.Schema, SchemaVersion)
	}
	return &cp, nil
}

// Delete removes the checkpoint. Idempotent.
func Delete() error {
	if err := os.Remove(paths.ReinstallCheckpoint()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Exists reports whether the checkpoint file is present. Status uses it to
// surface "reinstall pending".
func Exists() bool {
	_, err := os.Stat(paths.ReinstallCheckpoint())
	return err == nil
}

// EnsureSinglePending guards against a double checkpoint. A checkpoint already
// present when PrepareReinstall arrives yields an error and leaves the file
// untouched, and the caller resolves it, usually by aborting the second
// install.
func EnsureSinglePending() error {
	if Exists() {
		return errors.New("reinstall checkpoint already present — another install in flight or a previous one didn't complete")
	}
	return nil
}
