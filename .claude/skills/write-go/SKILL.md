---
name: write-go
description: >
  Foundational Go conventions shared by EVERY Go repository in the a-novel and a-novel-kit
  organizations — naming, error handling, dependency policy, context rules, the format/lint
  discipline, time handling, and secrets hygiene. Load this skill for ANY Go work (writing,
  reviewing, refactoring) in either org: it is the base layer that `write-go-service` (clean-
  architecture backend services under the a-novel org) and `write-go-kit` (shared libraries
  under the a-novel-kit org — golib, jwt, …) build on, so load the matching repo-kind skill
  ALONGSIDE this one. Pairs with `write-go-tests` (test conventions) and `document-code` (doc
  comments). Does NOT cover JS/TS, SQL (`write-sql`), Protobuf (`write-proto`), Dockerfiles
  (`write-dockerfiles`), or shell scripts (`write-bash-scripts`).
---

# Go Conventions (common)

This is the base layer for Go in every a-novel / a-novel-kit repository. Rules here hold
**regardless of repo kind** — a backend service under the `a-novel` org, a shared library under
the `a-novel-kit` org (`golib`, `jwt`, …), or a one-off tool. Repo-kind-specific rules live in
two companion skills; load the one that matches where you are working **in addition to** this
one:

- **`write-go-service`** — clean-architecture services under `a-novel` (`app/service-*`,
  `app/platform-*`): the `cmd`/`internal/{config,lib,dao,services,handlers,models}`/`pkg` layout,
  the interface+implementation pattern, transactions, OpenTelemetry instrumentation, REST/gRPC
  handlers, and the layer-specific test patterns.
- **`write-go-kit`** — shared libraries under `a-novel-kit` (`kit/golib`, `kit/jwt`, …): the
  golib-stays-minimal philosophy, when something earns its own package, and the public/community
  API obligations that come with it.

When work spans repos and needs versions kept in sync to merge cleanly, also load
**`manage-versions`**.

**Before touching any file, read it.** Before touching a package, read its siblings, the
interfaces it depends on, and the interfaces it exposes. These codebases are deliberately
consistent — coherence with the surrounding code outranks personal preference every time. When
two existing files disagree, the newer one and the one closest to your change usually win; if it
is genuinely unclear, ask rather than guess.

**Look up the API before you use it.** Before writing or changing code that touches an external
package or a non-trivial stdlib API, check the official `pkg.go.dev` docs and the package's
README / release notes. APIs evolve and the first approach that comes to mind is often not the
idiomatic one. Seconds of reading prevents subtle misuse — do it before the code, not after.

---

## After every edit

Run, in this order, after any change to Go files:

```
pnpm generate:go   # ONLY when you changed an interface or a .proto file — regenerates mocks / proto stubs
pnpm format:go     # always
pnpm lint:go       # always
```

`pnpm format:go` / `pnpm lint:go` exist in every Go repo (they wrap `go mod tidy` and a pinned
`golangci-lint` invoked through `golangci-lint.mod`); never skip them. If `lint` flags something
you did not introduce, fix it anyway while you are in the file — leave it cleaner than you found
it.

Then, before the change is done:

1. Invoke **`write-go-tests`** — write or update tests for every file you created or modified, and
   run the narrowest test target that covers the change (`a-novel test --type=go -y`, or raw
   `go test ./<pkg>/...` for one package) until it is green. Tests are part of the change, not a
   follow-up.
2. Invoke **`document-code`** — doc comments for every symbol you added or changed. Also part of
   the change.

(Repo-kind skills add steps: `write-go-service` adds the `write-openapi` step when a REST
contract changes; `write-go-kit` adds the public-API and docs-site obligations for a graduated
package.)

---

## Dependencies

The standing goal across both orgs: **the fewest libraries doing the most.** Every dependency is
a maintenance liability, a supply-chain surface, and a constraint on future choices.

- **Stdlib first.** If `net/http`, `encoding/json`, `context`, `errors`, `fmt`, `time`,
  `crypto/*`, `slices`, `maps`, `cmp`, etc. cover the need, use them. Never pull a package for
  what the standard library already does.
- **Reuse before adding.** Prefer a package already in `go.mod`. A second library that overlaps
  one already present is almost never worth it.
- **Vet candidates.** A new dependency must be: well-maintained (recent commits, responsive
  issue tracker, real test coverage); **owned by an organization, not a single personal
  account** — org ownership survives a maintainer losing interest; and **broad** — pick the
  library that covers the most of the surrounding problem space, so one dependency replaces
  three. A narrow utility from an individual's account is the worst combination.
- **Ask first — always.** Introducing a package not already in `go.mod` requires explicit
  developer approval. No exceptions, not even a small utility. (`a-novel-kit` repos hold this bar
  even higher — see `write-go-kit`; `a-novel` services keep their `internal/lib/` as close to
  empty as possible.)
