---
name: write-go-tests
description: >
  Write or modify Go tests in services or shared libraries. Own table-driven cases, generated
  mocks, assertions, fixtures, parallelism, and coverage; pair with write-go and the repo-kind
  skill.
---

# Go Test Conventions

This skill governs Go tests across every a-novel / a-novel-kit repository, services and shared
libraries alike. Tests define behavior, document contracts, and guard against regressions; they
must be clear, isolated, and exhaustive for the paths they cover. Load it alongside `write-go`
(base Go conventions) and the repo-kind skill, `write-go-service` or `write-go-kit`.

**Before writing any test**, read the existing tests in the same package. Patterns are consistent
by design — follow them exactly. Read the production code under test too; do not guess at behavior
or signatures.

**Look up the testing libraries online.** Check the official docs and real usage of `testify`,
`httptest`, `mockery`, or any other helper before writing — above all for mock assertion patterns
(`EXPECT`, `.Once()`, `mock.MatchedBy`) and JSON comparison utilities. Misuse yields silent
false-positives and missed failures.

**Never remove existing tests** unless the feature they cover is fully deprecated and removed from
the codebase. Fix a stale or failing test; do not delete it.

---

## Load the test pattern you need

Before writing or modifying a test body, read [table-driven tests and mocks](references/patterns.md).
When adding, moving, or sharing test data or setup, also read [fixtures](references/fixtures.md).
For layer-specific behavior load `write-go-service` or `write-go-kit` as appropriate.

Pick the closest truthful layer that exposes the changed behavior. Reuse established cases and
fixtures; do not create a second assertion of the same fact at every layer. Preserve coverage of
different contracts and failure paths. The selected references are conventions, not optional examples.

## Timing and validation

Load [develop-feature](../develop-feature/SKILL.md). Write only essential tests during drafting and
issue review; expand to the full relevant regression suite after scope approval. Use the coverage
guidance below to assess meaningful gaps, without a 100% target or a test quota per file.

Run the narrowest test target that exercises the code you changed:

```
a-novel test --type=go -y   # auto-discovers Go tests: services' internal/ + pkg/go, libraries' packages
a-novel test -y             # add pnpm too (services with pkg/js)

# Iterating on a single package — raw go test stays valid for the tight loop:
go test ./internal/dao/... -run TestJwkSelect
```

During incremental work, scope with `--type=go` (or raw `go test ./<pkg>/...` for one package);
reserve the full `a-novel test -y` for final pre-commit validation. CI does not use the CLI — its
composite actions invoke `gotestsum` directly.

---

## Test File Naming

Test files take the name of the production file they cover, plus a `_test.go` suffix:

| Production file           | Test file                  |
| ------------------------- | -------------------------- |
| `pg.userSelect.go`        | `pg.userSelect_test.go`    |
| `rest.userList.go`        | `rest.userList_test.go`    |
| `grpc.orderCreate.go`     | `grpc.orderCreate_test.go` |
| `userSearch.go` (service) | `userSearch_test.go`       |

**Underscore, not dot.** The Go toolchain excludes only files ending in `_test.go` (with an
underscore) from production builds. A file named `something.test.go` (with a dot) is **compiled
into the production binary** — `.test.` is text in the filename, not a build-tag signal. Such a
file carrying test-only globals has leaked into the shipped binary and must be moved (see
"Cross-package test fixtures" below).

---

## Test Function Naming

Test functions are named strictly after the type they test:

```
Test<TypeName>
```

Examples:

| Type under test  | Test function name   |
| ---------------- | -------------------- |
| `PgJwkSelect`    | `TestPgJwkSelect`    |
| `PgJwkSearch`    | `TestPgJwkSearch`    |
| `RestJwkList`    | `TestRestJwkList`    |
| `GrpcJwkGet`     | `TestGrpcJwkGet`     |
| `GrpcClaimsSign` | `TestGrpcClaimsSign` |
| `JwkSearch`      | `TestJwkSearch`      |

One test function per exported type. The name identifies what is under test, never what the test
does: no "TestWhenUserIsNotFound", no "TestReturnsErrorOnBadInput". Scenarios are sub-tests (see
below).

---

## Package

Always use the external test package:

```go
package handlers_test  // NOT package handlers
package core_test
package dao_test
package lib_test
```

This keeps tests off unexported internals, and honest about the public API.

---

## Sub-test Naming

Sub-test names describe the scenario:

