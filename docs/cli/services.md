# Run services

This runbook starts a service on your machine, reads its logs and reaches it. Each service is made of **targets**, the programs under its `cmd/` directory: a server stays up and answers requests, a job such as `migrations` runs once and exits. Its **infrastructure** is the containers it depends on, such as its Postgres database.

You name a target `<service>/<target>`. The examples use variables: set them to the service and target you want, then run the commands as they are.

```bash
SERVICE=service-json-keys
TARGET=rest
```

## Find what to run

```bash
a-novel run ps                                   # every service, its targets and their state
a-novel run topology --service="$SERVICE"        # what the service's targets depend on
a-novel run service status "$SERVICE"            # one service in detail
```

## Start and stop a target

```bash
a-novel run start "$SERVICE/$TARGET"
```

Its dependencies come up first: the database, then one-shot jobs such as migrations, then the target itself. Starting a target that already runs does nothing.

A target runs from source by default, the fastest loop while you edit code. Run it from its container image instead to test the image:

```bash
a-novel run start "$SERVICE/$TARGET" --mode=container
```

```bash
a-novel run restart "$SERVICE/$TARGET"           # stop, then start again
a-novel run kill "$SERVICE/$TARGET"              # stop the target
```

The infrastructure stays up when a target stops, so the next start is quick. Manage it on its own:

```bash
a-novel run service infra start "$SERVICE"       # infrastructure and one-shot jobs, no target
a-novel run service infra kill "$SERVICE"        # refused while a target runs
a-novel run service infra kill "$SERVICE" --force   # stop the targets too
```

## Read the logs

```bash
a-novel run logs "$SERVICE/$TARGET" --follow     # stream until Ctrl+C, which leaves the target running
a-novel run logs "$SERVICE/$TARGET" --tail=100   # the last 100 lines
a-novel run logs "$SERVICE/$TARGET" --previous   # the run before this one
```

The CLI keeps each target's current run and its five previous ones. An infrastructure container streams too, by its ID from `run ps`, such as `service-json-keys/infra/postgres-json-keys`.

## Reach a running service

Ports change on every run, so ask the CLI for them rather than guessing. `run env` prints the variables a target receives: ports, URLs, the database DSN.

```bash
eval "$(a-novel run env "$SERVICE")"
curl "$REST_URL/ping"
```

`run exec` runs a command beside a target. For a container, it runs inside it; otherwise it runs on your machine with the target's variables, which gives `psql` the right database:

```bash
a-novel run exec "$SERVICE/migrations" -- psql
```

To debug a target running as a host process, `run debug` prints the `dlv attach` command to run in your own terminal:

```bash
a-novel run debug "$SERVICE/$TARGET"
```

## Follow state from a script

`run watch` streams one JSON object per state change, such as a target turning healthy. React to it rather than polling `run ps`:

```bash
a-novel run watch --service="$SERVICE"
```

`a-novel run ps --json` and `a-novel run env --format=json` give the same data in a machine-readable form.
