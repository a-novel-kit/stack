---
name: write-go-tests
description: >
  Write, review, and maintain Go tests for Agora backend services. Use this skill whenever writing
  or modifying test files — new tests, regression coverage, mock wiring, or updating tests after a
  refactor. Applies to all layers (dao, services, handlers, lib) and exported packages (pkg/go).
  Does NOT apply to JS/TS tests.
---

# Go Test Writing Skill

This skill governs how to write Go tests in Agora backend services. Tests define behavior, document
contracts, and guard against regressions. They must be clear, isolated, and exhaustive for the
paths they cover.

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

## After Every Edit

Run only the targets that cover what you changed:

```
make test-unit   # internal/ — dao, services, handlers, lib
make test-pkg    # pkg/go — exported Go client
```

Never run `make test` (the full suite) during incremental work — it is reserved for the final
commit. Run the narrowest target that exercises the changed code.

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

## Layer-Specific Guidance

### DAO Tests

DAO tests run against a real PostgreSQL database inside an isolated transaction. The transaction
is automatically rolled back after each sub-test, so cases cannot interfere with each other.

```go
func TestPgJwkSelect(t *testing.T) {
    t.Parallel()

    // Fixed timestamps relative to now — deterministic and easy to read.
    hourAgo := time.Now().Add(-time.Hour).UTC().Round(time.Second)
    hourLater := time.Now().Add(time.Hour).UTC().Round(time.Second)

    testCases := []struct{ ... }{ ... }

    repository := dao.NewPgJwkSelect()  // constructed once, outside the loop

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            t.Parallel()

            postgres.RunIsolatedTransactionalTest(
                t,
                testutils.PostgresPresetTest,
                migrations.Migrations,
                func(ctx context.Context, t *testing.T) {
                    t.Helper()

                    db, err := postgres.GetContext(ctx)
                    require.NoError(t, err)

                    if len(testCase.fixtures) > 0 {
                        _, err = db.NewInsert().Model(&testCase.fixtures).Exec(ctx)
                        require.NoError(t, err)
                    }

                    // If the query reads a materialized view, refresh it after inserting fixtures.
                    _, err = db.NewRaw("REFRESH MATERIALIZED VIEW active_keys;").Exec(ctx)
                    require.NoError(t, err)

                    key, err := repository.Exec(ctx, testCase.request)
                    require.ErrorIs(t, err, testCase.expectErr)
                    require.Equal(t, testCase.expect, key)
                },
            )
        })
    }
}
```

Key rules:

- Always call `t.Parallel()` at the top of the outer test function, just like any other test.
- Construct the repository once outside the table loop: `repository := dao.NewPgJwkSelect()`.
- Insert fixtures using bun's `NewInsert().Model(...)` directly on the transaction-bound DB.
- If the operation depends on a materialized view, refresh it manually after inserting fixtures —
  PostgreSQL does not refresh materialized views automatically within a transaction.
- Test cases that verify filtering or ordering must include enough fixtures to make the assertion
  meaningful. A test named `"FilterUsage"` should have at least one row for the target usage and
  one for a different usage.
- Use fixed UUIDs (`uuid.MustParse("00000000-0000-0000-0000-000000000001")`) and fixed timestamps
  relative to `time.Now()` (e.g., `hourAgo`, `hourLater`) so the test data is deterministic and
  easy to read.
- Do not use mocks in DAO tests. The whole point of DAO tests is to exercise the real database
  interaction.

### Services Tests

Service tests use mocks for all DAO and sub-service dependencies. No database access.

```go
repositorySelect := servicesmocks.NewMockJwkSelectRepository(t)
serviceExtract := servicesmocks.NewMockJwkSelectServiceExtract(t)

if testCase.repositorySelectMock != nil {
    repositorySelect.EXPECT().
        Exec(mock.Anything, &dao.JwkSelectRequest{ID: testCase.request.ID}).
        Return(testCase.repositorySelectMock.resp, testCase.repositorySelectMock.err)
}

service := services.NewJwkSelect(repositorySelect, serviceExtract)

res, err := service.Exec(ctx, testCase.request)
require.ErrorIs(t, err, testCase.expectErr)
require.Equal(t, testCase.expect, res)

repositorySelect.AssertExpectations(t)
serviceExtract.AssertExpectations(t)
```

- Test every service dependency independently: one mock for each interface the service depends on.
- For services that iterate over a collection (e.g., calling extract for each DAO result), define
  the mock expectations as a slice, set each with `.Once()`, and assert that all were consumed.
