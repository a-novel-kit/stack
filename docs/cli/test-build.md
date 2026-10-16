# Test and build

`a-novel test` and `a-novel build` find what to run in the directory you call them from, so they work the same in every repository. Run them from a repository to cover it, or from the stack root to cover the whole workspace. Both show a picker, run your selection in parallel, then print a report with the full output of every failure.

## Test

```bash
a-novel test
```

The picker lists every Go and JavaScript test suite it finds, all selected. Tests that need a database get a throwaway one, started before them and removed after, so several suites run side by side.

`-y` runs everything without the picker, `-t go` keeps one kind of target (`pnpm`, `podman`), and `--dry-run` lists the targets without running them. `a-novel test --help` describes the rest.

While iterating on one package, plain `go test ./<package>/...` stays the quickest loop. Tests that read a secret, such as an API key, get it from the CLI's [encrypted store](./secrets).

## Build

```bash
a-novel build
```

The picker lists everything it can build: Go binaries, JavaScript bundles and container images. Images build the way CI builds them.

The same `-y`, `-t` and `--dry-run` apply; `a-novel build --help` describes the rest.

## Parallelism

Behind the picker, several targets run at once; `-y`, which scripts and agents use, runs them one at a time. `-j` sets the limit in both modes:

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
