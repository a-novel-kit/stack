// Package volumes implements the daemon's operations on a service's volumes:
// list, backup, restore and clear.
//
// A backup is the `podman volume export` tar stream, compressed with zstd into
// $XDG_DATA_HOME/a-novel/backups/<stack>/<service>/<volume>/<timestamp>.tar.zst.
// A restore decompresses it into `podman volume import`, and a clear takes a
// backup, unless told otherwise, before `podman volume rm -f`.
//
// A destructive operation runs only against a service whose infra and targets
// are down, since working on a live volume produces inconsistent state. The
// daemon's handlers enforce that, and --force cascade-kills the service first.
package volumes
