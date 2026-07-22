// Package volumes owns the daemon-side volume operations: list, backup,
// restore, clear — all service-scoped.
//
// Backup format: `podman volume export` produces a tar stream; we pipe
// it through zstd into $XDG_DATA_HOME/a-novel/backups/<stack>/<service>/<volume>/<ts>.tar.zst.
// Restore reverses: zstd-decompress + `podman volume import`. Clear is
// auto-backup (unless --no-backup) + `podman volume rm -f`.
//
// Every destructive op (backup, restore, clear) pre-checks that the
// service's infra + targets are all down — running ops on a live
// volume produces inconsistent state. --force overrides
// after cascade-killing the service first.
package volumes

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
)

// maxBackupsPerVolume is how many of the most-recent backups to keep per
// volume; older archives are pruned as new ones land.
const maxBackupsPerVolume = 5

// Volume is a list-row for `volume list`.
type Volume struct {
	Name        string // bare name (without stack/service prefix)
	FullName    string // podman volume name (`<stack>_<service>_<name>`)
	Service     string
	Stack       string
	SizeBytes   int64 // 0 if volume doesn't exist yet
	BackupCount int32 // archives in our backups dir
	Exists      bool  // does the podman volume actually exist?
}

// PodmanVolumeName returns the podman-namespaced volume name from a
// bare compose volume name, following the per-stack convention
// `<stack>_<service>_<volume>`.
func PodmanVolumeName(stack, service, bareVolume string) string {
	return stack + "_" + service + "_" + bareVolume
}

// BackupDir is the per-volume backups directory.
func BackupDir(stack, service, volume string) string {
	return filepath.Join(paths.BackupsRoot(), stack, service, volume)
}

// =============================================================================
// List
// =============================================================================

// List returns one Volume entry per compose-declared volume on the
// service. Existence + size come from `podman volume inspect`; backup
// count comes from the backups dir.
func List(svc *discovery.Service) ([]Volume, error) {
	out := make([]Volume, 0, len(svc.Volumes))
	for _, v := range svc.Volumes {
		full := PodmanVolumeName(svc.Stack, svc.Name, v.Name)
		size, exists := podmanVolumeSize(full)
		count := countBackups(svc.Stack, svc.Name, v.Name)
		out = append(out, Volume{
			Name:        v.Name,
			FullName:    full,
			Service:     svc.Name,
			Stack:       svc.Stack,
			SizeBytes:   size,
			BackupCount: count,
			Exists:      exists,
		})
	}
	return out, nil
}

// podmanVolumeSize returns (size-on-disk, exists). When the volume
// doesn't exist yet, returns (0, false).
func podmanVolumeSize(fullName string) (int64, bool) {
	// Size comes from `du -sb` on the volume's mountpoint, which
	// `podman volume inspect` reports.
	mp, err := podmanVolumeMountpoint(fullName)
	if err != nil || mp == "" {
		return 0, false
	}
	out, err := exec.Command("du", "-sb", mp).Output()
	if err != nil {
		return 0, true // exists but size unknown
	}
	// du output: "<bytes>\t<path>"
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, true
	}
	var n int64
	for _, c := range fields[0] {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	return n, true
}

