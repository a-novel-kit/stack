# Contributing to stack

Platform setup and day-to-day commands are in the [developer onboarding guide](https://github.com/a-novel-kit/.github/blob/master/README.md); read the [README](./README.md) first for what the CLI does.

This repo is the source of the `a-novel` CLI; [`cli/README.md`](./cli/README.md) is the deep reference. This file covers how to work on the repo.

## Layout

- `cli/` — the CLI's Go module (`github.com/a-novel-kit/stack/cli`). Commands live under `cli/internal/cli/` as Cobra subcommands.
- `app/`, `kit/` — checkouts of the repos the CLI operates on. Gitignored and populated by `a-novel core sync`; never commit inside them.

## Adding a command

A new command is a Cobra subcommand under `cli/internal/cli/` (a `*_cmd.go` file), wired into the right parent — `core` (lifecycle), `run` (daemon-backed verbs), or top-level (`test` / `build` / `publish`). Lint, format, and generate stay as `pnpm` scripts, not CLI verbs.

## Building and testing

After a change under `cli/`, run `a-novel install` to rebuild and reinstall — it hands the daemon off without dropping running services. Test with `a-novel test`, or `go test ./...` from `cli/`.

## Questions?

[Open an issue](https://github.com/a-novel-kit/stack/issues) — include logs and environment details.
