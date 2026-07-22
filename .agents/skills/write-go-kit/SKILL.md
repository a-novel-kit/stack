---
name: write-go-kit
description: >
  Go conventions for the a-novel-kit shared-library repos under `kit/` (`golib`, `jwt`, …) — the
  golib-stays-minimal bar, the stricter kit dependency policy, graduating a sub-package into its
  own community package, and kit test conventions. Load it to write or review Go there, or to
  decide whether something belongs in `golib` at all. ALWAYS load `write-go` alongside it; a-novel
  services use `write-go-service`. Pairs with `write-go-tests`, `document-code`,
  `manage-versions`. Not JS/TS, `kit/nodelib`, or `kit/workflows`.
---

# Go — a-novel-kit shared libraries

This skill governs Go in the `a-novel-kit` organization: the shared-library repos checked out
under `kit/` (`golib`, `jwt`, and any future graduate). These are **not** services — there is no
`dao`/`services`/`handlers` layering here. Load `write-go` (the base Go conventions) and
`write-go-tests` (and `document-code` when documenting) **alongside** this skill. For a-novel
backend services use `write-go-service` instead.

**Before touching anything**, read the repo's existing code and its README/CONTRIBUTING — kit
repos are small, cohesive, and consistent; copy the established pattern. When the change is needed
to support a service (or vice versa), load **`manage-versions`** as well: kit changes and the
service changes that consume them must be released in the right order.

---

## Why kit exists, and the standing tension

Shared libraries exist so services can be written **faster, with more focus** — by removing
boilerplate and duplicated glue, not by adding capability. Every line that lands in a kit repo is
maintained _forever_ as a community-facing package. So the bar for adding is high, and the default
for "should this go in kit?" is **no** until proven otherwise.

There are two kinds of kit repo:

- **`golib`** — the _tote_ package: a single module (`github.com/a-novel-kit/golib`) whose
  sub-packages each collect the boilerplate for one concern (`config`, `otel`, `httpf`, `grpcf`,
  `logging`, `postgres`, `smtp`). Glue lives here until — if ever — a chunk grows enough to
  graduate.
- **Graduated packages** — `jwt` is the precedent: when a `golib` chunk gets large, has cohesive
  scope, and has value beyond a-novel's internal needs (no suitable Go JWT library existed, so one
  was built), it moves to its own `a-novel-kit/<name>` repo and takes on the full set of
  community-package obligations below.

---

## golib — keep it as small as possible

**Rule #1: `golib` stays minimal. The ideal end state is that `golib` disappears entirely** and
every service depends only on external libraries. We are not there because duplicated boilerplate
is real, and a convenient place to put it is worth a small dependency. But every addition is a step
away from rule #1, so each one is a deliberate trade.

- **Never imitate the capabilities of an existing dependency.** If a well-maintained library does
  the job — even if it does _more_ than we need, even if its ergonomics aren't perfect — use the
  library, directly, even from a service. Do not wrap it in a `golib` sub-package "for
  convenience": a thin wrapper around a good dependency is a maintenance burden that adds nothing.
- **Don't reimplement the standard library.** `golib` is glue, not a re-export of stdlib.

### The "should this go in golib?" decision

A balance, not a checklist. Add a thing to `golib` only when **all** of these hold; if any is
shaky, the answer is no:

1. It is **pure boilerplate / glue** — no business logic, nothing a-novel-specific in its core; it
   does the same mechanical thing regardless of who calls it. (Examples already in `golib`: the
   `otel.Report*` span helpers, `postgres.GetContext`/`RunInTx`/migration plumbing, the `logging`
   presets, the `httpf` error-mapping + JSON helpers, the `config.LoadEnv` parsers.)
2. **Several services would otherwise copy it verbatim.** One service needing it is not enough — a
   one-off belongs inline in that service (or, reluctantly, its near-empty `internal/lib/`). Two
   services with the same copy-paste is the bar.
3. **No dependency already covers it.** If `samber/lo`, `uptrace/bun`, an `otel` contrib package,
   or anything else in (or addable to) `go.mod` does it, use that instead.
4. **You can't make it disappear instead.** Sometimes the right move is to delete the duplicated
   code from all the services and not have it anywhere — if the "boilerplate" is two lines, inline
   beats a `golib` symbol.

