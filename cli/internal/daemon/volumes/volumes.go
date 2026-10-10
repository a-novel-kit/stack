// Package volumes owns the daemon-side volume operations: list, backup,
// restore, clear — all service-scoped.
//
// Backup format: `podman volume export` produces a tar stream; we pipe
// it through zstd into $XDG_DATA_HOME/a-novel/backups/<stack>/<service>/<volume>/<ts>.tar.zst.
// Restore reverses: zstd-decompress + `podman volume import`. Clear is
// auto-backup (unless --no-backup) + `podman volume rm -f`.
//
// A destructive operation runs only against a service whose infra and targets
// are down, since working on a live volume produces inconsistent state. The
// daemon's handlers enforce that, and --force cascade-kills the service first.
package volumes

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
	"github.com/a-novel-kit/stack/cli/internal/shared/retention"
)

// maxBackupsPerVolume is how many of the most-recent backups to keep per
// volume; older archives are pruned as new ones land.
const maxBackupsPerVolume = 5

// Volume is a list-row for `volume list`.
type Volume struct {
	Name        string // bare name (without stack/service prefix)
	Service     string
	Stack       string
	SizeBytes   int64 // 0 if volume doesn't exist yet
	BackupCount int32 // archives in our backups dir
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

// List returns one Volume entry per compose-declared volume on the service.
// The size comes from podman, the backup count from the backups dir.
func List(svc *discovery.Service) []Volume {
	out := make([]Volume, 0, len(svc.Volumes))
	for _, v := range svc.Volumes {
		out = append(out, Volume{
			Name:        v.Name,
			Service:     svc.Name,
			Stack:       svc.Stack,
			SizeBytes:   podmanVolumeSize(PodmanVolumeName(svc.Stack, svc.Name, v.Name)),
			BackupCount: int32(len(retention.Files(BackupDir(svc.Stack, svc.Name, v.Name), isArchive))),
		})
	}
	return out
}

// podmanVolumeSize returns the volume's size on disk, from `du -sb` on the
// mountpoint `podman volume inspect` reports. A missing volume, or one du
// cannot measure, reports 0.
func podmanVolumeSize(fullName string) int64 {
	out, err := exec.Command("podman", "volume", "inspect", fullName,
		"--format", "{{.Mountpoint}}").Output()
	mountpoint := strings.TrimSpace(string(out))
	if err != nil || mountpoint == "" {
		return 0
	}
	if out, err = exec.Command("du", "-sb", mountpoint).Output(); err != nil {
		return 0
	}
	// du prints "<bytes>\t<path>".
	size, _, _ := strings.Cut(string(out), "\t")
	n, _ := strconv.ParseInt(size, 10, 64)
	return n
}

// isArchive reports whether a backups-dir entry is a backup archive.
func isArchive(name string) bool { return strings.HasSuffix(name, ".tar.zst") }

// =============================================================================
// Backup
// =============================================================================

// Backup writes a tar.zst archive of every compose-declared volume on the
// service. A non-empty tag is embedded in the filename for later
// identification. It returns the absolute path of each created archive.
//
// The caller owns the service-down pre-check; Backup itself runs
// unconditionally.
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
		// Read the archive back before calling it a backup: Clear destroys the
		// volume on the strength of this return value, so a truncated archive
		// has to surface now.
		if err := validateArchive(dest); err != nil {
			_ = os.Remove(dest)

			return archives, fmt.Errorf("verify backup of volume %s: %w", v.Name, err)
		}
		archives = append(archives, dest)
		// Prune to the retention limit only after a successful backup, so a
		// failed one never prunes away recovery state.
		retention.Prune(dir, isArchive, maxBackupsPerVolume)
	}
	return archives, nil
}

// backupOne runs `podman volume export <name>` and pipes it through zstd into
// dest. The stream never holds the volume's bytes in memory.
//
// A failed backup leaves no file behind, so every archive on disk is a complete
// one. Clear treats a successful backup as license to destroy the volume, and
// `restore` picks from the same directory.
func backupOne(volumeName, dest string) error {
	if err := writeBackup(volumeName, dest); err != nil {
		_ = os.Remove(dest)

		return err
	}

	return nil
}

// writeBackup exports the volume into dest, compressed.
//
// The closes carry the real errors. zstd.Encoder.Close writes the final block
// and the frame checksum, so it is the call a full disk surfaces in, after
// everything else looked fine. Every close error is returned, since a dropped
// one reports a truncated archive as a successful backup.
func writeBackup(volumeName, dest string) (err error) {
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer func() {
		if closeErr := out.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close %s: %w", dest, closeErr)
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

// sanitizeTag replaces shell-unfriendly chars with dashes so the filename is
// safe.
func sanitizeTag(tag string) string {
	return strings.Map(func(c rune) rune {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			return c
		default:
			return '-'
		}
	}, tag)
}

// =============================================================================
// Restore
// =============================================================================

// Restore replaces each volume's contents from a backup archive. `from` selects
// the archive timestamp; empty takes each volume's most recent .tar.zst by
// mtime. Every archive is validated before any volume is touched. It returns
// the restored volume names.
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
	// 2. Every archive is valid, so restore each in turn.
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

// resolveBackup returns the path to the timestamped archive named by `from`, or
// to the most recent archive in dir when it is empty.
func resolveBackup(dir, from string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read backups dir %s: %w", dir, err)
	}
	if from != "" {
		// A prefix match covers both `<ts>.tar.zst` and `<ts>.<tag>.tar.zst`. A bare
		// timestamp matches every tagged variant of it, so returning the first — which
		// os.ReadDir sorts by name, putting .auto-pre-clear ahead of the plain
		// archive — restores something other than what the operator named. An
		// ambiguous prefix is an error they resolve with a fuller name; a full
		// filename prefix-matches only itself.
		var matches []string

		for _, e := range entries {
			if strings.HasPrefix(e.Name(), from) && isArchive(e.Name()) {
				matches = append(matches, e.Name())
			}
		}

		switch len(matches) {
		case 0:
			return "", fmt.Errorf("no backup matching timestamp %q", from)
		case 1:
			return filepath.Join(dir, matches[0]), nil
		default:
			return "", fmt.Errorf("timestamp %q matches %d backups (%s); pass a longer prefix to pick one",
				from, len(matches), strings.Join(matches, ", "))
		}
	}
	archives := retention.Files(dir, isArchive)
	if len(archives) == 0 {
		return "", errors.New("no backups available")
	}
	return archives[len(archives)-1], nil
}

// validateArchive reads the archive through to confirm it decompresses cleanly.
// It runs up front, so a corrupt input never trashes a volume halfway.
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
	// `podman volume import` requires the target volume to exist.
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

// Clear destroys every volume on the service. It takes an auto-backup first, so
// undo is one `restore` away; noBackup makes the deletion irreversible.
//
// The caller owns the service-down pre-check.
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
