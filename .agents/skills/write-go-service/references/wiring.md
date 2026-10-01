# Configuration, models, clients, and entrypoints

Read this reference when routed here by [write-go-service](../SKILL.md). Its rules apply to the selected work.

## Config layer (`internal/config/`)

Configuration structs + loading from env vars or YAML. No logic beyond parsing and defaulting.

- **Static config** (feature flags, algorithm choices, topology) → YAML files.
- **Dynamic config with defaults** (ports, timeouts, DSNs) → env vars via the `internal/config/env/`
  helper package.
- Group related fields into named structs; nest for sub-domains (`App.Rest`, `App.Grpc`, `App.Main`).
- Provide a `*Default` value/function (in `*.config.default.go`) that assembles the full config
  tree from env vars and YAML — this is what `cmd/` reads at startup.
- Env var names: `<SERVICE_ENV_PREFIX>_<FIELD_NAME>`, screaming snake case. Test-only presets do
  **not** belong here — they go in a dedicated `internal/config/configtest/` package (see
  `write-go-tests`).

---


## Lib layer (`internal/lib/`)

Only tools unavailable from dependencies or stdlib. Keep it **as small as possible — ideally
nonexistent**. Before adding anything: check stdlib/deps first, then ask the developer whether it
belongs here at all (vs. inside the relevant package). During maintenance, look for `lib/` code a
newer upstream now subsumes, and delete it.

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
