# Service tests by layer

Read this reference when routed here by [write-go-service](../SKILL.md). Its rules apply to the selected work.

Contents:

- [Tests — layer-specific patterns](#tests--layer-specific-patterns)
- [DAO tests — real Postgres, rolled-back transaction](#dao-tests--real-postgres-rolled-back-transaction)
- [Core tests — mocks for every dependency, no DB](#core-tests--mocks-for-every-dependency-no-db)
- [REST handler tests — `net/http/httptest`, no real server](#rest-handler-tests--nethttphttptest-no-real-server)
- [gRPC handler tests — call the method directly, read the status](#grpc-handler-tests--call-the-method-directly-read-the-status)
- [lib tests — pure unit tests](#lib-tests--pure-unit-tests)
- [pkg/go tests — end-to-end against a running service](#pkggo-tests--end-to-end-against-a-running-service)
- [Common test pitfalls (service-specific)](#common-test-pitfalls-service-specific)

## Tests — layer-specific patterns

`write-go-tests` covers the common shape (table-driven, `t.Parallel()`, mockery `.EXPECT()`,
`require`, the `_test` package, cross-package fixture subpackages). These are the
service-architecture additions.

### DAO tests — real Postgres, rolled-back transaction

DAO tests run against a real PostgreSQL database inside an isolated transaction rolled back after
each sub-test, so cases can't interfere. No mocks — the point is to exercise the real DB.

```go
func TestPgJwkSelect(t *testing.T) {
    t.Parallel()

    hourAgo := time.Now().Add(-time.Hour).UTC().Round(time.Second)
    hourLater := time.Now().Add(time.Hour).UTC().Round(time.Second)

    testCases := []struct{ /* name, fixtures, request, expect, expectErr */ }{ /* … */ }

    dao := dao.NewPgJwkSelect() // constructed once, outside the loop

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            t.Parallel()

            postgres.RunIsolatedTransactionalTest(
                t, configtest.PostgresPreset, migrations.Migrations,
                func(ctx context.Context, t *testing.T) {
                    t.Helper()

                    db, err := postgres.GetContext(ctx)
                    require.NoError(t, err)

                    if len(testCase.fixtures) > 0 {
                        _, err = db.NewInsert().Model(&testCase.fixtures).Exec(ctx)
                        require.NoError(t, err)
                    }
                    // If the query reads a materialized view, refresh it after inserting fixtures —
                    // PostgreSQL does not refresh materialized views automatically inside a transaction.
                    _, err = db.NewRaw("REFRESH MATERIALIZED VIEW active_keys;").Exec(ctx)
                    require.NoError(t, err)

                    key, err := dao.Exec(ctx, testCase.request)
                    require.ErrorIs(t, err, testCase.expectErr)
                    require.Equal(t, testCase.expect, key)
                },
            )
        })
    }
}
```

- `t.Parallel()` on the outer function, like any test. Construct the DAO once outside the
  loop. Insert fixtures via `db.NewInsert().Model(...)` on the transaction-bound DB.
- A test verifying filtering/ordering needs enough fixtures to make the assertion meaningful (a
  `"FilterUsage"` case needs at least one matching row and one non-matching row).
- Use fixed UUIDs (`uuid.MustParse("00000000-0000-0000-0000-000000000001")`) and fixed timestamps
  relative to `time.Now()` (`hourAgo`, `hourLater`) so the data is deterministic and readable.

### Core tests — mocks for every dependency, no DB

```go
daoSelect := coremocks.NewMockJwkSelectDao(t)
if testCase.daoSelectMock != nil {
    daoSelect.EXPECT().
        Exec(mock.Anything, &dao.JwkSelectRequest{ID: testCase.request.ID}).
        Return(testCase.daoSelectMock.resp, testCase.daoSelectMock.err)
}

service := core.NewJwkSelect(daoSelect /* , sub-services… */)
res, err := service.Exec(t.Context(), testCase.request)
require.ErrorIs(t, err, testCase.expectErr)
require.Equal(t, testCase.expect, res)
daoSelect.AssertExpectations(t)
```

- One mock per interface the service depends on; test each independently.
- For a service that iterates a collection (calling a sub-service per DAO result), register the
  mock expectations as a slice, set each `.Once()`, and `AssertExpectations`.
- When a mock argument can't be fully specified up front (a generated UUID, an encrypted blob), use
  `mock.MatchedBy(func(r *dao.SomeRequest) bool { ... })` with a validator that calls `t.Error`
  (not `require`) and returns a bool.
- For a service returning a collection, add a `"Success/Empty"` case with `expect:
[]*core.Jwk{}` (non-nil) — `make([]*T, len(entities))` always returns a non-nil slice, so a
  `nil` expectation would diverge from reality and mask a regression.

### REST handler tests — `net/http/httptest`, no real server

```go
handler := handlers.NewRestJwkGet(service, config.LoggerDev)
w := httptest.NewRecorder()
handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/jwk?id="+id, nil))

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

- Build requests with `httptest.NewRequestWithContext(t.Context(), method, url, body)`, using the
  **real registered method and path** (`http.MethodGet`, `"/healthcheck"` not `"/"`).
- Decode the expected response into `any` (not a typed struct) — this avoids import coupling and
  matches JSON numbers as `float64`. For empty list results use `[]any{}` (not `nil`): Go encodes a
  nil slice as `null` and a non-nil empty slice as `[]`, distinct API contracts.
- Always test: success, every mapped error sentinel (e.g. 404), the generic fallback (500), and —
  if the handler parses input before calling the service — an invalid-input case. Assert the body
  on success cases (including empty collections); on error cases assert only the status code.

### gRPC handler tests — call the method directly, read the status

```go
handler := handlers.NewGrpcJwkGet(service)
res, err := handler.JwkGet(t.Context(), testCase.request)
st, ok := status.FromError(err)
require.True(t, ok, st.Code().String())
require.Equal(t, testCase.expectStatus, st.Code(), "got %s (%v)", st.Code(), err)
require.Equal(t, testCase.expect, res)
```

- Always go through `status.FromError` — even a nil error yields a valid status (`codes.OK`). Set
  `expectStatus: codes.OK` explicitly on success; set `expect` to `nil` on every error case (the
  handler returns nil on error by convention).
- Always test: success, every mapped error code (e.g. `codes.NotFound`), the generic fallback
  (`codes.Internal`), and an invalid-input case if the handler parses input first. For list
  handlers add `"Success/Empty"` with the repeated field set explicitly
  (`expect: &protogen.JwkListResponse{Keys: []*protogen.Jwk{}}`) — `lo.Map` returns a non-nil
  empty slice, so `&protogen.JwkListResponse{}` (nil `Keys`) would fail the equality check.

### lib tests — pure unit tests

`internal/lib/` tests use no mocks, no DB, no external dependencies. Test edge cases thoroughly:
invalid inputs, boundary conditions, error paths. When a `lib` function reads a context value
(a master key, …), build the context in the outer test function before the table loop so it's
shared across cases.

### pkg/go tests — end-to-end against a running service

`pkg/go` tests connect to a running service stack and exercise the exported client API. Scope the run
to the package with `a-novel test --type=go -y -C pkg/go`. Don't mock anything at this layer — the
point is the real integration path.

### Common test pitfalls (service-specific)

- **Mocking the database in DAO tests.** DAO tests always use the real DB via
  `postgres.RunIsolatedTransactionalTest`. Mocks belong in core and handler tests.
- **Using DAO sentinels in handler tests.** Handler tests must not import `dao`. The service mock
  returns the _core-layer_ sentinel (`core.ErrJwkNotFound`), not the DAO one
  (`dao.ErrJwkSelectNotFound`) — that mirrors what the real service returns after translation and
  keeps the test honest about the handler's contract.