- **Remove, don't accumulate.** During maintenance, actively look for dependencies — and
  hand-rolled helpers — that a newer upstream now subsumes, and delete the duplicate.

---

## Package naming

One directory = one package. Package names are **short, lowercase, single words** — no
underscores, no camelCase: `package dao`, `package jwk`, `package httpf`. The name is what every
caller types in imports; keep it unambiguous, collision-free, and non-stuttering with the types
inside it (`jwk.Key`, not `jwk.JwkKey`).

---

## File naming

Multi-word file names are **camelCase** — never snake_case (`master_key.go` ✗), never
run-together when the name is two words (`masterkey.go` ✗): `masterKeyContext.go`,
`userSearch.go`. A test file mirrors the production file it covers with a `_test.go` suffix
(`userSearch.go` → `userSearch_test.go`) — see `write-go-tests` for why the underscore (not a
dot) matters and how to share fixtures across packages. The repo-kind skills define the
layer/role prefixes (`pg.*`, `rest.*`, `grpc.*`, `*.config.go`, `common.go`, …).

---

## Naming conventions

### Variables and fields

- **Be explicit.** `userRepository`, not `repo`. `orderCreateService`, not `svc`. Length is not
  the cost; ambiguity is.
- **Short names are fine for conventional roles only**: `w` / `r` for HTTP handler params, `ctx`
  for `context.Context`, `err` for errors, `i` / `k` / `v` in range loops, `t` for `*testing.T`.
- **Acronyms keep ecosystem casing**: `ID`, `URL`, `JSON`, `JWT`, `JWK`, `HTTP`, `REST`, `gRPC`,
  `SQL`, `TTL`. An acronym that _starts_ an unexported identifier goes all-lowercase: `id`,
  `url`, `grpc`.
- **Don't shadow imported package names** (`json`, `http`, `context`, `errors`, `time`, …).
  Rename the variable.

### Constructors

Every type with a constructor uses `New<TypeName>(deps...) *<TypeName>`. Return the concrete
pointer, not an interface — unless the concrete type is genuinely an implementation detail hidden
by design (e.g. a `pkg/go` client behind an interface).

**Always the composite-literal form**, never `new(T)`:

```go
// WRONG — `new(T)` is a second style for the same thing; mixing the two is noise.
func NewPgUserSelect() *PgUserSelect { return new(PgUserSelect) }

// CORRECT — reads identically whether the struct is empty or has fields.
func NewPgUserSelect() *PgUserSelect              { return &PgUserSelect{} }
func NewUserSearch(r UserSearchRepository) *UserSearch { return &UserSearch{repository: r} }
```

Treat any `new(T)` you find as cleanup-on-sight when the file is already in scope.

---

## Error handling

- **Lowercase, no trailing punctuation.** Errors get wrapped and read mid-sentence:
  `errors.New("user not found")`, not `"User not found."`.
- **Sentinels for expected outcomes**: `var ErrUserNotFound = errors.New("user not found")`,
  defined in the same file as the type that produces it (or a shared file if reused across the
  package). Map an upstream/library error onto your own sentinel by _joining_ it — `err =
errors.Join(err, ErrUserNotFound)` — so callers keep both identities.
- **One vocabulary per concept.** If the type is `Jwk`, every error message that refers to it
  says `"jwk …"` — never `"key not found"` in one file and `"jwk not found"` in another. Drift in
  error wording is one of the easiest sources of low-level entropy and one of the hardest to
  dashboard around. Pick the term used in the type name and propagate it everywhere the error
  surfaces.
- **Wrap with context and `%w`**: `fmt.Errorf("search users: %w", err)`. Always `%w`, never
  `%v`, so callers keep `errors.Is` / `errors.As` identity. The message chain should let a reader
  trace the call path on its own.
- **Never silently discard an error.** If one truly can be dropped, write `_ = ...` with a
  comment saying why.

### Reporting errors on spans / telemetry — the layer-relative rule

