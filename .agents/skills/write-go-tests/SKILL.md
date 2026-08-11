---
name: write-go-tests
description: >
  Test conventions for ALL Go code in the a-novel and a-novel-kit organizations — file/function
  naming, table-driven structure, mockery, assertions, parallelism, cross-package fixtures,
  helpers, coverage. Load it whenever writing or modifying a Go test file in a backend service OR
  a shared library. Pairs with `write-go`; layer-specific patterns live in `write-go-service`.
  Does NOT apply to JS/TS tests.
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

## After every edit

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

## Cross-Package Test Fixtures

Some fixtures are shared across packages — a Postgres preset reused by both `dao_test` and
`handlers_test`, say. Go's `_test.go` rule is per-package (package X's `_test.go` cannot be
imported from package Y's), so a shared fixture has to live in a regular `.go` file, which is
compiled into production binaries.

**Always isolate cross-package fixtures into a dedicated subpackage.** Name the directory and
package after the layer plus the suffix `test`, mirroring Go stdlib conventions like
`net/http/httptest` and `testing/iotest`:

| Layer     | Subpackage path               | Package name |
| --------- | ----------------------------- | ------------ |
| `config/` | `internal/config/configtest/` | `configtest` |
| `lib/`    | `internal/lib/libtest/`       | `libtest`    |
| `core/`   | `internal/core/coretest/`     | `coretest`   |

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
  guarded only by a `Test` prefix on the variable. The variable is exported and compiled in, and a
  future change can wire it into a production code path without a single review flag.
- Use `.test.go` (with a dot) as a substitute for `_test.go` — the Go toolchain does not recognize
  the dot, so the file is compiled into the production binary.
- Reuse the bare name `testutils` for several fixture subpackages in one project. Two imports of
  `testutils` from different paths force aliasing at every call site. Use the layer-prefixed name
  (`configtest`, `libtest`) so each fixture subpackage has a unique, descriptive name.

---

## Static and large test data

Keep only short values inline when they make a test case easier to read. Put structured definitions
and large payloads under the package's `testdata/` directory, then embed them from an `_test.go` file
with `//go:embed`.

Prefer YAML (`.yaml`) for human-authored semantic fixtures. Convert it to the production format only at
the boundary the test exercises. Keep JSON when its exact representation is part of the behavior:
parser or encoder cases, exact wire bytes, malformed JSON, and byte-size boundaries. A production JSON
asset, including a JSON Schema document, keeps its native format when a test embeds it.

Reuse the repository's YAML parser. If none exists, apply `choose-dependency`; this preference does not
waive approval for a new package.

Reuse existing fixture and mock data before adding another definition. Keep one canonical large value
and derive small case-specific variants from it. When multiple packages need the same data, let the
dedicated `*test` fixture subpackage own and expose it instead of copying it into several `testdata/`
directories.

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

## Table-Driven Structure

Every test uses a table of cases. The top-level test function sets up shared state and defines the
table; each case runs in a sub-test.

```go
func TestGrpcJwkGet(t *testing.T) {
    t.Parallel()

    errFoo := errors.New("foo")  // generic internal error for error-path cases

    type serviceMock struct {
        resp *core.Jwk
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
- Exception: when the test genuinely cannot be parallelized (it mutates global state, or uses a
  non-parallelizable resource), suppress the linter with `//nolint:paralleltest` on the outer
  function and `//nolint:tparallel` inside sub-tests — and add a comment explaining why.
- Define inline mock structs (`type serviceMock struct{...}`) inside the test function, not at
  package level, so each test stays self-contained.
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
`pnpm generate:go` after adding or changing any interface. Never write mocks by hand.

**Instantiate** a mock with the generated constructor:

```go
service := handlersmocks.NewMockGrpcJwkGetService(t)
daoSearch := coremocks.NewMockJwkSearchDao(t)
```

**Set expectations** with `.EXPECT()`:

```go
service.EXPECT().
    Exec(mock.Anything, &core.JwkSelectRequest{
        ID: uuid.MustParse(testCase.request.GetId()),
    }).
    Return(testCase.serviceMock.resp, testCase.serviceMock.err)
```

- Use `mock.Anything` for the `ctx` argument — context identity is not meaningful to assert.
- Use concrete expected values for all other arguments. They are the contract being enforced.
- Add `.Once()` when the same mock method is registered several times in a loop (e.g., for each
  item in a slice).

**Nil-mock pattern**: declare mock fields as pointers in the test case struct. A nil field means
the mock must not be called at all, so skip registering the expectation:

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

It verifies every registered expectation was called.

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
