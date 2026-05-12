---
name: write-go-tests
description: >
  Test conventions for ALL Go code in the a-novel and a-novel-kit organizations — file/function
  naming, the table-driven structure, mockery usage, assertions, parallelism, cross-package
  fixtures, helpers, and coverage. Load this skill whenever writing or modifying a Go test file
  (new tests, regression coverage, mock wiring, or updating tests after a refactor) in a backend
  service OR a shared library. Pairs with `write-go` (base Go conventions); the layer-specific
  test patterns for clean-architecture services (the Postgres transactional harness for DAO
  tests, REST/gRPC handler test shapes, …) live in `write-go-service`. Does NOT apply to JS/TS
  tests.
---

# Go Test Conventions

This skill governs how to write Go tests across every a-novel / a-novel-kit repository — backend
services and shared libraries alike. Tests define behavior, document contracts, and guard against
regressions; they must be clear, isolated, and exhaustive for the paths they cover. Load it
alongside `write-go` (base Go conventions) and the repo-kind skill — `write-go-service` or
`write-go-kit`.

**Before writing any test**, read the existing tests in the same package. Patterns are consistent
by design — follow them exactly. Read the production code being tested too; do not guess at
behavior or signatures.

**Look up the testing libraries online.** When using `testify`, `httptest`, `mockery`, or any other
test helper, check the official documentation and real-world usage examples before writing. This is
especially important for mock assertion patterns (`EXPECT`, `.Once()`, `mock.MatchedBy`) and for
JSON comparison utilities. Correct usage prevents subtle false-positives or missed failures in tests.

**Never remove existing tests** unless the feature they cover is fully deprecated and removed from
the codebase. Stale or failing tests must be fixed, not deleted.

---

## After every edit

Run the narrowest test target that exercises the code you changed:

```
make test-unit   # services: internal/ — dao, services, handlers, lib
make test-pkg    # services: pkg/go — exported Go client
make test        # libraries (golib, jwt, …); also the full pre-commit run on services
```

On services, never run the full `make test` during incremental work — `make test-unit` /
`make test-pkg` are faster and targeted; reserve `make test` for the final commit. Libraries
typically expose only `make test`.

---

## Test File Naming

Test files follow the same naming convention as the production files they cover, with a `_test.go`
suffix:

| Production file           | Test file                  |
| ------------------------- | -------------------------- |
| `pg.userSelect.go`        | `pg.userSelect_test.go`    |
| `rest.userList.go`        | `rest.userList_test.go`    |
| `grpc.orderCreate.go`     | `grpc.orderCreate_test.go` |
| `userSearch.go` (service) | `userSearch_test.go`       |

**Underscore, not dot.** The Go toolchain only excludes files ending in `_test.go` (with an
underscore) from production builds. A file named `something.test.go` (with a dot) is **compiled
into the production binary** — the `.test.` part is just text in the filename, not a build-tag
signal. If you find a file with that shape carrying test-only globals, it has leaked from the
test surface into the shipped binary and must be moved (see "Cross-package test fixtures" below).

---

## Cross-Package Test Fixtures

Some test fixtures need to be shared across packages — for example, a Postgres preset reused by
both `dao_test` and `handlers_test`. Because Go's `_test.go` rule is per-package (a `_test.go`
file in package X cannot be imported from a `_test.go` file in package Y), these shared fixtures
have to live in regular `.go` files. The risk: regular `.go` files are compiled into production
binaries.

**Always isolate cross-package fixtures into a dedicated subpackage.** Name the directory and
package after the layer plus the suffix `test`, mirroring Go stdlib conventions like
`net/http/httptest` and `testing/iotest`:

| Layer       | Subpackage path                   | Package name   |
| ----------- | --------------------------------- | -------------- |
| `config/`   | `internal/config/configtest/`     | `configtest`   |
| `lib/`      | `internal/lib/libtest/`           | `libtest`      |
| `services/` | `internal/services/servicestest/` | `servicestest` |

```go
// internal/config/configtest/postgres.go
package configtest

// PostgresPreset is the PostgreSQL configuration used in integration tests.
var PostgresPreset = postgrespresets.NewDefault(pgdriver.WithDSN(env.PostgresDsn))
```