- When a mock argument cannot be fully specified in advance (e.g., a generated UUID or encrypted
  key), use `mock.MatchedBy(func(r *dao.SomeRequest) bool { ... })` with a validation function
  that calls `t.Error` (not `require`) and returns a bool.
- For services that return a collection, add a `"Success/Empty"` case with `expect: []*services.Jwk{}`
  (non-nil empty slice). Services that use `make([]*T, len(entities))` always return a non-nil slice
  — a nil `expect` would diverge from the real return value and mask a regression.

### REST Handler Tests

REST handler tests use `net/http/httptest` — no real server required.

```go
handler := handlers.NewRestJwkGet(service, config.LoggerDev)
w := httptest.NewRecorder()

handler.ServeHTTP(w, testCase.request)

res := w.Result()
require.Equal(t, testCase.expectStatus, res.StatusCode)

if testCase.expectResponse != nil {
    data, err := io.ReadAll(res.Body)
    require.NoError(t, errors.Join(err, res.Body.Close()))

    var jsonRes any
    require.NoError(t, json.Unmarshal(data, &jsonRes))
    require.Equal(t, testCase.expectResponse, jsonRes)
}
```

- Build requests with `httptest.NewRequestWithContext(t.Context(), method, url, body)`.
- Use the **correct HTTP method** that matches the route registration (`http.MethodGet` for GET
  routes, etc.). The handler's `ServeHTTP` may not check the method, but the test should still
  reflect the real contract to avoid misleading readers.
- Use the **actual registered path** for the URL (e.g., `"/healthcheck"` not `"/"`).
- Use `any` (not a typed struct) as the expected response type — this avoids import coupling and
  matches JSON numbers as `float64`, which is what `json.Unmarshal` into `any` produces.
- Always test: the success path, each mapped error sentinel (e.g., 404), and the generic fallback
  (500). Test invalid input (e.g., unparseable ID) if the handler parses input before calling the
  service.
- Assert the response body for every success case, including empty collection responses. Use
  `expectResponse: []any{}` (not nil) for empty list results — Go encodes a nil slice as `null`
  and a non-nil empty slice as `[]`, which are distinct API contracts. Leaving the body unchecked
  allows a nil-vs-empty regression to pass silently.
- Do not assert on the response body for error cases — only the status code matters.

### gRPC Handler Tests

gRPC tests call the method directly and extract the gRPC status code from the returned error.

```go
handler := handlers.NewGrpcJwkGet(service)

res, err := handler.JwkGet(t.Context(), testCase.request)
resSt, ok := status.FromError(err)
require.True(t, ok, resSt.Code().String())
require.Equal(
    t,
    testCase.expectStatus, resSt.Code(),
    "expected status code %s, got %s (%v)", testCase.expectStatus, resSt.Code(), err,
)
require.Equal(t, testCase.expect, res)
```

- Always check `status.FromError` — even a nil error produces a valid status (`codes.OK`).
- Set `expectStatus: codes.OK` explicitly on success cases; do not leave it zero-valued.
- Set `expect` (the response) to nil for all error cases — the handler returns nil on error by
  convention.
- Always test: the success path, each mapped error sentinel (e.g., `codes.NotFound`), and the
  generic fallback (`codes.Internal`). Test invalid input (e.g., `"Error/InvalidID"` with a
  non-UUID string) if the handler parses input before calling the service.
- For gRPC list handlers, add a `"Success/Empty"` case. Handlers use `lo.Map` which always
  returns a non-nil empty slice, so `expect` must set the repeated field explicitly:
  `expect: &protogen.JwkListResponse{Keys: []*protogen.Jwk{}}`. Using `&protogen.JwkListResponse{}`
  (nil Keys) would fail because `nil != []*protogen.Jwk{}`.

### lib Tests

`lib/` tests are pure unit tests — no mocks, no external dependencies, no database. Test edge
cases thoroughly: invalid inputs, boundary conditions, and error paths.

When a `lib` function uses a context value (e.g., a master key), set up the context in the outer
test function before the table loop so it is shared across cases:

```go
ctxReal, err := lib.NewMasterKeyContext(t.Context(), masterKey)
require.NoError(t, err)
```

### pkg/go Tests

`pkg/go` tests are end-to-end integration tests that connect to a running service. They are run
with `make test-pkg`, not `make test-unit`. They test the exported client API, not individual
handlers or services.

These tests require the full service stack to be running. Do not try to mock anything at this
layer — the point is to test the real integration path.

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
