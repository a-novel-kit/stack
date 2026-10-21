# Test and build

`a-novel test` and `a-novel build` cover the directory you run them in: one repository, or the whole workspace from the stack root. Pick what to run; failures print in full at the end.

## Test

```bash
a-novel test
```

Tests that need a database get a throwaway one. `-y` skips the picker, and `--help` lists the filters. For a single package, plain `go test` is quicker.

## Build

```bash
a-novel build
```

`a-novel build` builds the Go binaries, pnpm bundles and container images it finds.

## Parallelism

With the picker, several targets run at once. With `-y`, one runs at a time. `-j` sets the limit:

```bash
a-novel test -y -j 4
```

## Lint, format and generate

Most repositories expose these as pnpm scripts. A repository's `CONTRIBUTING.md` names any check outside them.

```bash
pnpm lint            # every linter of the repository
pnpm format          # rewrite files to the formatting rules
pnpm generate:go     # Go repositories: regenerate mocks and protobuf code
```

`pnpm run` lists the rest.

## Before you push

From the repository you changed, run the checks its CI runs. Lint checks formatting, so format first:

```bash
pnpm format
a-novel test -y
pnpm lint
a-novel build -y -t podman   # when the change touches an image
```
