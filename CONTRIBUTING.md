# Contributing to stack

The platform taxonomy — repository kinds, libraries vs tooling, and where the `a-novel` CLI sits — lives in the [libraries, tooling & platform concepts](https://github.com/a-novel-kit/.github/blob/master/CONTRIBUTING.md). The service anatomy the CLI operates on (target, server / job, infra, the clean-arch layers) is defined in the [service & architecture concepts](https://github.com/a-novel/.github/blob/master/CONTRIBUTING.md) and only referenced here. Platform setup and day-to-day commands are in the [developer onboarding guide](https://github.com/a-novel-kit/.github/blob/master/README.md); read the [README](./README.md) first for what the CLI does.

This repo is the source of the `a-novel` CLI; [`cli/README.md`](./cli/README.md) is the deep reference. This file covers how to work on the repo.

## Glossary

The CLI's own vocabulary. Terms borrowed from service anatomy — **target**, **server**, **job**, **infra** — are defined once in the [service & architecture concepts](https://github.com/a-novel/.github/blob/master/CONTRIBUTING.md) and only summarized here.

| Term          | Meaning                                                                                                                                                                                                                                                    |
| ------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Stack**     | This repo _and_ a registered workspace checkout the daemon manages. Stacks coexist (`A_NOVEL_STACKS=name:path,…`); each carries its own `app/` + `kit/`.                                                                                                   |
| **Workspace** | The `app/` (services) + `kit/` (libraries) checkouts under a stack, cloned and fast-forwarded by `a-novel core sync`. Distinct from the stack that hosts them — the two were once conflated.                                                               |
| **Daemon**    | The per-user background supervisor: one per user, reached over a unix socket. It starts and supervises targets, allocates ports, streams logs, and owns volume operations.                                                                                 |
| **Target**    | A `cmd/<name>` binary in a service — the runnable unit the daemon starts. A **server** stays up and exposes an API; a **job** runs once and exits. (See [service anatomy](https://github.com/a-novel/.github/blob/master/CONTRIBUTING.md#runnable-units).) |
| **Infra**     | A backing compose service with no `cmd/` (e.g. Postgres) — a target's dependency, not a target itself. The daemon brings it up before the targets that need it.                                                                                            |
| **Mode**      | How a target runs: **go-exec** (`go run ./cmd/<target>`, the default) or **container** (`podman compose --profile <target>`).                                                                                                                              |
| **Phase**     | A target instance's lifecycle position, as the daemon tracks it: `pending → starting → running → stopping → terminated`.                                                                                                                                   |

## Layout

- `cli/` — the CLI's Go module (`github.com/a-novel-kit/stack/cli`). Commands live under `cli/internal/cli/` as Cobra subcommands.
- `app/`, `kit/` — checkouts of the repos the CLI operates on. Gitignored and populated by `a-novel core sync`; never commit inside them.

## Adding a command

A new command is a Cobra subcommand under `cli/internal/cli/` (a `*_cmd.go` file), wired into the right parent — `core` (lifecycle), `run` (daemon-backed verbs), or top-level (`test` / `build` / `publish`). Lint, format, and generate stay as `pnpm` scripts, not CLI verbs.

## Building and testing

After a change under `cli/`, run `a-novel install` to rebuild and reinstall — the new binary takes over from the running daemon in place, so running services stay up. Test with `a-novel test`, or `go test ./...` from `cli/`.

## Questions?

[Open an issue](https://github.com/a-novel-kit/stack/issues) — include logs and environment details.
