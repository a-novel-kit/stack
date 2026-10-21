# Back up service data

A service keeps its data in volumes that survive restarts. Backup, restore and clear need the service stopped. `--force` stops it first.

```bash
SERVICE=service-json-keys
a-novel run volume list "$SERVICE"
```

## Back up and restore

```bash
BACKUP_TAG=before-migration
a-novel run volume backup "$SERVICE" --tag="$BACKUP_TAG"
```

```bash
a-novel run volume restore "$SERVICE"
```

To restore an older backup, pass `--from` with the timestamp from its file name. `volume backup` prints that name.

## Reset to empty

```bash
a-novel run volume clear "$SERVICE"
```

Clearing backs up first, so `volume restore` undoes it. The next start recreates the database.
