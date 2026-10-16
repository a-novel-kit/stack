# Back up service data

A service keeps its data, such as its database, in Podman volumes that outlive its containers: stopping a service or the daemon never deletes them. This runbook saves that data, brings it back, or resets the service to empty.

Volumes belong to the service, shared by all its targets. Set the service, then run the commands as they are:

```bash
SERVICE=service-json-keys
```

```bash
a-novel run volume list "$SERVICE"     # volumes, sizes and backup counts
```

## Back up and restore

A snapshot needs the service stopped, so it cannot catch a half-written state. Every command below refuses while anything of the service runs; `--force` stops it all first.

```bash
BACKUP_TAG=before-migration
a-novel run volume backup "$SERVICE" --tag="$BACKUP_TAG"
```

```bash
a-novel run volume restore "$SERVICE"                  # the latest backup
```

To restore an older one, pass its timestamp from `volume list` with `--from`. The CLI keeps a few recent backups of each volume and prunes older ones.

## Reset to empty

```bash
a-novel run volume clear "$SERVICE"
```

Clearing backs the volumes up first, so the step can be undone with `volume restore`. The next start recreates the database and runs the migrations. `--no-backup` skips the safety copy.
