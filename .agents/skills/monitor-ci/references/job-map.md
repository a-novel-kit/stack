# Common service CI jobs

Read this reference when routed here by [monitor-ci](../SKILL.md). Its rules apply to the selected work.

## CI Job Map (typical Agora service repo)

The `main` workflow on Agora service repos under `a-novel/service-*` runs on every push to any
branch. The job set varies by repo; the table below is the common surface across those services.
For the jobs on the current checkout, run `gh pr checks <n>` (or read
`.github/workflows/main.yaml`) and intersect with the rows below; anything unlisted is
repo-specific or downstream of a base table entry.

| CI Job                        | What it checks                             | Local equivalent              | Typical failure                                                              |
| ----------------------------- | ------------------------------------------ | ----------------------------- | ---------------------------------------------------------------------------- |
| `generated-go`                | `go generate ./...` is up to date          | `pnpm generate:go`            | Forgot to run `pnpm generate:go` after proto/interface                       |
| `lint-go`                     | `golangci-lint run` clean                  | `pnpm lint:go`                | New Go code violates style or has a bug                                      |
| `lint-proto`                  | `buf lint` clean                           | `pnpm lint:proto`             | Proto file violates buf style                                                |
| `lint-node`                   | `pnpm lint:ci` clean                       | `pnpm lint:ci`                | JS/TS code violates eslint/prettier                                          |
| `test-go`                     | Go unit tests in `/internal`               | `a-novel test --type=go -y`   | Broken Go code or test                                                       |
| `test-pkg`                    | Go integration tests in `/pkg/go`          | `a-novel test --type=go -y`   | gRPC contract mismatch OR flake                                              |
| `test-pkg-js`                 | JS integration tests in `/pkg/js`          | `a-novel test --type=pnpm -y` | REST contract mismatch OR flake                                              |
| `build-database`              | Docker build for Postgres image            | `a-novel build --type=podman` | Dockerfile error, bad init script                                            |
| `build-migrations`            | Docker build for migrations job            | (none)                        | Migration file issue                                                         |
| `build-job-rotate-keys`       | Docker build for rotate-keys job           | (none)                        | Go build error in cmd/rotatekeys                                             |
| `build-grpc`                  | Docker build for gRPC service image        | (none)                        | Go build error                                                               |
| `build-standalone-grpc`       | Docker build for standalone gRPC dev image | (none)                        | Go build error                                                               |
| `build-rest`                  | Docker build for REST service image        | (none)                        | Go build error                                                               |
| `build-standalone-rest`       | Docker build for standalone REST dev image | (none)                        | Go build error                                                               |
| `build-js`                    | `pnpm build:rest` for pkg/js               | `pnpm -C pkg/js build:rest`   | TS compile error or broken export                                            |
| `report-grc` / `publish-docs` | Post-success reporting, **master only**    | (none)                        | Rarely actionable; usually transient                                         |
| `report-codecov`              | Coverage upload, runs on **every branch**  | (none)                        | Upload failure can still mark the run failed in PR checks; usually transient |

`test-go` blocks most application `build-*` jobs (`build-grpc`, `build-rest`,
`build-standalone-*`, `build-job-rotate-keys`); when it fails they are cancelled, so fix `test-go`
first. `build-database` does **not** depend on `test-go`, and `build-migrations` depends only on
`build-database`, so failures in those two surface independently and need their own diagnosis.

Check contexts are lane-suffixed (`test-go`, `lint-node`, …); `write-github-actions` owns that rule
and the reasons for it. A repo not yet migrated may still emit a bare `test`.
