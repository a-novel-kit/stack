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

It builds binaries, bundles and container images the way CI does.

## Parallelism

The picker runs several targets at once, `-y` one at a time; `-j` sets the limit:

```bash
a-novel test -y -j 4
```

## Lint, format and generate

These are pnpm scripts, named the same in every repository:

```bash
pnpm lint            # every linter of the repository
pnpm format          # rewrite files to the formatting rules
pnpm generate:go     # Go repositories: regenerate mocks and protobuf code
```

`pnpm run` lists the rest.