func podmanVolumeMountpoint(fullName string) (string, error) {
	out, err := exec.Command("podman", "volume", "inspect", fullName,
		"--format", "{{.Mountpoint}}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// =============================================================================
// Backup
// =============================================================================

// Backup writes a tar.zst archive of every compose-declared volume on
// the service. `tag`, if non-empty, is embedded in the filename for
// later identification. Returns the absolute paths of the created
// archives, one per volume.
//
// Caller is responsible for the pre-check (service down). Backup itself
// runs unconditionally — the caller decides whether to enforce the
// safety guard.
func Backup(svc *discovery.Service, tag string) ([]string, error) {
	timestamp := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	var archives []string
	for _, v := range svc.Volumes {
		full := PodmanVolumeName(svc.Stack, svc.Name, v.Name)
		dir := BackupDir(svc.Stack, svc.Name, v.Name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return archives, fmt.Errorf("mkdir %s: %w", dir, err)
		}
		filename := timestamp + ".tar.zst"
		if tag != "" {
			filename = timestamp + "." + sanitizeTag(tag) + ".tar.zst"
		}
		dest := filepath.Join(dir, filename)
		if err := backupOne(full, dest); err != nil {
			return archives, fmt.Errorf("backup volume %s: %w", v.Name, err)
		}
		// Read the archive back before calling it a backup. The check already
		// existed but ran only on restore — at the far end of the lifecycle, so a
		// truncated archive was discovered on the day it was needed. Clear destroys
		// the volume on the strength of this return value, so it has to be earned
		// here.
		if err := validateArchive(dest); err != nil {
			_ = os.Remove(dest)

			return archives, fmt.Errorf("verify backup of volume %s: %w", v.Name, err)
		}
		archives = append(archives, dest)
		// Prune past the retention limit AFTER successful backup, so a
		// failed backup doesn't trigger a prune that loses recovery
		// state.
		pruneOldBackups(dir)
	}
	return archives, nil
}

// backupOne runs `podman volume export <name>` and pipes through zstd
// into `dest`. Stream-oriented — the volume's bytes never sit in memory.
//
// A failed backup leaves no file behind. Clear reads a successful backup as
// licence to destroy the volume, and a truncated archive left on disk would
// also be offered by `restore --previous` — so a partial write is worse than
// no write at all.
func backupOne(volumeName, dest string) error {
	if err := writeBackup(volumeName, dest); err != nil {
		_ = os.Remove(dest)

		return err
	}

	return nil
}

// writeBackup exports the volume into dest, compressed.
//
// The closes are the crux rather than bookkeeping: zstd.Encoder.Close writes
// the final block and the frame checksum, so it is precisely the call that can
// fail after everything else has looked fine — a full disk surfaces here, not
// in cmd.Run, whose bytes were only accepted into the encoder's buffer.
// Discarding it reports a truncated archive as a successful backup.
func writeBackup(volumeName, dest string) error {
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	// Guards the early returns only; the success path closes explicitly below so
	// the error is reported rather than dropped.
	closed := false

	defer func() {
		if !closed {
			_ = out.Close()
		}
	}()

	cmd := exec.Command("podman", "volume", "export", volumeName)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("podman volume export: stdout pipe: %w", err)
	}

	var stderr strings.Builder

	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("podman volume export: %w (stderr: %s)", err, stderr.String())
	}

	if err := compressTo(out, stdout); err != nil {
		_ = cmd.Wait()

		return err
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("podman volume export: %w (stderr: %s)", err, stderr.String())
	}

	// Reach the platter before reporting success: an archive still in the page
	// cache is not one the volume can be destroyed against.
	if err := out.Sync(); err != nil {
		return fmt.Errorf("fsync %s: %w", dest, err)
	}

	closed = true

	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dest, err)
	}

	return nil
}

// compressTo streams src into dst through zstd, reporting the flush that
// completes the frame.
func compressTo(dst io.Writer, src io.Reader) error {
	enc, err := zstd.NewWriter(dst)
	if err != nil {
		return fmt.Errorf("zstd writer: %w", err)
	}

	if _, err := io.Copy(enc, src); err != nil {
		_ = enc.Close()

		return fmt.Errorf("compress volume: %w", err)
	}

	if err := enc.Close(); err != nil {
		return fmt.Errorf("flush zstd frame: %w", err)
	}

	return nil
}

// sanitizeTag drops shell-unfriendly chars so the filename is safe.
func sanitizeTag(tag string) string {
	keep := func(c rune) rune {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			return c
		default:
			return '-'
		}
	}
	out := make([]rune, 0, len(tag))
	for _, c := range tag {
		out = append(out, keep(c))
	}
	return string(out)
}

// pruneOldBackups keeps the maxBackupsPerVolume most-recent files, by
// modification time, removing the rest.
func pruneOldBackups(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type entry struct {
		path string
		mt   time.Time
	}
	var files []entry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tar.zst") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, entry{path: filepath.Join(dir, e.Name()), mt: info.ModTime()})
	}
	if len(files) <= maxBackupsPerVolume {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mt.Before(files[j].mt) })
	for _, f := range files[:len(files)-maxBackupsPerVolume] {
		_ = os.Remove(f.path)
	}
}

// =============================================================================
// Restore
// =============================================================================

