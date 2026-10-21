package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
)

func newVolumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "volume",
		Short: "Manage service volumes (backup, restore, clear, list)",
		Long: `A service's volumes hold its data, such as its database, for all its targets.
Backup, restore and clear refuse while a target or the infra of the service
runs; --force stops them first.

Backups are tar.zst archives under
$XDG_DATA_HOME/a-novel/backups/<stack>/<service>/<volume>/, the latest five
per volume. Stopping a service or the daemon keeps its volumes.`,
	}
	cmd.AddCommand(newVolumeListCmd())
	cmd.AddCommand(newVolumeBackupCmd())
	cmd.AddCommand(newVolumeRestoreCmd())
	cmd.AddCommand(newVolumeClearCmd())
	return cmd
}

func newVolumeListCmd() *cobra.Command {
	var ss stackScope
	cmd := &cobra.Command{
		Use:   "list <service>",
		Short: "List a service's volumes with sizes and backup counts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return listVolumes(cmd, ss.stack, args[0])
		},
	}
	ss.bind(cmd)
	return cmd
}

// listVolumes prints one row per volume of a service: its name, size and
// backup count.
func listVolumes(cmd *cobra.Command, stack, service string) error {
	resp, err := rpc.New("").ListVolumes(cmd.Context(), stack, service)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(resp.GetVolumes()) == 0 {
		_, _ = fmt.Fprintln(out, "(no volumes declared)")
		return nil
	}
	_, _ = fmt.Fprintf(out, "%-40s %12s %8s\n", "VOLUME", "SIZE", "BACKUPS")
	for _, v := range resp.GetVolumes() {
		_, _ = fmt.Fprintf(out, "%-40s %12s %8d\n", v.GetName(), humanBytes(v.GetSizeBytes()), v.GetBackupCount())
	}
	return nil
}

func newVolumeBackupCmd() *cobra.Command {
	var ss stackScope
	var tag string
	var force bool
	cmd := &cobra.Command{
		Use:   "backup <service>",
		Short: "Snapshot a service's volumes (refuses while the service runs)",
		Long: `Archive each of the service's volumes into the backups directory, and print
each archive's path. An archive of a volume in use can be corrupt, so the
command refuses while the service runs; --force stops it first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return backupVolumes(cmd, ss.stack, args[0], tag, force)
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&tag, "tag", "", "user-supplied label included in the archive filename")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "cascade-stop the service before backing up")
	return cmd
}

// backupVolumes archives a service's volumes, tag in the file names, and prints
// each archive's path.
func backupVolumes(cmd *cobra.Command, stack, service, tag string, force bool) error {
	resp, err := rpc.New("").BackupVolume(cmd.Context(), stack, service, tag, force)
	if err != nil {
		return err
	}
	for _, p := range resp.GetArchivePaths() {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), p)
	}
	return nil
}

func newVolumeRestoreCmd() *cobra.Command {
	var ss stackScope
	var from string
	var force bool
	cmd := &cobra.Command{
		Use:   "restore <service>",
		Short: "Restore volumes from a backup (latest by default)",
		Long: `Restore each of the service's volumes from its latest backup, or from the one
whose file name starts with --from, such as the timestamp 'volume backup'
printed. Every archive is checked before any volume changes. Refuses while
the service runs; --force stops it first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return restoreVolumes(cmd, ss.stack, args[0], from, force)
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&from, "from", "", "specific backup timestamp (default: latest)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "cascade-stop the service before restoring")
	return cmd
}

// restoreVolumes restores a service's volumes from the backup from selects,
// the latest when empty, and prints the volumes it restored.
func restoreVolumes(cmd *cobra.Command, stack, service, from string, force bool) error {
	resp, err := rpc.New("").RestoreVolume(cmd.Context(), stack, service, from, force)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "restored: %v\n", resp.GetRestoredVolumes())
	return nil
}

func newVolumeClearCmd() *cobra.Command {
	var ss stackScope
	var noBackup bool
	var force bool
	cmd := &cobra.Command{
		Use:   "clear <service>",
		Short: "Destroy a service's volumes (backs them up first)",
		Long: `Delete every volume of the service, after a backup that 'volume restore' takes
back. --no-backup skips the backup, and nothing can undo the deletion then.
Refuses while the service runs; --force stops it first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return clearVolumes(cmd, ss.stack, args[0], noBackup, force)
		},
	}
	ss.bind(cmd)
	cmd.Flags().BoolVar(&noBackup, "no-backup", false, "skip the auto-backup (irreversible)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "cascade-stop the service before clearing")
	return cmd
}

// clearVolumes empties a service's volumes, after a backup unless noBackup is
// set, and prints the volumes it cleared.
func clearVolumes(cmd *cobra.Command, stack, service string, noBackup, force bool) error {
	resp, err := rpc.New("").ClearVolume(cmd.Context(), stack, service, noBackup, force)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cleared: %v\n", resp.GetClearedVolumes())
	return nil
}

// humanBytes renders an int64 byte count in a short human form.
func humanBytes(n int64) string {
	const (
		k = 1024
		m = k * 1024
		g = m * 1024
	)
	switch {
	case n >= g:
		return fmt.Sprintf("%.1fG", float64(n)/g)
	case n >= m:
		return fmt.Sprintf("%.1fM", float64(n)/m)
	case n >= k:
		return fmt.Sprintf("%.1fK", float64(n)/k)
	case n > 0:
		return fmt.Sprintf("%dB", n)
	default:
		return "-"
	}
}
