# CI failure diagnosis

Read this reference when routed here by [monitor-ci](../SKILL.md). Its rules apply to the selected work.

Contents:

- [Phase 2: Classify](#phase-2-classify)
- [2.1 `generated-go` failure](#21-generated-go-failure)
- [2.2 `lint-go` / `lint-proto` / `lint-node` failure](#22-lint-go--lint-proto--lint-node-failure)
- [2.3 `test-go` (Go unit) failure](#23-test-go-go-unit-failure)
- [2.4 `test-pkg` / `test-pkg-js` failure](#24-test-pkg--test-pkg-js-failure)
- [2.5 `build-*` (Docker) failure](#25-build--docker-failure)
- [2.6 `build-js` failure](#26-build-js-failure)

## Phase 2: Classify

Map the failed-step log to one of these categories; the category determines the fix path.

### 2.1 `generated-go` failure

**Symptom**: job fails with a message like `go generate definitions are not up-to-date`.

**Root cause**: a `.proto` file or Go interface (used by a mock) changed without
`pnpm generate:go` being run afterward.

**Fix**: the original commit is already pushed and `git-conventions` forbids amending
pushed history, so the regenerated files land as their own follow-up:

```bash
pnpm generate:go
git status --porcelain
git add internal/handlers/protogen/ internal/handlers/mocks/ internal/core/mocks/
git commit -m "chore(gen): regenerate Go bindings for <scope>"
git push
```

That splits the proto/interface change and its regen across two commits, the cost of
noticing after push. The "generated files belong in the same commit" guidance in
`git-conventions` is a structure preference; the "never amend a pushed commit" rule is
categorical and wins here.

### 2.2 `lint-go` / `lint-proto` / `lint-node` failure

**Symptom**: linter reports specific files + line numbers.

**Fix**: run the matching local script, read its output, edit the flagged files, re-run
until clean, then commit:

```bash
pnpm lint:go        # or lint-proto / lint-node
# edit flagged files
pnpm lint:go        # re-run to confirm clean
git add <files>
git commit -m "fix(<scope>): resolve lint findings"
git push
```

Use a `fix(<scope>): resolve lint findings` commit for trivial mechanical changes and a
`refactor` or `fix` commit for invasive rewrites. `git-conventions` forbids amending pushed
commits unconditionally, so a noisy `fix(lint): ...` follow-up is the right call; under
squash-merge the PR author squashes or absorbs it at merge time.

#### When the findings do not reproduce locally

Before editing anything, confirm the finding is real. Two traps produce lint failures that exist
only on the runner, and "fixing" either one edits correct code or buries a `//nolint` in it.

**Reproduce with the binary the action installs, not the repo-pinned tool.**
`go tool -modfile=golangci-lint.mod golangci-lint` builds from the repo's own module graph; the
action downloads a released binary built against a different toolchain. Same version number, two
different builds. Take the version from the job log and run that:

```bash
gh run view <run-id> --repo <org>/<repo> --log | grep -i 'golangci-lint binary v'
curl -sSL https://github.com/golangci/golangci-lint/releases/download/vX.Y.Z/golangci-lint-X.Y.Z-linux-amd64.tar.gz | tar xz
./golangci-lint-X.Y.Z-linux-amd64/golangci-lint run --path-mode=abs   # from the module dir, as CI does
```

**Suspect the analyzer cache when the same key gives two verdicts.** The cache key
(`golangci-lint.cache-Linux-<mod>-<n>-<hash>`) derives from the Go version and the modfiles, never
from source, while results inside it are stored per package by content hash. A PR that edits a
package invalidates only that package, which is re-analyzed against stale inter-package facts left
in the cache — and stale facts are how staticcheck loses knowledge like "`(*testing.T).Fatalf` never
returns", turning every `if x == nil { t.Fatalf(...) }` into a bogus `SA5011`. `master` passes on the
identical key because its packages were unchanged and their verdicts were replayed, not recomputed.

The signature: findings on lines the PR never touched, in packages it merely brushed, that reproduce
on no local configuration — warm cache, cold `GOCACHE`/`GOLANGCI_LINT_CACHE`, released binary, same
Go version. Confirm by comparing cache keys, then evict:

```bash
gh run view <failing-run> --repo <org>/<repo> --log | grep 'Restored cache'
gh run view <last-green-master-run> --repo <org>/<repo> --log | grep 'Restored cache'  # same key?
gh cache list --repo <org>/<repo>          # find the id
gh cache delete <id> --repo <org>/<repo>   # ask the operator first — shared CI state
```

A re-run alone does not help: it restores the same cache. Caches regenerate on the next lint job, so
eviction is reversible, but it affects every run on the repo — surface it rather than doing it
silently. And note what this means for the green check on `master`: it is not evidence the package
still lints clean, only that nothing forced it to be re-examined.

### 2.3 `test-go` (Go unit) failure

**Symptom**: `--- FAIL: TestXxx` in the log, optionally a stack trace.

**Fix**:

1. Reproduce locally first — never fix blind:

   ```bash
   # Run just the failing package and test for fast iteration
   go test ./internal/<package>/... -run TestXxx -v
   # Or the full suite if multiple tests fail
   a-novel test --type=go -y
   ```

2. Decide from the failure: **is the test wrong, or is the code wrong?**
   - New test for behaviour the code doesn't yet implement → fix the code
   - Existing test that used to pass → fix the new code that broke it
   - Test assertion out of date vs. new intended behaviour → fix the test
   - Follow `write-go-service` / `write-go-tests` for the actual fix

3. Re-run until green locally, then commit. This skill always runs on an already-pushed
   branch, so `git-conventions`' "never amend a pushed commit" rule applies unconditionally:
   - A fix belonging with the feature: follow-up commit on the branch, collapsed by
     squash-merge at PR merge time (if configured).
   - A genuine separate fix: new `fix(<scope>)` commit.

4. Push and go back to Phase 1.

### 2.4 `test-pkg` / `test-pkg-js` failure

**Symptom**: integration test failure against a running gRPC or REST service.

**First check for flake**, since these jobs depend on cold image startup:

- `connection refused` / `dial tcp` / `EOF` / `context deadline exceeded` before any
  assertion → likely flake, service wasn't ready
- Sudden `502 Bad Gateway` or transport-level error → likely flake
- Timeout on first request only, subsequent requests pass locally → likely flake

For a suspected flake, retry the failed jobs only — do not rerun the whole workflow:

```bash
gh run rerun <run-id> --failed
```

If the same job fails twice with the same transport-level symptom, stop treating it as a
flake and investigate as real.

**If the failure is real** (assertion mismatch, wrong status code, unexpected field):

1. Reproduce locally — these suites need a running service:

   ```bash
   a-novel test --type=go -y       # starts gRPC standalone
   a-novel test --type=pnpm -y    # starts REST standalone
   ```

2. The failure usually means a contract mismatch between handler and client:
   - `test-pkg` failing → gRPC handler vs. `pkg/go` client drift — check `write-proto`
   - `test-pkg-js` failing → REST handler vs. `openapi.yaml` vs. `pkg/js/rest/` drift —
     all three must match, see `implement-feature`'s OpenAPI / REST / JS sync rule

3. Fix the out-of-date side, re-run until green, commit, push.

### 2.5 `build-*` (Docker) failure

**Symptom**: `docker build` step fails. Root causes:

- **Go compilation error** in the image's entrypoint binary → the real failure is in Go
  source; fix via Phase 2.3 approach (edit, `go build ./...`, commit). The `build-*`
  failure is a downstream symptom.
- **Dockerfile syntax / COPY path wrong** → follow `write-dockerfiles` to fix
- **Base image pull failure** → usually transient; retry with `gh run rerun --failed`
- **Migration init script failure** (`build-database`, `build-migrations`) → follow
  `write-sql` for migration fixes

Always read the `--log-failed` output before guessing. `undefined: Foo` or `type Bar has no
field Baz` is a Go source issue, not a Dockerfile one.

### 2.6 `build-js` failure

**Symptom**: `pnpm build:rest` fails in `pkg/js/`.

**Fix**: follow `write-js-package`. Reproduce with `pnpm -C pkg/js build:rest`. Typical
causes:

- TypeScript compile error after an API change
- Missing export in `pkg/js/rest/index.ts`
- Broken import path after a file rename