// Restore replaces each volume's contents with a backup archive. `from`
// selects the archive timestamp; empty means "latest backup for each
// volume" (the most recent .tar.zst by mtime in the per-volume dir).
//
// Validates the archive (decompress sanity-check)
// before touching any volume. Returns the list of restored volume
// names.
func Restore(svc *discovery.Service, from string) ([]string, error) {
	// 1. Resolve the source archive for each volume.
	plans := make([]restorePlan, 0, len(svc.Volumes))
	for _, v := range svc.Volumes {
		dir := BackupDir(svc.Stack, svc.Name, v.Name)
		archive, err := resolveBackup(dir, from)
		if err != nil {
			return nil, fmt.Errorf("locate backup for %s: %w", v.Name, err)
		}
		if err := validateArchive(archive); err != nil {
			return nil, fmt.Errorf("validate archive %s: %w", archive, err)
		}
		plans = append(plans, restorePlan{
			volume:  PodmanVolumeName(svc.Stack, svc.Name, v.Name),
			archive: archive,
			bare:    v.Name,
		})
	}
	// 2. After all archives validated, restore each.
	var restored []string
	for _, p := range plans {
		if err := restoreOne(p.volume, p.archive); err != nil {
			return restored, fmt.Errorf("restore %s: %w", p.bare, err)
		}
		restored = append(restored, p.bare)
	}
	return restored, nil
}

type restorePlan struct{ volume, archive, bare string }

// resolveBackup returns the path to either a specific timestamped
// archive (when from != "") or the most-recent one in dir.
func resolveBackup(dir, from string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read backups dir %s: %w", dir, err)
	}
	if from != "" {
		// Match prefix — the file may be `<ts>.tar.zst` or
		// `<ts>.<tag>.tar.zst`, so prefix-match handles both.
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), from) && strings.HasSuffix(e.Name(), ".tar.zst") {
				return filepath.Join(dir, e.Name()), nil
			}
		}
		return "", fmt.Errorf("no backup matching timestamp %q", from)
	}
	// Newest by mtime.
	type cand struct {
		path string
		mt   time.Time
	}
	var cands []cand
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tar.zst") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		cands = append(cands, cand{path: filepath.Join(dir, e.Name()), mt: info.ModTime()})
	}
	if len(cands) == 0 {
		return "", errors.New("no backups available")
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mt.After(cands[j].mt) })
	return cands[0].path, nil
}

// validateArchive opens the archive and reads through to ensure it
// decompresses cleanly. Done up-front so we don't trash
// a volume halfway through with a corrupt input.
func validateArchive(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	dec, err := zstd.NewReader(f)
	if err != nil {
		return fmt.Errorf("open zstd reader: %w", err)
	}
	defer dec.Close()
	if _, err := io.Copy(io.Discard, dec); err != nil {
		return fmt.Errorf("decompress check: %w", err)
	}
	return nil
}

// restoreOne pipes a zstd-decompressed archive into `podman volume import`.
func restoreOne(volumeName, archive string) error {
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("open %s: %w", archive, err)
	}
	defer func() { _ = f.Close() }()
	dec, err := zstd.NewReader(f)
	if err != nil {
		return fmt.Errorf("zstd reader: %w", err)
	}
	defer dec.Close()
	// Ensure the target volume exists; podman volume import requires it.
	_ = exec.Command("podman", "volume", "create", volumeName).Run()
	cmd := exec.Command("podman", "volume", "import", volumeName, "-")
	cmd.Stdin = dec
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("podman volume import: %w (stderr: %s)", err, stderr.String())
	}
	return nil
}

// =============================================================================
// Clear
// =============================================================================

// Clear destroys every volume on the service. When noBackup is false
// (the default), takes an auto-backup first so undo is one
// `restore --previous` away. With noBackup=true, deletion is
// irreversible.
//
// Caller is responsible for the pre-check (service down).
func Clear(svc *discovery.Service, noBackup bool) ([]string, error) {
	if !noBackup {
		if _, err := Backup(svc, "auto-pre-clear"); err != nil {
			return nil, fmt.Errorf("auto-backup before clear: %w", err)
		}
	}
	var cleared []string
	for _, v := range svc.Volumes {
		full := PodmanVolumeName(svc.Stack, svc.Name, v.Name)
		cmd := exec.Command("podman", "volume", "rm", "-f", full)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return cleared, fmt.Errorf("rm volume %s: %w\n%s", v.Name, err, string(out))
		}
		cleared = append(cleared, v.Name)
	}
	return cleared, nil
}

// countBackups returns the number of .tar.zst files in the per-volume
// backups dir.
func countBackups(stack, service, volume string) int32 {
	dir := BackupDir(stack, service, volume)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var n int32
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".tar.zst") {
			n++
		}
	}
	return n
}
