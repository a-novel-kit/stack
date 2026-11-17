package reinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil"
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

// Write serializes cp to the checkpoint at path atomically and synced to disk,
// so a crash never leaves a corrupt checkpoint.
// It stamps the schema and timestamp itself.
func Write(path string, cp Checkpoint) error {
	cp.Schema = SchemaVersion
	cp.WrittenAt = time.Now()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if err := fsutil.WriteAtomic(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Read returns the checkpoint at path, or (nil, nil) when none exists, the
// common case at a fresh start. A malformed file or an unknown schema is an
// error. The daemon logs it and discards the checkpoint, so the handoff
// degrades to "containers survive, go-exec targets are lost".
func Read(path string) (*Checkpoint, error) {
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

// Delete removes the checkpoint at path. Idempotent.
func Delete(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Exists reports whether the checkpoint at path is present. Status uses it to
// surface "reinstall pending".
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// EnsureSinglePending guards against a double checkpoint. A checkpoint already
// present at path when PrepareReinstall arrives yields an error and leaves the
// file untouched, and the caller resolves it, usually by aborting the second
// install.
func EnsureSinglePending(path string) error {
	if Exists(path) {
		return errors.New("reinstall checkpoint already present: another install in flight or a previous one didn't complete")
	}
	return nil
}