Test files import it as `configtest`:

```go
import (
    "github.com/a-novel/service-json-keys/v2/internal/config/configtest"
)

postgres.NewContext(ctx, configtest.PostgresPreset)
```

**Never:**

- Define test fixtures in the production package (e.g., `internal/config/postgres.config.go`)
  guarded only by a `Test` prefix on the variable. The variable is exported and compiled in,
  and a future change can wire it into a production code path without a single review flag.
- Use `.test.go` (with a dot) as a substitute for `_test.go`. As covered above, the dot is not
  recognized by the Go toolchain — the file is compiled into the production binary.
- Reuse the bare name `testutils` for multiple fixture subpackages in the same project. Two
  imports of `testutils` from different paths force aliasing at every call site, which is the
  same kind of low-level entropy this skill exists to prevent. Use the layer-prefixed name
  (`configtest`, `libtest`) so each fixture subpackage has a unique, descriptive name.

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

One test function per exported type. Never name a test after a behavior, scenario, or action
("TestWhenUserIsNotFound", "TestReturnsErrorOnBadInput") — the name identifies what is under test,
not what the test does. Scenarios are sub-tests (see below).

---

## Package

Always use the external test package:

```go
package handlers_test  // NOT package handlers
package services_test
package dao_test
package lib_test
```

This prevents tests from relying on unexported internals, which keeps them honest about the public API.

---

## Table-Driven Structure

Every test uses a table of cases. The top-level test function sets up shared state and defines the
table; each case runs in a sub-test.

```go
func TestGrpcJwkGet(t *testing.T) {
    t.Parallel()

    errFoo := errors.New("foo")  // generic internal error for error-path cases

    type serviceMock struct {
        resp *services.Jwk
        err  error
    }

    testCases := []struct {
        name string

        request *protogen.JwkGetRequest

        serviceMock *serviceMock  // nil → mock must not be called

        expect       *protogen.JwkGetResponse
        expectStatus codes.Code
    }{
        {
            name: "Success",
            // ...
        },
        {
            name: "Error/NotFound",
            // ...
        },
        {
            name: "Error/Internal",
            // ...
        },
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            t.Parallel()
            // ...
        })
    }
}
```

**Key rules:**

- Call `t.Parallel()` at the top of the outer test function.
- Call `t.Parallel()` at the top of every sub-test body.
- Exception: if the test genuinely cannot be parallelized (e.g., it mutates global state or
  uses a non-parallelizable resource), suppress the linter with `//nolint:paralleltest` on the
  outer function and `//nolint:tparallel` inside sub-tests — and add a comment explaining why.
- Define inline mock structs (`type serviceMock struct{...}`) inside the test function, not at
  package level. This keeps each test self-contained.
- Use `errors.New("foo")` (typically named `errFoo`) as a sentinel for generic internal error
  paths that need a non-nil, non-sentinel error.

---

## Sub-test Naming

Sub-test names describe the scenario:

- Use `"Success"` for the happy path.
- Use `"Success/<Variant>"` for multiple valid scenarios (`"Success/OldKeys"`, `"Success/RecentKeys"`).
- Use `"Error/<What>"` for error paths (`"Error/NotFound"`, `"Error/Internal"`, `"Error/InvalidID"`).

Never use spaces in sub-test names — Go test filtering uses `/` and spaces break it.

---

## Mocks

Mocks are generated by `mockery` from the interfaces defined in each production file. Run
`make generate` after adding or changing any interface. Never write mocks by hand.

**Instantiate** a mock with the generated constructor:

```go
service := handlersmocks.NewMockGrpcJwkGetService(t)
repository := servicesmocks.NewMockJwkSearchRepository(t)
```

**Set expectations** with `.EXPECT()`:

```go
service.EXPECT().
    Exec(mock.Anything, &services.JwkSelectRequest{
        ID: uuid.MustParse(testCase.request.GetId()),
    }).
    Return(testCase.serviceMock.resp, testCase.serviceMock.err)
```

- Use `mock.Anything` for the `ctx` argument — context identity is not meaningful to assert.
- Use concrete expected values for all other arguments. This is the contract being enforced.
- Add `.Once()` when the same mock method is registered multiple times in a loop (e.g., for each
  item in a slice).