- Use `"Success"` for the happy path.
- Use `"Success/<Variant>"` for multiple valid scenarios (`"Success/OldKeys"`, `"Success/RecentKeys"`).
- Use `"Error/<What>"` for error paths (`"Error/NotFound"`, `"Error/Internal"`, `"Error/InvalidID"`).

Never use spaces in sub-test names — Go test filtering uses `/` and spaces break it.

---

## Assertions

Use `require` everywhere, not `assert`. A sub-test stops on the first failure; continuing after a
failed assertion produces misleading output and may panic.

```go
require.NoError(t, err)
require.ErrorIs(t, err, testCase.expectErr)
require.Equal(t, testCase.expect, res)
```

For JSON payloads where `json.RawMessage` causes spurious inequality, compare marshalled forms:

```go
jsonExpect, err := json.Marshal(testCase.expect)
require.NoError(t, err)
jsonResult, err := json.Marshal(result)
require.NoError(t, err)
require.JSONEq(t, string(jsonExpect), string(jsonResult))
```

---

## Context

Use `t.Context()` instead of `context.Background()` in test bodies. This ties the context
lifetime to the test, so in-flight operations are cancelled when the test ends.

---

## Layer-specific test patterns

DAO tests against a real Postgres in a rolled-back transaction, service tests wiring layered mocks,
REST and gRPC handler test shapes, how `lib` and `pkg/go` tests differ — that is
clean-architecture-service detail, and it lives in **`write-go-service`**. Load that skill when
writing tests inside an `a-novel` service. Shared libraries under `a-novel-kit` have no such
layers: see **`write-go-kit`** for their coverage expectations and `Example_xxx` doc-test
conventions.

---

## Test Helpers

Shared test utilities belong in a `utils_test.go` file (or a dedicated `test/` subpackage when they
are shared across packages). Every helper must:

- Accept `t *testing.T` as its first argument.
- Call `t.Helper()` as its first statement, so failure attribution points at the caller.
- Use `panic` (not `require`) for setup errors that should be impossible in practice — a panic
  surfaces clearly in test output and signals a bug in the test setup, not a runtime error.

```go
func mustEncryptBase64Value(ctx context.Context, t *testing.T, data any) string {
    t.Helper()
    res, err := lib.EncryptMasterKey(ctx, data)
    if err != nil {
        panic(err)
    }
    return base64.RawURLEncoding.EncodeToString(res)
}
```

---

## Coverage

Track coverage as a signal, not a target. Gaps in trivial glue code or wired-up constructors are
acceptable; gaps in business logic, error paths, or protocol translations are not. A test written
to bump a number produces noise, not confidence. Ask of each test: "would a bug here be caught by
it?" If not, it is not worth writing.

---

## Common Pitfalls

- **Removing tests.** Delete a test only once its feature is gone; otherwise fix it.
- **Misnamed test functions.** The name must match the type under test exactly: `TestGrpcJwkGet`,
  not `TestJwkGet`.
- **Missing `t.Parallel()`.** Every test function and every sub-test calls it, absent a documented
  reason it cannot.
- **`//nolint:paralleltest` without a real reason.** It suppresses the linter and can mask a data
  race. Verify the reason is real (global mutation, non-parallelizable resource); if it is only a
  previous author's uncertainty, remove it and fix the underlying issue.
- **Data races in parallel sub-test closures.** Parallel sub-test closures run concurrently, so any
  assignment to a variable declared _outside_ the closure is a data race. Declare a new local with
  `:=` inside the closure instead of writing to an outer one with `=` — above all for `err`
  variables shared between setup code and the table loop.
- **`assert` instead of `require`.** Always use `require` in test bodies.
- **`context.Background()` in test bodies.** Use `t.Context()` instead.
- **Hard-coding mock expectations for context.** Always use `mock.Anything` for `ctx`.
- **Skipping `AssertExpectations`.** Call it for every mock, even when only the happy path was
  reached — it catches unexpected calls too.
- **Asserting response body on error paths.** For REST handlers, assert only the status code on
  error cases. The body is an implementation detail.
- **Mocking the database in DAO tests.** DAO tests always use a real database via
  `postgres.RunIsolatedTransactionalTest`. Mocks belong in service and handler tests.
- **Using DAO sentinels in handler tests.** Handler tests must not import `dao`. Their service mock
  returns the _core-layer_ sentinel (e.g., `core.ErrJwkNotFound`), not the DAO one
  (`dao.ErrJwkSelectNotFound`), mirroring what the real service returns after translation and
  keeping the test honest about the handler's contract.
- **Running the full suite during incremental work.** Scope it — see "After every edit".
