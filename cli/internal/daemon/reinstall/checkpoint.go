// Package reinstall implements the daemon's reinstall-handoff
// checkpoint per spec §3.6.
//
// Lifecycle:
//
//  1. PrepareReinstall RPC writes a JSON checkpoint listing every
//     running go-exec target (target ID, mode, env). Containers aren't
//     listed — they survive the daemon's death anyway.
//  2. Daemon exits cleanly; go-exec children receive SIGTERM via the
//     normal Kill path.
//  3. Install script overwrites the binary.
//  4. New daemon starts, observes the checkpoint at well-known path,
//     re-launches the listed go-exec targets, deletes the checkpoint.
//
// The checkpoint is single-purpose handoff state — the daemon's
// correctness doesn't depend on it (missing checkpoint = degraded
// behavior, not broken behavior). This preserves the spec §3.3
// stateless-recovery principle.
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

// SchemaVersion is bumped when Checkpoint's shape changes. An older
// daemon reading a newer schema (or vice versa) silently discards the
// checkpoint — handoff degrades gracefully rather than fails.
const SchemaVersion = 1

// Checkpoint is the JSON document persisted between daemon
// invocations.
type Checkpoint struct {
	Schema    int                `json:"schema"`
	WrittenAt time.Time          `json:"written_at"`
	GoExec    []GoExecCheckpoint `json:"go_exec"`
}

// GoExecCheckpoint is one running go-exec target's relaunch payload.
// Container targets are NOT recorded — podman keeps them alive across
// daemon restarts, and adoption (a separate phase 3 follow-up) picks
// them up.
type GoExecCheckpoint struct {
	TargetID string   `json:"target_id"`
	Env      []string `json:"env"`
}

// Path returns the canonical checkpoint location.
func Path() string { return paths.ReinstallCheckpoint() }

// Write serializes cp to Path(), fsync'd. Atomic via tmp+rename so a
// crash between write and rename doesn't leave a corrupt file. Stamps
// the schema + timestamp itself.
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

// Read returns the checkpoint at Path(), or (nil, nil) if no checkpoint
// exists (the common case at every fresh start). A schema mismatch or
// malformed file is returned as (nil, nil) — handoff degrades to
// "containers survive, go-exec targets are lost" rather than failing
// the daemon's startup.
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
		// Silently drop malformed checkpoint — see package doc.
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

// Exists reports whether the checkpoint file is present. Cheap — used by
// Status to surface "reinstall pending".
func Exists() bool {
	_, err := os.Stat(Path())
	return err == nil
}

// EnsureSinglePending guards against double-checkpoint races. If the
// checkpoint already exists when a new PrepareReinstall arrives,
// returns an error rather than overwriting (the caller is responsible
// for resolving — typically by aborting the second install).
func EnsureSinglePending() error {
	if Exists() {
		return errors.New("reinstall checkpoint already present — another install in flight or a previous one didn't complete")
	}
	return nil
}