**Nil-mock pattern**: declare mock fields as pointers in the test case struct. When nil, the mock
should not be called at all — simply skip registering the expectation:

```go
if testCase.serviceMock != nil {
    service.EXPECT().Exec(...).Return(...)
}
```

**Always call `AssertExpectations`** at the end of each sub-test for every mock:

```go
service.AssertExpectations(t)
repository.AssertExpectations(t)
```

This verifies that every registered expectation was actually called.

---

## Assertions

Use `require` everywhere, not `assert`. Sub-tests stop on the first failure; continuing after a
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

How DAO tests run against a real Postgres in a rolled-back transaction, how service tests wire
layered mocks, how REST and gRPC handler tests are structured, how `lib` and `pkg/go` tests
differ — that is all clean-architecture-service detail and lives in **`write-go-service`**. Load
that skill when writing tests inside an `a-novel` service. Shared libraries under `a-novel-kit`
have no such layers: see **`write-go-kit`** for their coverage expectations and `Example_xxx`
doc-test conventions.

---

## Test Helpers

Shared test utilities belong in a `utils_test.go` file (or a dedicated `test/` subpackage for
helpers that need to be shared across packages). Every helper must:

- Accept `t *testing.T` as its first argument.
- Call `t.Helper()` as its first statement, so failure attribution points at the caller.
- Use `panic` (not `require`) for setup errors that should be impossible in practice — panics
  surface clearly in test output and signal a bug in the test setup, not a runtime error.

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

Track coverage as a signal, not a target. The goal is meaningful tests, not a high percentage.
Coverage gaps in trivial glue code or wired-up constructors are acceptable; gaps in business logic,
error paths, or protocol translations are not. Adding a test just to bump a number produces noise,
not confidence. When evaluating what to test, ask: "would a bug here be caught by this test?"
If the answer is no, the test is not worth writing.

---

## Common Pitfalls

- **Removing tests.** Never delete a test unless its feature is fully removed. Fix it instead.
- **Misnamed test functions.** The test function name must match the type under test exactly:
  `TestGrpcJwkGet` not `TestJwkGet`, `TestPgJwkSearch` not `TestJwkSearch`.
- **Missing `t.Parallel()`.** Every test function and every sub-test must call it unless there
  is a documented reason they cannot.
- **`//nolint:paralleltest` without a real reason.** This annotation suppresses the linter but
  can mask data races. Before adding it, verify there is an actual reason (global mutation,
  non-parallelizable resource). If the only reason is that a previous author was unsure, remove
  it and fix the underlying issue.
- **Data races in parallel sub-test closures.** When sub-tests run with `t.Parallel()`, their
  closures execute concurrently. Any assignment to a variable declared _outside_ the closure is
  a data race. Use `:=` (short declaration) inside the closure to declare a new local variable,
  not `=` (assignment) to write to an outer one. This applies especially to `err` variables
  shared between setup code and the table loop.
- **`assert` instead of `require`.** Always use `require` in test bodies.
- **`context.Background()` in test bodies.** Use `t.Context()` instead.
- **Hard-coding mock expectations for context.** Always use `mock.Anything` for `ctx`.
- **Skipping `AssertExpectations`.** Always call it for every mock, even if only the happy path
  was reached — it catches unexpected calls too.
- **Asserting response body on error paths.** For REST handlers, only assert the status code on
  error cases. The body is an implementation detail.
- **Mocking the database in DAO tests.** DAO tests always use a real database via
  `postgres.RunIsolatedTransactionalTest`. Mocks belong in service and handler tests.
- **Using DAO sentinels in handler tests.** Handler tests must not import `dao`. The service mock
  in a handler test should return the _service-layer_ sentinel (e.g., `services.ErrJwkNotFound`),
  not the DAO sentinel (`dao.ErrJwkSelectNotFound`). This mirrors what the real service returns
  after translation, and keeps the test honest about the handler's actual contract.
- **Running `make test` during incremental work.** Use `make test-unit` or `make test-pkg` to
  avoid running the full suite on every change.
