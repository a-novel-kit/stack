# Configuration, models, clients, and entrypoints

Read this reference when routed here by [write-go-service](../SKILL.md). Its rules apply to the selected work.

## Config layer (`internal/config/`)

Configuration structs + loading from env vars or YAML. No logic beyond parsing and defaulting.

- **Static config** (feature flags, algorithm choices, topology) → YAML files.
- **Dynamic config with defaults** (ports, timeouts, DSNs) → env vars via the `internal/config/env/`
  helper package.
- Group related fields into named structs; nest for sub-domains (`App.Rest`, `App.Grpc`, `App.Main`).
- **Server config comes from golib.** `App.Rest` is an `httpf.ServerConfig` loaded with
  `httpf.LoadServerConfig(getEnv, defaults)` over the `REST_*` variables; `App.Grpc` is a
  `grpcf.ServerConfig` loaded the same way over `GRPC_*`. Never redeclare those variables, types or
  defaults. A service that needs another default overrides it on a copy of
  `httpf.ServerConfigDefault` (or `grpcf.ServerConfigDefault`), next to the reason.
- **Servers come from golib, one line each.** `cmd/` builds the router with
  `httpf.NewRouter(cfg.Rest, cfg.Otel, cfg.HttpLogger, cfg.App.DowntimeStart)` and runs it with
  `httpf.ListenAndServe(ctx, cfg.Rest, router)`; gRPC uses
  `grpcf.NewServer(ctx, cfg.Otel, cfg.Logger, cfg.App.DowntimeStart, ctxInterceptor)` and
  `grpcf.ListenAndServe(ctx, cfg.Grpc, server)`. Both servers take the process context: when it ends,
  requests and unary calls drain within the shutdown timeout, while gRPC streams end with
  `UNAVAILABLE` so their clients resume elsewhere. A REST request that keeps taking on work, such as
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

Every package the client imports joins each consumer's binary and runs its initialization there.
The client imports no `core` or `dao` package. A type it shares with the service, such as the
claims of a token, lives in a leaf package under `internal/models`. Check the closure with
`go list -deps ./pkg/go`.

---

## cmd/ (targets)

Each `cmd/<name>/main.go` wires the full dependency graph and starts a server or runs a job:

```
1. Load config (env/yaml)
2. Initialize observability (OpenTelemetry, then the Sentry reporter)
3. Set up shared contexts (database connection, secrets)
4. Construct DAO objects
5. Construct service objects (inject DAOs via interfaces)
6. Construct handler objects (inject services via interfaces)
7. Register routes / gRPC services
8. Start the server with graceful shutdown on SIGINT/SIGTERM
```

- Dependency injection is **explicit and manual** — no framework, no reflection. Wire everything in
  `main()` with plain constructor calls.
- Graceful shutdown: `signal.Notify` on a channel, block on it, then stop the server
  (`server.GracefulStop()` for gRPC, `httpServer.Shutdown(ctx)` for HTTP). Never `os.Exit`.

```go
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit
server.GracefulStop()
```

### Error reporting

Each server and job reports its faults to Sentry through golib's
[`sentryf`](https://pkg.go.dev/github.com/a-novel-kit/golib/sentryf), whose package docs hold the
API and what an event carries. A migrations binary reports nothing.

- **The reporter's config loads like any other setting.** `internal/config/env` reads
  `Sentry = sentryf.LoadConfig(getEnv)`, so the service's prefix applies, and each binary's config
  struct carries it as a `Sentry sentryf.Config` field.
- **`main` installs the reporter right after OpenTelemetry,** before any work, and flushes it on
  exit within a bounded timeout:

  ```go
  lo.Must0(sentryf.Init(cfg.Sentry))
  defer sentryf.Flush(config.SentryFlushTimeout)
  ```

- **An empty `SENTRY_DSN` turns reporting off,** so local runs and tests need no Sentry. With a DSN,
  `Init` refuses a missing release or environment.
- **golib captures the servers' faults:** a 5xx in `httpf.HandleError`, a gRPC call that ends with
  `Internal` or `Unavailable`, a panic in a handler or under `otel.RecoverPanic`, and a drain past
  its budget or a serve failure, which `httpf.Serve` and `grpcf.ListenAndServe` report as
  `serve HTTP` or `serve gRPC`. A handler adds no capture of its own, and a handled 4xx condition is
  never captured.
- **Work that outlives its request reports its own failure.** No handler sees it fail, so its owner
  takes a report function from `cmd`, which captures each failure. Authentication's REST API wires
  `MailDelivery`'s report to `sentryf.Capture`. A job that waits for the same work reports the result
  in `main` instead, and passes no report.
- **A job's `main` captures the job's failure** under the operation's name, such as
  `sentryf.Capture(ctx, "rotate-keys", err)`, before the deferred flush runs. Its setup after
  `sentryf.Init`, such as loading a key or connecting to the database, returns its error to that
  capture rather than panicking past it. A usage error, such as an unknown operation, captures
  nothing. Prove the capture with `sentryftest.Record(t)`. The recorder swaps the process-wide
  reporter, so that test runs serially.
- **A job outwaits golib's send timeout at exit.** A Cloud Run job with Direct VPC egress waits
  30 seconds or more for Cloud NAT before its first connection out, while the database answers at
  once. golib's reporter therefore gives each event `sentryf.SendTimeout`, 2 minutes, to leave. A
  job's `main` flushes within `config.SentryJobFlushTimeout`, set to
  `sentryf.SendTimeout + time.Minute`. A server keeps `config.SentryFlushTimeout`, inside its
  platform's shutdown grace period.
- **A server's drain ends 6 seconds after SIGTERM.** `env.TimeoutShutdownDefault` sets the REST and
  gRPC shutdown budgets, so the 2-second Sentry and OpenTelemetry flushes that follow finish before
  Cloud Run's SIGKILL at 10 seconds. Work still running when the drain ends is cut, and the server
  reports it.
- **An event that cannot leave within `sentryf.SendTimeout` is lost.** The Cloud Monitoring
  job-failure alert still reports that job's failure. Production runs the jobs with OpenTelemetry
  off, so their events carry no trace ID.
