# Contributing to stack

For platform-wide setup (Go, Node, Podman, the `a-novel` CLI) and the day-to-day commands, see the [developer onboarding guide](https://github.com/a-novel-kit/.github/blob/master/README.md). Read the [README](./README.md) first for what the CLI does and how it is used.

This repo is the source of the `a-novel` CLI. The deep reference — the full command tree, the daemon architecture, state directories, and the compose contract — lives in [`cli/README.md`](./cli/README.md); this file covers how to work on the repo.

## Layout

- `cli/` — the CLI's Go module (`github.com/a-novel-kit/stack/cli`). Commands live under `cli/internal/cli/` as Cobra subcommands.
- `app/`, `kit/` — local checkouts of the repos the CLI operates on. Gitignored and populated by `a-novel core sync`; they are not part of this repo's history, so never commit inside them.

## Adding a command

New cross-repo tooling is a Cobra subcommand under `cli/internal/cli/` (a `*_cmd.go` file), wired into the right parent: `core` for daemon and workspace lifecycle, `run` for daemon-backed verbs, or the top level for the standalone `test` / `build` / `publish` verbs. Lint, format, and generate stay as `pnpm` scripts — they are deliberately not CLI verbs.

## Building and testing

After a change under `cli/`, run `a-novel install` to rebuild and reinstall the binary — it hands the daemon off without dropping running services. Run the tests with `a-novel test`, or `go test ./...` from inside `cli/`.

## Questions?

- Open an issue at https://github.com/a-novel-kit/stack/issues
- Check existing issues for similar problems
- Include relevant logs and environment details
