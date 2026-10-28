# Service telemetry

Read this reference when routed here by [write-go-service](../SKILL.md). Its rules apply to the selected work.

## Telemetry (OpenTelemetry)

Every DAO and service operation is wrapped in a span. A failure path returns through
`otel.ReportError(span, err)`, which marks the span failed and returns the error so it inlines in a
`return`; a success path returns directly and leaves the status unset. Neither ends the span; a
`defer span.End()` does. A closure that takes the context, such as a `WithinTx` body, belongs to its
operation's span: write-go's [declarations rule](../../write-go/SKILL.md#declarations-outside-control-flow)
keeps it a closure.

The `a-novel-must-open-span` semgrep rule requires that span in every function of the dao, core and
handlers layers that takes a context. A helper of those layers that does work takes the context for
its span even when it needs it for nothing else: `newIDs` only generates UUIDv7s, yet opens
`core.newIDs` and reports a failed `uuid.NewV7` there. `internal/lib` is tooling rather than a layer,
so its functions open no span: the layer that calls one reports its errors on its own span.

```go
ctx, span := otel.Tracer().Start(ctx, "dao.PgUserSelect")
defer span.End()
// ...
return &user, nil
```

A deferred `span.End()` ends with the function. An operation that returns a lazy iterator
(`iter.Seq2`) does its work later, as the caller reads it, so the iterator opens a sub-span of its
own, such as `"core.GenerationStream(events)"`, and reports there the error that ends it.

**Span names** follow `"<layer>.<identifier>"`:

- **Handlers** — strip the protocol prefix from the type name; the span prefix already encodes it:
  `"rest.JwkGet"` for `RestJwkGet`, `"grpc.ClaimsSign"` for `GrpcClaimsSign`. Never double it:
  `"rest.RestJwkGet"`, `"grpc.GrpcStatus"` are wrong.
- **DAO** — keep the storage prefix; it's the technology qualifier within the layer:
  `"dao.PgJwkSearch"` for `PgJwkSearch`.
- **Core** — nothing to strip: `"core.JwkSearch"` for `JwkSearch`.
- **Sub-spans** (private methods) — append in parentheses: `"rest.JwkGet(parseID)"`,
  `"grpc.Status(reportPostgres)"`.

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
- **gRPC handler** → `grpcf.Fault(code, msg, otel.ReportError(span, err))` for a server-fault code
  (`Internal`, `Unavailable`); a client-error code (`InvalidArgument`, `NotFound`) returns a
  `status.Error` without a report. A server stream whose caller left returns
  `status.FromContextError(ctx.Err())` before mapping its error: leaving cuts the upstream read the
  stream forwards, which would otherwise map to a server fault.
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

## Traces across processes

One user action is one trace, from the platform's server to the last service it reaches.

- **Incoming:** golib's presets install the W3C Trace Context propagator, and `otelhttp` and
  `otelgrpc` server instrumentation continue the caller's trace. Add nothing per handler.
- **Outgoing:** a gRPC call goes through the callee's `pkg/go` client, which traces its connection
  (see [wiring](wiring.md#pkggo-exported-go-client)). An HTTP call goes through
  `httpf.NewPoolClient`, which does the same with `otelhttp`.
- **Deferred work:** work a request creates but something else runs later, such as a sweep or a
  queue consumer, stores the creating request's `traceparent` with the work item. Inject it through
  `propagation.TraceContext{}`, never the global propagator, so baggage stays out of storage. The
  span that runs the work links to it with `trace.WithLinks` rather than taking it as parent, which
  would stretch the original trace across the work's lifetime. Skip the link when the work runs in
  the creating trace. `service-genai`'s `core.GenerationCheck` is the reference.

## Export

The servers export over golib's `otelpresets.OTLP`, configured by the standard `OTEL_EXPORTER_OTLP_*`
and `OTEL_SERVICE_NAME` variables. The preset sets no exporter option of its own, so the backend
changes with the variables alone. `service-template`'s `internal/config` is the reference.

- **Pick every preset from one flag.** `exportsOTLP` holds when `OTEL` and `OTEL_EXPORTER_OTLP_ENDPOINT`
  are both set. It chooses `LogOTel`, `GRPCOTel` and a nil request logger together. A logger that
  checked the endpoint alone would write into the disabled provider of a job without `OTEL`.
- **Read `OTEL_*` without the service's prefix.** The SDK reads them unprefixed, so the endpoint the
  service checks is the one its exporter uses.
- **Leave the liveness path untraced** with `UntracedPaths`. The probe then opens no trace, the
  handler's own span included.
- **Write no request or call log.** The server span records each request and call.
