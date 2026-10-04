# Service telemetry

Read this reference when routed here by [write-go-service](../SKILL.md). Its rules apply to the selected work.

## Telemetry (OpenTelemetry)

Every DAO and service operation is wrapped in a span. A failure path returns through
`otel.ReportError(span, err)`, which marks the span failed and returns the error so it inlines in a
`return`; a success path returns directly and leaves the status unset. Neither ends the span; a
`defer span.End()` does.

```go
ctx, span := otel.Tracer().Start(ctx, "dao.PgUserSelect")
defer span.End()
// ...
return &user, nil
```

**Span names** follow `"<layer>.<identifier>"`:

- **Handlers** — strip the protocol prefix from the type name; the span prefix already encodes it:
  `"rest.JwkGet"` for `RestJwkGet`, `"grpc.ClaimsSign"` for `GrpcClaimsSign`. Never double it:
  `"rest.RestJwkGet"`, `"grpc.GrpcStatus"` are wrong.
- **DAO** — keep the storage prefix; it's the technology qualifier within the layer:
  `"dao.PgJwkSearch"` for `PgJwkSearch`.
- **Core** — nothing to strip: `"core.JwkSearch"` for `JwkSearch`.
- **Sub-spans** (private methods) — append in parentheses: `"rest.JwkGet(parseID)"`,
  `"grpc.Status(reportPostgres)"`.

> Some existing services (and `service-template`) currently use a bare `"handler.X"` prefix on
> handler spans instead of `"rest.X"` / `"grpc.X"`. That is tech debt — bring a handler span name
> in line with the rule above when the file is already in scope for a change.

**Reporting follows `write-go`**: every failed operation reports on its own span, and the detail is
recorded once. Here is how it lands on the service layers:

- **DAO** hits `sql.ErrNoRows`, a unique violation, etc. → `otel.ReportError`. It ran a query and
  got a real outcome it didn't fully resolve. (Join the domain sentinel onto the error first so
  callers keep `errors.Is` identity.) Its span holds the description.
- **Core** that produces a sentinel itself (validation failure, claim/source mismatch, a wrong
  secret it detected) → `otel.ReportError`; its span holds the description.
- **Core** that receives an error from a DAO/sub-service and returns it upward → still
  `otel.ReportError`. Its span takes the `Error` status; the description stays on the child span.
- **REST handler** → `httpf.HandleError` marks the span for a 5xx and leaves a 4xx unset; never add
  a report on a path that goes through it. A handler that writes its own error status reports only a
  server fault's cause.
- **gRPC handler** → `_ = otel.ReportError(span, err)` before a server-fault code (`Internal`,
  `Unavailable`); a client-error code (`InvalidArgument`, `NotFound`) returns without it.
- **Handled conditions** (a best-effort cleanup failed, a cap was hit) → no span status. The child
  span that failed already holds the error; log only what needs an operator and no span records.
- **Anti-pattern**: a `reportUnexpected(span, err)` keyed on a list of "known" sentinels, or a bare
  `return nil, ErrXxx` from a function that has a span.

The "is the service broken" view is built on the server span's **HTTP status code**, which the otel
HTTP instrumentation records. Core and DAO spans answer "did this operation fail", which is `true`
for the lookup behind a deliberate 404. For bulk-anomaly visibility on a specific security sentinel (a
spike of `ErrInvalidSignature`), use a counter or an audit log rather than span status.

**Span attributes** use a semantic `<entity>.<field>` scheme describing the _data_, not the Go
variable holding it: `"key.id"`, `"key.usage"`, `"key.expires_at"` — never `"request.Jwk.*"` or
`"request.Usage"`. Each fact is set once, on the first span that learns it: a request input on the
outermost span that receives it, so a DAO does not repeat its caller's input; a result on the
innermost span that produces it, so a core span does not repeat its DAO's row count. A shared
operation called from several parents keeps an input any of them lacks. Leave out constants,
configuration values, and timestamps the span's own timing already gives. Do **not** add spans to
constructors or config loading, nor span events: only operations that do real work get a span.