When in genuine doubt: **don't add it.** Adding a symbol is cheap; removing one that other repos
import is a coordinated cross-repo release dance (deprecate → migrate every consumer → remove —
see `manage-versions`). That asymmetry is why the bar is high.

### Removing from golib

Maintenance includes shrinking. When a newer upstream subsumes a `golib` helper, **delete the
helper** — staged: deprecate it (a doc comment + a `// Deprecated:` line pointing at the
replacement), drive the consumers to migrate, then remove it in a later release. (See
`manage-versions` for the backward-compatible-staging rule; it applies to every public symbol you
retire.)

---

## Dependencies in kit repos (stricter than the base rule)

`write-go`'s dependency policy applies, with the bar raised:

- The kit exists to consolidate dependencies so services have fewer to vet, so adding a **new**
  dependency to a kit repo — especially to `golib` — works against the goal. Do it only when the
  alternative is hand-rolling something substantial that the new library does well and broadly.
- A kit dependency must be **organization-backed, never tied to a single personal account** —
  org ownership survives a maintainer losing interest, and a public package can't afford a
  surprise-abandoned dep. Well-maintained, broad coverage, active issue tracker: non-negotiable.
- Adding a dependency anywhere needs explicit developer approval (`write-go`). In a kit repo,
  expect the answer to lean "no" — propose it with the alternatives spelled out.

---

## golib structure

- One module, sub-packages by **concern**. A sub-package is an independent unit — keep
  cross-coupling between `golib` sub-packages to the minimum the concern genuinely requires.
- Within a sub-package, separate the **ready-made values** from the **mechanics**: `presets/`
  holds the pre-assembled configs (e.g. `otel/presets`, `postgres/presets`, `logging/presets`),
  `utils/` (or top-level helper files) holds the functions. Keep the top-level package surface of
  a sub-package small and stable.
- `golib` ships a small amount of generated code (the `grpcf` proto bits) — `buf` is in the
  toolchain (`buf.yaml`, `buf.gen.yaml`); `pnpm generate:go` runs `go generate` and `buf generate`,
  `pnpm lint:proto` runs `buf lint`, `pnpm format:proto` runs `buf format`/`buf dep update`. Treat `.proto` in
  a kit repo with the **`write-proto`** skill (the contract-stability rules there apply doubly to a
  shared library).
- Doc comments on every exported symbol (`document-code`) — `golib` is consumed by every service,
  so its godoc _is_ its documentation.

---

## Graduating a sub-package into its own repo

When a `golib` sub-package grows large, has a cohesive scope, and has value beyond a-novel's
internal needs, it earns its own `a-novel-kit/<name>` repo (the JWT precedent). **This is the
developer's call** — when you see a candidate, flag it; do not move it unprompted.

Once graduated, the package is a **public, community-facing library**, and the rules change:

- **Design the API broadly, not for our needs.** A graduated package covers its _domain_, not just
  the slice a-novel happens to use: `jwt` implements JWA / JWE / JWK / JWS / JWP, not only the two
  token flows the auth service needs. Design the exported surface as if strangers will build on it.
- **A real documentation site, not just godoc.** `jwt` has a `docs/` directory published to GitHub
  Pages. The README carries badges (Go version, CI status, codecov, Go Report Card), a `go get`
  line, and a link to the docs site.
- **Thorough tests + coverage gates.** A `codecov.yml`, high coverage, and `Example_xxx` /
  `ExampleType_method` functions for the public API — those render on pkg.go.dev _and_ run as
  tests, so they keep the docs honest.
