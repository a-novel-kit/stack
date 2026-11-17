# Configuration, models, clients, and entrypoints

Read this reference when routed here by [write-go-service](../SKILL.md). Its rules apply to the selected work.

## Config layer (`internal/config/`)

Configuration structs + loading from env vars or YAML. No logic beyond parsing and defaulting.

- **Static config** (feature flags, algorithm choices, topology) → YAML files.
- **Dynamic config with defaults** (ports, timeouts, hosts) → env vars via the `internal/config/env/`
  helper package.
- Group related fields into named structs; nest for sub-domains (`App.Rest`, `App.Grpc`, `App.Main`).
- **Server config comes from golib.** `App.Rest` is an `httpf.ServerConfig` loaded with
  `httpf.LoadServerConfig(getEnv, defaults)` over the `REST_*` variables; `App.Grpc` is a
  `grpcf.ServerConfig` loaded the same way over `GRPC_*`. Never redeclare those variables, types or
  defaults. A service that needs another default overrides it on a copy of
  `httpf.ServerConfigDefault` (or `grpcf.ServerConfigDefault`), next to the reason.
- **The other standard variables come from golib loaders.** `env.go` reads each block in one line
  and never redeclares its variables or defaults:
  - `otelpresets.LoadTelemetry(getEnv, otelpresets.ServerFlushTimeout)` picks the exporter and the
    loggers that match it (see [telemetry](telemetry.md#export)).
  - `postgrespresets.LoadConnection(getEnv, postgrespresets.ConnectionDefault)` reads the discrete
    `POSTGRES_*` variables, pool bounds included. `Connection.Preset` builds one preset per role,
    such as the migrations job's with its `lock_timeout`.
  - `servicetoken.LoadConfig(getEnv)` reads `SERVICE_TOKEN_ISSUER`, `SERVICE_TOKEN_AUDIENCE`,
    `SERVICE_TOKEN_CALLERS` and `SERVICE_ISSUER_OPTIONAL`.
  - `grpcf.LoadClientConfig(getEnv, "SERVICE_<NAME>", defaults)` reads a dependency's `_HOST`,
    `_PORT`, `_TLS` and `_TOKEN_PATH`. `main` dials it with `Address()` and `DialOptions()`.
- **Servers come from golib, one line each.** `cmd/` builds the router with
  `httpf.NewMux(cfg.Rest, cfg.RestLogger, cfg.App.DowntimeStart)` and runs it with
  `httpf.ListenAndServe(ctx, cfg.Rest, router)`; gRPC uses
  `grpcf.NewGRPCServer(ctx, cfg.GrpcLogger, cfg.App.DowntimeStart, nil, serviceToken...)` and
  `grpcf.ListenAndServe(ctx, cfg.Grpc, server)`. Both trace under the providers `otel.Init`
  installs. A nil base context gives each gRPC call the values of the process context, the database
  handle included. Both servers take the process context: when it ends, requests and unary calls
  drain within the shutdown timeout, while gRPC streams end with `UNAVAILABLE` so their clients
  resume elsewhere. A REST request that keeps taking on work, such as
  a batch, mounts `httpf.EndOnShutdown(ctx)` with `With`: its context ends when shutdown begins, so
  it stops starting work, which it must check its context for. What differs per service, such as the
  ping path or the CORS methods, is a default on the config copy, never an argument.
- **`main` reads as wiring.** One line per dependency, server and route. No option literals, middleware
  chains or explanatory comments inline: a route's middleware is a named value from its own file,
  mounted with chi's `With`.
- Provide a `*Default` value/function (in `*.config.default.go`) that assembles the full config
  tree from env vars and YAML — this is what `cmd/` reads at startup.
- Env var names: `<SERVICE_ENV_PREFIX>_<FIELD_NAME>`, screaming snake case. Test-only presets do
  **not** belong here — they go in a dedicated `internal/config/configtest/` package (see
  `write-go-tests`).

---

## Lib layer (`internal/lib/`)

Only tools unavailable from dependencies or stdlib. Keep it **as small as possible — ideally
nonexistent**. Before adding anything: check stdlib/deps first, then ask the developer whether it
belongs here at all (vs. inside the relevant package). Lib functions open no span; the calling
layer's span reports their errors (see [telemetry](telemetry.md)). During maintenance, look for `lib/` code a
newer upstream now subsumes, and delete it.

**Accepted exception: genai's provider connectors** (`internal/lib/connectors`). They stay here until
the a-novel-kit AI library replaces them. Several connectors implement the same modes, so the
package declares the mode interfaces and the registry that resolves them, and core takes that
registry rather than declaring an interface per mode. Do not flag either as drift, and do not
extend the pattern: any other external client goes behind a consumer-owned interface.

---

## Models (`internal/models/`)

### SQL migrations (`models/migrations/`)

Use **`write-sql`** for all migration work. Rules that span both skills:

- Always create a paired `up.sql` + `down.sql`.
- Name with a second-precise timestamp: run `date '+%Y%m%d%H%M%S'` before creating the files.
- **Never modify a `.up.sql` that has merged to `master`** — write a new migration instead. Down
  migrations, and up migrations still only on the current branch, may be freely edited.

### Protobuf (`models/proto/`)

Use **`write-proto`** for all `.proto` work — naming, the buf toolchain, breaking-change rules,
well-known types, the new-RPC walkthrough. The cross-cutting rule: proto-generated types never
enter `core` or `dao`; handlers own all proto↔core conversion.

---

## pkg/go (exported Go client)

Optional — create only when another service consumes this one as a library. When it exists: export
the minimal surface external consumers need; follow the same naming/interface rules as `internal/`;
provide a `NewClient()` that wraps connection setup and returns a clean interface; leak no internal
implementation detail through the public API.

`NewClient()` prepends `grpc.WithStatsHandler(otelgrpc.NewClientHandler())` to the caller's dial
options, so every call opens a client span and sends the caller's trace context. The client owns
its connection, so consumers trace without wiring of their own and never add a second handler. A
`pkg/go` test calls an in-process server with a caller span in context and asserts the received
`traceparent` carries its trace ID: without the handler each server starts a new trace, and no
other check notices.

A client whose requests can exceed 256 KiB also raises gRPC's retry buffer to its receive limit,
with `grpc.MaxRetryRPCBufferSize` beside `grpc.MaxCallRecvMsgSize` in its default call options.
gRPC keeps 256 KiB of a call for a retry by default. A larger call, once sent, is never retried, so
golib's in-cluster connection cannot move it to another pod when a full server refuses it. The
connection holds each request in memory until the server begins its answer.

Every package the client imports joins each consumer's binary and runs its initialization there.
The client imports no `core` or `dao` package. A type it shares with the service, such as the
claims of a token, lives in a leaf package under `internal/models`. Check the closure with
`go list -deps ./pkg/go`, and assert it in a `pkg/go` test, as service-authentication's
`Success/DependencyBoundary` case does.

---

## cmd/ (targets)

Each `cmd/<name>/main.go` wires the full dependency graph and starts a server or runs a job:

```
1. Load config (env/yaml)
2. Initialize observability (OpenTelemetry)
3. Set up shared contexts (database connection, secrets)
4. Construct DAO objects
5. Construct service objects (inject DAOs via interfaces)
6. Construct handler objects (inject services via interfaces)
7. Register routes / gRPC services
8. Start the server with graceful shutdown on SIGINT/SIGTERM
```

- Dependency injection is **explicit and manual**: no framework, no reflection. Wire everything in
  `main()`, or in the `newRouter` or `newServer` it calls, with plain constructor calls.
- Graceful shutdown: `signal.NotifyContext` gives the process context, and golib's
  `ListenAndServe` drains the server when it ends. Never `os.Exit`.

```go
func main() {
    cfg := config.AppPresetDefault

    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    lo.Must0(otel.Init(cfg.Otel))
    defer cfg.Otel.Flush()

    ctx = lo.Must(postgres.NewContext(ctx, cfg.Postgres))
    defer cfg.Postgres.Close()

    router := lo.Must(newRouter(cfg))

    lo.Must0(httpf.ListenAndServe(ctx, cfg.Rest, router))
}

// newRouter builds the REST API behind the service-token gate, which leaves the liveness probe open.
func newRouter(cfg config.App) (http.Handler, error) {
    withServiceToken, err := httpf.ServiceTokenGate(
        cfg.ServiceToken, httpf.NewPoolClient(httpf.PoolOptions{}), cfg.Logger, cfg.Rest.Liveness,
    )
    if err != nil {
        return nil, fmt.Errorf("service token gate: %w", err)
    }

    // ... DAO, core and handler wiring

    router := httpf.NewMux(cfg.Rest, cfg.RestLogger, cfg.App.DowntimeStart)

    router.Route("/v0", func(api chi.Router) {
        api.Get("/ping", httpf.Ping)
        api.Get("/healthcheck", httpf.Health(map[string]func(context.Context) error{
            "client:postgres": postgres.Health,
        }))
        // ... one line per route
    })

    return withServiceToken(router), nil
}
```

A gRPC `main` opens the same way, then serves `lo.Must(newServer(ctx, cfg))`:

```go
// newServer builds the gRPC server behind the service-token gate, which leaves the health service open.
func newServer(ctx context.Context, cfg config.App) (*grpc.Server, error) {
    serviceToken, err := grpcf.ServiceTokenGate(cfg.ServiceToken, httpf.NewPoolClient(httpf.PoolOptions{}))
    if err != nil {
        return nil, fmt.Errorf("service token gate: %w", err)
    }

    // ... DAO, core and handler wiring

    server := grpcf.NewGRPCServer(ctx, cfg.GrpcLogger, cfg.App.DowntimeStart, nil, serviceToken...)
    grpcf.RegisterHealth(ctx, server)

    // ... one Register<Service>Server line per handler

    return server, nil
}
```

- **The service token gates fail closed.** Each returns `servicetoken.ErrIssuerRequired` without
  an issuer, unless `SERVICE_ISSUER_OPTIONAL` is true. `newRouter` and `newServer` return that
  error, so `lo.Must` in `main` stops the process at startup. The REST gate leaves open the paths it
  lists, such as the liveness path. The gRPC gate leaves the health service open.
- **A service tests its gate wiring, never the gate.** golib's tests cover the gate's own cases.
  `cmd/rest`'s `TestNewRouter` and `cmd/grpc`'s `TestNewServer` build the real router or server and
  check what the wiring decides, one row each:
  - the liveness ping (REST) and the health service (gRPC, `SERVING`) answer without a token;
  - a gated route, such as the healthcheck, refuses a call without a token (401, or
    `Unauthenticated` for a unary call and a stream);
  - a valid token passes;
  - the opt-out admits a call without a token;
  - an empty issuer returns `servicetoken.ErrIssuerRequired`.

  a-novel/service-template#968 is the reference.

- **Ping and health come from golib.** `httpf.Ping` answers the liveness path with "pong".
  `httpf.Health` runs its named probes at once, each under a span `rest.Health(<name>)`, and
  answers `{"client:postgres": {"status": "up"}}`, with 503 when a probe fails. A service writes
  only the probes it owns, such as a dependency's status call.
- **A probe returns an error safe to export.** `httpf.Health` exports a probe's error on the
  probe's span. Authentication's SMTP probe returns `errSmtpUnhealthy`, never the error of the SMTP
  `Ping`, whose reply text can carry a credential.
- **`grpcf.RegisterHealth(ctx, server)`** registers the health and echo services. The health
  service reports `SERVING` until `ctx` ends, then `NOT_SERVING`, so callers stop sending calls to
  a server that drains.

### Fault reporting

A fault reaches Grafana as a failed span: Alloy counts a failed server span or root span as a
fault, and golib's [`otel`](https://pkg.go.dev/github.com/a-novel-kit/golib/otel) names its type on
that span as `error.type`, which the fault alerts group by. `write-infra`'s
[Grafana coverage](../../write-infra/references/grafana.md) holds the rest. A migrations binary
records none.

- **golib records the servers' faults:** a 5xx in `httpf.HandleError`, a gRPC call that ends with
  `Internal` or `Unavailable`, and a panic in a handler or under `otel.RecoverPanic`, which also
  logs the panic's type and stack at error level. A handler adds no record of its own, and a
  handled 4xx condition is never one.
- **A serve failure or a drain past its budget** returns from `httpf.ListenAndServe` or
  `grpcf.ListenAndServe`, and `main` exits with it. A failure while serving restarts the pod, which
  Grafana's Health dashboard counts.
- **Work that outlives its request records its own failure.** No server span sees it fail, so each
  run is the root of its own trace, `trace.WithNewRoot()`, and a failure sets
  `otel.ReportError(span, err)` and `span.SetAttributes(otel.ErrorType(err))` on that root, as
  golib's `worker.Poll` does. A job that waits for the same work records the result on the job's
  root instead.
- **A job's root span carries the job's failure.** The job runs its work with
  `worker.Run(ctx, "job.<Name>", fn)`, which opens that root span, records a failure on it the same
  way, and returns it to `main`. `main` exits non-zero after the deferred flush, so the Kubernetes
  Job fails too. Its setup, such as loading a key or connecting to the database, runs inside `fn`
  and returns its error rather than panicking past it. A usage error, such as an unknown operation,
  records nothing.
- **A job flushes its telemetry within `otelpresets.JobFlushTimeout`,** one minute, which it passes
  to `otelpresets.LoadTelemetry`. A job often ends within milliseconds, so every span and log leaves
  in that exit flush. The OpenTelemetry SDK gives the spans, then the logs, 30 seconds each. A
  server passes `otelpresets.ServerFlushTimeout`, 2 seconds.
- **A server's drain ends 6 seconds after SIGTERM.** golib's `httpf.ServerConfigDefault` and
  `grpcf.ServerConfigDefault` set the REST and gRPC shutdown budgets, so the 2-second OpenTelemetry
  flush that follows finishes well inside the pod's 30-second termination grace period. Work still
  running when the drain ends is cut, and the server returns an error wrapping
  `context.DeadlineExceeded`. Authentication alone overrides the REST budget, to 25 seconds, which
  outlasts its SMTP send timeout.