When a function instruments itself with a span (or any other per-operation telemetry), reporting
follows one rule: **every layer that has a span records, on its own span, every error it sees —
whether it raises it, propagates it, or maps it to a transport response.** The forbidden moves
are: _suppressing_ reporting based on the error's _identity_ at a propagating layer, and `return
nil, ErrXxx` bare from a layer that has a span. "Expected" is never a property the error value
carries, and never something one layer guesses on a caller's behalf.

- A layer that _raises_ an error (a DAO hitting `sql.ErrNoRows`, a validator producing
  `ErrInvalidRequest`, a service detecting a mismatch) → `otel.ReportError(span, err)`.
- A layer that _receives_ an error and returns it upward → still `otel.ReportError`. Returning
  upward is _propagating_; wrapping it (`errors.Join`, `fmt.Errorf("...: %w", err)`) changes
  nothing.
- The handler that maps the error to a transport response → still reports. `golib/httpf.HandleError`
  calls `otel.ReportError` unconditionally before writing the HTTP status, so the REST handler span
  records the error whatever status it maps to; the gRPC manual mapping should do the same by hand
  (`_ = otel.ReportError(span, err)` before `status.Error(...)`). The handler span shows which
  error a request ended on — that is the point of having it.
- **Anti-pattern**: a helper that suppresses reporting based on the error's _identity_ at a layer
  that still propagates or surfaces it (a `reportUnexpected(span, err)` keyed on a list of "known"
  sentinels). It couples the layer to an error registry and silently drops real signal. The
  layer-local question is just "did I see this error?" — if yes, report it.

Spans are independent — a child span ending `Error` does not taint the parent. So the DAO span,
the service span, _and_ the handler span all say "no row" for a 404, while the "is the service
broken" view is built on the **HTTP status code** (recorded by the otel HTTP instrumentation) and
counts the 404 as a 404, not a 500 — span status answers "did an error occur in processing", which
is `true` even for a deliberate 404, and that is fine. For bulk-anomaly visibility on a specific
security sentinel, use a counter / audit log / dedicated event — not `span.status`. The helpers
`otel.ReportError` / `otel.ReportSuccess` / `otel.ReportSuccessNoContent` live in `golib/otel`;
`ReportError` only sets `RecordError` + `SetStatus(Error)` — it does **not** end the span (a
`defer span.End()` does), and returning a sentinel with no `Report*` call leaves the span `Unset`,
which backends treat as "completed, not a failure". `write-go-service` covers span naming and the
span-per-operation rule.

---

## Context rules

- **Never store `context.Context` in a struct.** It is request-scoped and must not outlive the
  call. Pass it as the first parameter of every method that needs it.
- **Contexts flow downward only** — caller to callee. Never return one.
- **Context values are for request-scoped data that crosses API boundaries** — DB transactions,
  trace spans, auth tokens. They are not a back door for optional arguments.

---

## Time capture

When an operation derives more than one timestamp from "now" — `created_at` + `expires_at`,
`start` + `deadline`, paired audit fields — capture `now := time.Now()` **once** at the top and
reuse it:

```go
// WRONG — two wall-clock reads; expires_at - created_at is not exactly the TTL, and a test can't freeze it.
repo.Exec(ctx, &dao.InsertRequest{Now: time.Now(), Expiration: time.Now().Add(cfg.TTL)})

// CORRECT — one logical instant.
now := time.Now()
repo.Exec(ctx, &dao.InsertRequest{Now: now, Expiration: now.Add(cfg.TTL)})
```

Only call `time.Now()` more than once when you genuinely want distinct measurements (e.g.
computing an elapsed duration).

---

## Secrets and sensitive data

- **Never log, trace, or put in span attributes**: passwords, tokens, API keys, private keys,
  key ciphertexts, signed JWTs, or any other credential material. Record identifiers only
  (`user.id`, `key.id`), never the secret itself. A redacted `"*****"` of the same length still
  leaks the input length over every trace — don't do that either.
- **Never return secret material in an error message or a transport response.** Errors get
  logged; responses get cached and indexed.
- **Compare secrets in constant time** — `crypto/subtle.ConstantTimeCompare`, never `==` or
  `bytes.Equal` — and on the "not found" branch of an authentication path do the equivalent work
  (e.g. a throwaway hash comparison) so a lookup miss costs the same as a wrong-secret outcome
  and timing does not reveal whether the subject exists.

---

## Loop variable scope

These codebases target Go 1.22+, where `for` loop variables are per-iteration; a closure captures
the right value with no extra copy. Remove any `current := item` shadow copies inside `for range`
loops when you encounter them — they are dead code on this minimum version.

---

## Common pitfalls

- **`context.Context` stored in a struct.** Always a method parameter.
- **A new dependency added without asking.** Explicit developer approval, every time.
- **Logging / tracing secret material.** Identifiers only — never the secret.
- **Multiple `time.Now()` for timestamps that should share one instant.** Capture once, reuse.
- **Suppressing span reporting for an error.** Every span'd layer reports every error it sees —
  raised, propagated, or mapped to a transport status. No identity-keyed `reportUnexpected`
  helpers; no bare `return nil, ErrXxx` from a layer that has a span.
- **`new(T)` in a constructor.** Use `&T{}`.
- **snake_case or run-together multi-word file names.** camelCase.
- **Discarding an error without `_ =` and a why-comment.**
- **Shadowing an imported package name with a local variable.** Rename the variable.
