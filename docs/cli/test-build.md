# Test and build

`a-novel test` and `a-novel build` find what to run in the directory you call them from, so they work the same in every repository. Run them from a repository to cover it, or from the stack root to cover the whole workspace. Both show a picker, run your selection in parallel, then print a report with the full output of every failure.

## Test

```bash
a-novel test
```

The picker lists a target per Go module and per pnpm `test` or `test:*` script, all selected. Tests that need a database get a throwaway one, started before them and removed after, so several suites run side by side.

| Command                   | Runs                                                       |
| ------------------------- | ---------------------------------------------------------- |
| `a-novel test -y`         | Everything, without the picker, one target at a time       |
| `a-novel test -t go`      | Go targets only; `-t pnpm` for JavaScript                  |
| `a-novel test --dry-run`  | Nothing: lists the targets and their environments          |
| `a-novel test --keep`     | As usual, leaving the environment up for a faster next run |
| `a-novel test --no-cover` | Without coverage, which is on by default                   |

While iterating on one package, plain `go test ./<package>/...` stays the quickest loop. Tests that read a secret, such as an API key, get it from the CLI's [encrypted store](./secrets).

## Build

```bash
a-novel build
```

The picker lists a target per Go module, per pnpm `build` or `build:*` script, and per Dockerfile: the root `Dockerfile` and each `builds/*.Dockerfile`. Images build the way CI builds them.

| Command                   | Builds                                                       |
| ------------------------- | ------------------------------------------------------------ |
| `a-novel build -y`        | Everything, without the picker, one target at a time         |
| `a-novel build -t podman` | Images only; `-t go`, `-t pnpm`, or a list like `-t go,pnpm` |
| `a-novel build --dry-run` | Nothing: lists the targets                                   |

## Parallelism

Behind the picker, a quarter of your CPUs' worth of targets run at once. `-y`, which scripts and agents use, runs one at a time; `-j` sets the limit in both modes:

```bash
a-novel test -y -j 4
```

## Lint, format and generate

These are not CLI commands: each repository exposes them as pnpm scripts with the same names everywhere.

```bash
pnpm lint            # every linter of the repository
pnpm format          # rewrite files to the formatting rules
pnpm generate:go     # Go repositories: regenerate mocks and protobuf code
```

Each splits into narrower scripts, such as `pnpm lint:go`; `pnpm run` lists a repository's scripts.
