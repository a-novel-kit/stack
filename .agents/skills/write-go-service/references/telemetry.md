# Service telemetry

Read this reference when routed here by [write-go-service](../SKILL.md). Its rules apply to the selected work.

## Telemetry (OpenTelemetry)

Every DAO and service operation is wrapped in a span. Three `golib/otel` helpers cover every return
path:

- `otel.ReportError(span, err)` — `RecordError` + `SetStatus(Error)`, returns `err` so it inlines in
  a `return`. It does **not** end the span; a `defer span.End()` does.
- `otel.ReportSuccess(span, value)` — `SetStatus(Ok)`, returns `value`.
- `otel.ReportSuccessNoContent(span)` — `SetStatus(Ok)` for void operations.

```go
ctx, span := otel.Tracer().Start(ctx, "dao.PgUserSelect")
defer span.End()
// ...
return otel.ReportSuccess(span, &user), nil
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

**Reporting is layer-relative** — `write-go` states the principle; here is how it lands on the
service layers:

- **DAO** hits `sql.ErrNoRows`, a unique violation, etc. → `otel.ReportError`. It ran a query and
  got a real outcome it didn't fully resolve. (Join the domain sentinel onto the error first so
  callers keep `errors.Is` identity.)
- **Core** that produces a sentinel itself (validation failure, claim/source mismatch, a wrong
  secret it detected) → `otel.ReportError`. It raised it.
- **Core** that receives an error from a DAO/sub-service and returns it upward → still
  `otel.ReportError`. Returning upward is _propagating_, not discarding; wrapping it changes nothing.
- **Handler** that maps the error to a transport response → it reports too. `httpf.HandleError`
  calls `otel.ReportError` unconditionally before writing the HTTP status; on the gRPC side, do the
  same by hand (`_ = otel.ReportError(span, err)` before `status.Error(...)`). The handler span
  then shows which error each request ended on, whatever status it mapped to. The handler is the
  _boundary_, not a layer that makes the error vanish.
- **Anti-pattern**: a `reportUnexpected(span, err)` keyed on a list of "known" sentinels, used at
  _any_ layer that still propagates or surfaces the error. It couples the layer to an error
  registry and drops real signal. The forbidden moves are: suppressing reporting based on the
  error's identity, and `return nil, ErrXxx` bare from a layer that has a span. Every span'd layer
  reports.

The "is the service broken" view is built on the **HTTP status code**, which the otel HTTP
instrumentation records — not on span status. Span status answers "did an error occur in
processing", which is `true` even for a deliberate 404. That is fine: spans are independent, so
every layer's span can record "no row" while the dashboard still counts the 404 as a 404. For
bulk-anomaly visibility on a specific security sentinel (a spike of `ErrInvalidSignature`), use a
counter, audit log, or dedicated event rather than `span.status`.

**Span attributes** use a semantic `<entity>.<field>` scheme describing the _data_, not the Go
variable holding it: `"key.id"`, `"key.usage"`, `"key.expires_at"` — never `"request.Jwk.*"` or
`"request.Usage"`. Add request attributes with `span.SetAttributes(attribute.String(...))` for
debuggability. Do **not** add spans to constructors or config loading — only to operations that do
real work.
