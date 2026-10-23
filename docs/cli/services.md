# Run services

A service runs as **targets**, the programs in its `cmd/` directory. A server stays up, and a job such as `migrations` runs once. Its **infrastructure** is what it depends on, such as its database. Name a target `<service>/<target>`:

```bash
SERVICE=service-json-keys
TARGET=rest
```

## Find what to run

```bash
a-novel run ps                              # every service and its state
a-novel run ps --service="$SERVICE"         # one service and its state
a-novel run topology --service="$SERVICE"   # what its targets depend on
```

## Start and stop a target

```bash
a-novel run start "$SERVICE/$TARGET"
```

Dependencies start first: the database, then migrations, then the target. Targets run from source. Run the container image instead with `--mode=container`:

```bash
a-novel run start "$SERVICE/$TARGET" --mode=container
```

```bash
a-novel run restart "$SERVICE/$TARGET"
a-novel run kill "$SERVICE/$TARGET"
```

Infrastructure outlives its targets, so restarts are quick. Start it alone:

```bash
a-novel run service infra start "$SERVICE"
```

Stop it once no target runs, or with `--force`:

```bash
a-novel run service infra kill "$SERVICE"
```

## Read the logs

```bash
a-novel run logs "$SERVICE/$TARGET" --follow     # live, until Ctrl+C
a-novel run logs "$SERVICE/$TARGET" --tail=100   # the last 100 lines
a-novel run logs "$SERVICE/$TARGET" --previous   # the run before this
```

Ctrl+C stops following, not the target. Infrastructure logs stream the same way, by the ID `run ps` shows.

## Reach a running service

Ports change on every run; ask the CLI:

```bash
eval "$(a-novel run env "$SERVICE")"
curl "$REST_URL/ping"
```

`run exec` runs a command with a target's environment, inside its container when it has one. A command on your machine reaches the database through `POSTGRES_DSN` or the discrete `POSTGRES_*` settings:

```bash
a-novel run exec "$SERVICE/migrations" -- sh -c 'psql "$POSTGRES_DSN"'
```

For a target running from source, get the debugger's attach command:

```bash
a-novel run debug "$SERVICE/$TARGET"
```

## Script it

`run watch` streams state changes as JSON, one object per line:

```bash
a-novel run watch --service="$SERVICE"
```

`run ps --json` and `run env --format=json` cover the rest.
