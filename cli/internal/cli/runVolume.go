package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
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
	req := &anovelv1.ListVolumesRequest{}
	cmd := &cobra.Command{
		Use:   "list <service>",
		Short: "List a service's volumes with sizes and backup counts",
		Long: `Print one row per volume the service's compose file declares: its name, its
size on disk, and how many backups 'volume restore' can take it back to. A
volume not created yet reads '-' for its size.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Service = args[0]
			resp, err := rpc.Call(cmd.Context(), rpc.New().ListVolumes, req)
			if err != nil {
				return err
			}
			printVolumes(cmd.OutOrStdout(), resp.GetVolumes())
			return nil
		},
	}
	addStackFlag(cmd, &req.Stack)
	return cmd
}

// printVolumes prints one row per volume: its name, size and backup count.
func printVolumes(out io.Writer, volumes []*anovelv1.Volume) {
	if len(volumes) == 0 {
		_, _ = fmt.Fprintln(out, "(no volumes declared)")
		return
	}
	_, _ = fmt.Fprintf(out, "%-40s %12s %8s\n", "VOLUME", "SIZE", "BACKUPS")
	for _, v := range volumes {
		_, _ = fmt.Fprintf(out, "%-40s %12s %8d\n", v.GetName(), humanBytes(v.GetSizeBytes()), v.GetBackupCount())
	}
}

func newVolumeBackupCmd() *cobra.Command {
	req := &anovelv1.BackupVolumeRequest{}
	cmd := &cobra.Command{
		Use:   "backup <service>",
		Short: "Snapshot a service's volumes (refuses while the service runs)",
		Long: `Archive each of the service's volumes into the backups directory, and print
each archive's path. An archive of a volume in use can be corrupt, so the
command refuses while the service runs; --force stops it first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Service = args[0]
			resp, err := rpc.Call(cmd.Context(), rpc.New().BackupVolume, req)
			if err != nil {
				return err
			}
			for _, p := range resp.GetArchivePaths() {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), p)
			}
			return nil
		},
	}
	addStackFlag(cmd, &req.Stack)
	cmd.Flags().StringVar(&req.Tag, "tag", "", "user-supplied label included in the archive filename")
	cmd.Flags().BoolVarP(&req.Force, "force", "f", false, "cascade-stop the service before backing up")
	return cmd
}

func newVolumeRestoreCmd() *cobra.Command {
	req := &anovelv1.RestoreVolumeRequest{}
	cmd := &cobra.Command{
		Use:   "restore <service>",
		Short: "Restore volumes from a backup (latest by default)",
		Long: `Restore each of the service's volumes from its latest backup, or from the one
whose file name starts with --from, such as the timestamp 'volume backup'
printed. Every archive is checked before any volume changes. Refuses while
the service runs; --force stops it first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Service = args[0]
			resp, err := rpc.Call(cmd.Context(), rpc.New().RestoreVolume, req)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "restored: %v\n", resp.GetRestoredVolumes())
			return nil
		},
	}
	addStackFlag(cmd, &req.Stack)
	cmd.Flags().StringVar(&req.From, "from", "", "specific backup timestamp (default: latest)")
	cmd.Flags().BoolVarP(&req.Force, "force", "f", false, "cascade-stop the service before restoring")
	return cmd
}

func newVolumeClearCmd() *cobra.Command {
	req := &anovelv1.ClearVolumeRequest{}
	cmd := &cobra.Command{
		Use:   "clear <service>",
		Short: "Destroy a service's volumes (backs them up first)",
		Long: `Delete every volume of the service, after a backup that 'volume restore' takes
back. --no-backup skips the backup, and nothing can undo the deletion then.
Refuses while the service runs; --force stops it first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Service = args[0]
			resp, err := rpc.Call(cmd.Context(), rpc.New().ClearVolume, req)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cleared: %v\n", resp.GetClearedVolumes())
			return nil
		},
	}
	addStackFlag(cmd, &req.Stack)
	cmd.Flags().BoolVar(&req.NoBackup, "no-backup", false, "skip the auto-backup (irreversible)")
	cmd.Flags().BoolVarP(&req.Force, "force", "f", false, "cascade-stop the service before clearing")
	return cmd
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
