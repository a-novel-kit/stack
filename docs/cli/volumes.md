# Back up service data

A service keeps its data in volumes that survive restarts.

```bash
SERVICE=service-json-keys
a-novel run volume list "$SERVICE"
```

## Back up and restore

These commands need the service stopped; `--force` stops it first.

```bash
BACKUP_TAG=before-migration
a-novel run volume backup "$SERVICE" --tag="$BACKUP_TAG"
```

```bash
a-novel run volume restore "$SERVICE"
```

`--from` restores an older backup, by the timestamp `volume list` shows.

## Reset to empty

```bash
a-novel run volume clear "$SERVICE"
```

Clearing backs up first, so `volume restore` undoes it. The next start recreates the database.