- **The `a-novel-kit/.github` org defaults.** `LICENSE`, `SECURITY.md`, `CODE_OF_CONDUCT.md` (the
  Contributor Covenant, verbatim — don't author one), and a `CONTRIBUTING.md` that links the org
  concepts doc at `a-novel-kit/.github/blob/master/CONTRIBUTING.md` rather than restating it.
- **Semver discipline.** Public API stability is why the package graduated into its own versioned
  repo. Deprecate-then-remove; never break the API without a major bump; and even a major bump
  stages the break (add the new path → migrate consumers → remove the old path in a later
  release). See `manage-versions`.

A change to a graduated package that any a-novel service consumes is a cross-repo change — load
`manage-versions` and order the merges (library PR merges + releases first; the service re-pins to
the released tag before merging).

---

## a-novel-kit repo conventions

Match the repo's existing setup; the standard shape:

- **pnpm scripts** — match the repo's existing set (no Makefiles anywhere):
  - `a-novel test -y` — gotestsum via `gotestsum.mod` (the CLI discovers it).
  - `pnpm lint:go` — `golangci-lint` via `golangci-lint.mod`; `pnpm lint:proto` — `buf lint`
    (when there's proto); `pnpm lint` — the prettier/docs tooling.
  - `pnpm generate:go` — `go generate` (+ `buf generate` if proto).
  - `pnpm format:go` — `go mod tidy` + `golangci-lint --fix`; `pnpm format:proto` —
    `buf format` + `buf dep update`; `pnpm format` — prettier.

  `write-go` already says: run `pnpm format:go` and `pnpm lint:go` after every edit;
  `pnpm generate:go` when generated inputs changed.

- **Tool dependencies are pinned in `*.mod` side files** — `golangci-lint.mod`, `gotestsum.mod` —
  kept out of the main `go.mod`, invoked via `go tool -modfile=<name>.mod <tool>`. Don't move tool
  deps into `go.mod`.
- **`renovate.json`** keeps dependencies (and the consumers, in service repos) up to date; a
  `pnpm` workspace + `prettier.config.js` cover the docs/markdown tooling.
- **CI is the reusable workflows from `a-novel-kit/workflows`** (`go-actions/lint-go`,
  `go-actions/test-go`, the publish/release actions, etc.). A tag push triggers
  `publish-actions/auto-release` — i.e. **versions are git tags** (`vX.Y.Z`), and the next tag is
  computed from the conventional-commit history on `master`. See `manage-versions`.

---

## Tests in kit repos

`write-go-tests` covers the common shape (table-driven, `t.Parallel()`, `require`, the `_test`
package, helpers, mockery where interfaces exist). The kit-flavoured additions:

- **Coverage is gated** (codecov) — and for a graduated package, expected to be high. Cover the
  public API exhaustively: every exported function, every documented error path, edge cases and
  boundaries. A public library that 80%-covers its own surface is not done.
- **Write `Example_xxx` / `ExampleType_method` functions for the public API.** They double as the
  reference documentation on pkg.go.dev _and_ run as tests, so they can't silently rot. A
  graduated package should have an example for each major entry point.
- **A single shared-fixture sub-package may use a plain name** (`testutils/` — `jwt` has one). The
  layer-prefixed-name rule in `write-go-tests` (`configtest`, `libtest`, …) keeps _several_ fixture
  packages distinct in one repo; a small library with one is fine.
- **No service-DB harness.** Kit libraries have no `dao` layer, so there is no
  `postgres.RunIsolatedTransactionalTest` to use, and most kit tests are pure unit tests. Where a
  sub-package wraps an external system (`postgres`, `smtp`), its tests may stand a real instance up
  via the repo's `scripts/test.sh` — follow whatever the repo already does.

---

## Common pitfalls

(`write-go` lists the language-level ones. These are the kit-specific ones:)

- **Adding a thin wrapper around a good dependency to `golib`.** Use the dependency directly, even
  from a service.
- **Putting a one-off in `golib` because it "might be reused".** Two real consumers, or it stays
  out.
- **Reimplementing stdlib or an existing dep in `golib`.** Rule #1: `golib` shrinks.
- **Removing a public `golib`/graduated-package symbol in one shot.** Deprecate → migrate consumers
  → remove later, ordered per `manage-versions`.
- **Designing a graduated package's API around a-novel's needs only.** Cover the domain.
- **Authoring a bespoke `CODE_OF_CONDUCT.md` / restating the org contributing guide.** Use the
  `a-novel-kit/.github` defaults; the per-repo `CONTRIBUTING.md` only _extends_ the org-wide one.
- **Moving tool dependencies into `go.mod`.** They live in `golangci-lint.mod` / `gotestsum.mod`.
- **Graduating a sub-package without the developer's say-so.** Flag the candidate; let the
  developer decide.
- **Treating a kit change as self-contained when a service needs it.** It's a cross-repo change —
  `manage-versions`: the library merges and releases first, the service re-pins to the released
  tag before merging.
