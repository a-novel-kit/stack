# Common service CI jobs

Read this reference when routed here by [monitor-ci](../SKILL.md). Its rules apply to the selected work.

## CI Job Map (typical A-Novel service repo)

The `main` workflow on A-Novel service repos under `a-novel/service-*` runs on pull requests, merge
groups and pushes to `master`. A label change on a pull request runs it again. A branch push without
a pull request runs nothing. The job set varies by repo; the table below is the common surface
across those services. For the jobs on the current checkout, run `gh pr checks <n>` (or read
`.github/workflows/main.yaml`) and intersect with the rows below; anything unlisted is
repo-specific or downstream of a base table entry.

| CI Job                       | What it checks                                                | Local equivalent                        | Typical failure                                                              |
| ---------------------------- | ------------------------------------------------------------- | --------------------------------------- | ---------------------------------------------------------------------------- |
| `check-migration-snapshots`  | Migrations and schema snapshots are append-only, **PRs only** | (none)                                  | Edited an existing migration or schema snapshot                              |
| `generated-go`               | `go generate ./...` is up to date                             | `pnpm generate:go`                      | Forgot to run `pnpm generate:go` after proto/interface                       |
| `lint-go`                    | `golangci-lint run` clean                                     | `pnpm lint:go`                          | New Go code violates style or has a bug                                      |
| `lint-proto`                 | `buf lint` clean                                              | `pnpm lint:proto`                       | Proto file violates buf style                                                |
| `lint-js`                    | `pnpm lint:ci` clean                                          | `pnpm lint:ci`                          | JS/TS code violates eslint/prettier                                          |
| `lint-shell`                 | `shellcheck` on scripts and workflow `run:` steps             | (none)                                  | Shell script or `run:` step violates shellcheck                              |
| `lint-dockerfile`            | `hadolint` on Dockerfiles                                     | (none)                                  | Dockerfile or `RUN` block violates hadolint                                  |
| `lint-semgrep`               | `semgrep` service ruleset clean                               | podman semgrep, see `open-pull-request` | Code breaks a layout convention golangci-lint cannot express                 |
| `scan-secrets`               | `gitleaks` finds no credential in the tree                    | (none)                                  | Committed credential or a value gitleaks mistakes for one                    |
| `lint-workflows`             | `zizmor` on workflows and composite actions                   | (none)                                  | Excessive permissions, credential leak, or floating ref                      |
| `test-go`                    | Go unit tests in `/internal` and `/cmd`                       | `a-novel test --type=go -y`             | Broken Go code or test                                                       |
| `test-pkg`                   | Go integration tests in `/pkg/go`                             | `a-novel test --type=go -y`             | gRPC contract mismatch OR flake                                              |
| `test-pkg-js`                | JS integration tests in `/pkg/js`                             | `a-novel test --type=pnpm -y`           | REST contract mismatch OR flake                                              |
| `build-database`             | Docker build for Postgres image                               | `a-novel build --type=podman`           | Dockerfile error, bad init script                                            |
| `test-reproducible-database` | Postgres image rebuilds to the same digest                    | (none)                                  | Nondeterministic step in `builds/database.Dockerfile`                        |
| `build-migrations`           | Docker build for migrations job                               | (none)                                  | Migration file issue                                                         |
| `build-rotate-keys`          | Docker build for rotate-keys job                              | (none)                                  | Go build error in cmd/rotate-keys                                            |
| `build-grpc`                 | Docker build for gRPC service image                           | (none)                                  | Go build error                                                               |
| `build-standalone-grpc`      | Docker build for standalone gRPC dev image                    | (none)                                  | Go build error                                                               |
| `build-rest`                 | Docker build for REST service image                           | (none)                                  | Go build error                                                               |
| `build-standalone-rest`      | Docker build for standalone REST dev image                    | (none)                                  | Go build error                                                               |
| `build-js`                   | `pnpm build:rest` for pkg/js                                  | `pnpm -C pkg/js build:rest`             | TS compile error or broken export                                            |
| `publish-docs`               | API docs deploy to Pages, **master only**                     | (none)                                  | Rarely actionable; usually transient                                         |
| `report-codecov`             | Coverage upload, runs on **every event**                      | (none)                                  | Upload failure can still mark the run failed in PR checks; usually transient |

`build-database` feeds `generated-go`, `test-go`, the `test-pkg*` jobs, `build-migrations`, and the
application `build-*` images; when it fails they are all skipped, so fix it first. A skipped job has no result
of its own. Platforms skip their tests and `build-platform` the same way when `build-js` or
`lint-types` fails.

Check contexts are lane-suffixed (`test-go`, `lint-js`, …); `write-github-actions` owns that rule
and the reasons for it.
