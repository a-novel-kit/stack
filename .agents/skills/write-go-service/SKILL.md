---
name: write-go-service
description: >
  Clean-architecture conventions for the a-novel backend SERVICES — `app/service-*`,
  `app/platform-*`, and any repo with the `cmd`/`internal/{config,lib,dao,core,handlers,models}`/`pkg`
  layout. Load this skill whenever writing or modifying Go in such a repo: new endpoints, schema
  changes, business logic, new layers, handlers, refactors, or layer-specific tests (DAO Postgres
  harness, REST/gRPC handler tests). Covers the layer split and import direction, the
  interface+implementation pattern, DAO/core/handlers/config/lib/models/pkg/cmd, transaction
  scoping, OpenTelemetry instrumentation, and the service security model. ALWAYS load `write-go`
  (the base Go conventions) ALONGSIDE this skill; for shared libraries under `a-novel-kit` use
  `write-go-kit` instead of this one. Pairs with `write-go-tests`, `document-code`, `write-sql`,
  `write-proto`, `write-openapi`. Does NOT cover JS/TS.
---

# Go — Backend Services (clean architecture)

This skill governs Go in the a-novel backend services: repos shaped as
`cmd/` + `internal/{config,lib,dao,core,handlers,models}` + (optionally) `pkg/`. The goal is
coherent, idiomatic, minimal code that strictly respects the layered architecture.

**This skill is layered on `write-go`.** Everything in `write-go` — read-before-edit, the
`pnpm format:go`/`pnpm lint:go` discipline, dependency policy, naming of packages/files/variables,
constructors, error sentinels and `%w` wrapping, context rules, time-capture-once, secrets
hygiene, the layer-relative span-reporting rule — applies here unchanged. This skill adds only the
**service-architecture-specific** rules on top. Load `write-go` and `write-go-tests` (and
`document-code` when documenting) alongside it. For shared libraries under `a-novel-kit` (`golib`,
`jwt`, …) load `write-go-kit` instead of this skill.

**Before touching any layer**, read its sibling files, the interfaces it depends on, and the
interfaces it exposes. These services are deliberately consistent — copy the established pattern
exactly; coherence outranks preference.

---

## After every edit

Run the base loop from `write-go` (`pnpm generate:go` when interfaces/proto changed →
`pnpm format:go` → `pnpm lint:go`), then:

1. **`write-go-tests`** — tests for every file touched; run `a-novel test --type=go -y` (or raw
   `go test` on the one package being iterated), the narrowest target that covers the change.
   Reserve the full `a-novel test -y` for the final commit.
2. **`document-code`** — doc comments for every symbol added or changed.
3. If you changed a REST handler in a way that alters the public API contract (new endpoint,
   changed parameters, changed response shape, added/removed status codes), invoke
   **`write-openapi`** to update `openapi.yaml`. The spec is a durable public commitment; letting
   it drift breaks clients.

---

## Project structure

```
cmd/                   # Main targets (one binary per subdirectory)
internal/
  config/              # Static configuration types + env-driven presets
  lib/                 # Minimal internal utilities (keep as small as possible — ideally empty)
  dao/                 # Data access layer (postgres, external sources)
  core/                # Business logic layer
  handlers/            # Transport layer (REST, gRPC)
  models/
    migrations/        # SQL migrations (up/down pairs)
    proto/             # Protobuf definitions
pkg/go/                # Exported Go client library (only if another service consumes this one)
```

**Layer import direction is one-way and strict:**

```
config   →  (no imports from internal layers)
lib      →  (no imports from internal layers)
dao      →  config, lib
core     →  config, lib, dao   (via interfaces only)
handlers →  config, lib, core   (via interfaces only)
cmd      →  all layers (wires everything together)
```

Handlers never import `dao` directly. Core never imports `handlers`. No circular imports.
Proto-generated types never enter the `core` or `dao` layers — handlers own all proto↔core
conversion.

---

## File names

`write-go` already mandates camelCase for multi-word file names. Within a service, the layer/role
prefix is fixed — match the existing files exactly:

| Layer / role       | Pattern                             | Example                                    |
| ------------------ | ----------------------------------- | ------------------------------------------ |
| DAO                | `pg.<entity>[<Operation>].go`       | `pg.user.go` (model), `pg.userSearch.go`   |
| DAO SQL            | `pg.<entity><Operation>.sql`        | `pg.userSearch.sql`                        |
| Core               | `<entity><Operation>.go`            | `userSearch.go`, `orderCreate.go`          |
| Handlers           | `<protocol>.<entity><Operation>.go` | `rest.userList.go`, `grpc.orderCreate.go`  |
| Config             | `<subject>.config.go`               | `app.config.go`, `users.config.go`         |
| Config defaults    | `<subject>.config.default.go`       | `app.config.default.go`                    |
| Lib                | `<subject>.go` (camelCase)          | `masterKeyContext.go`, `masterKeyCrypt.go` |
| Shared layer types | `common.go`                         | sentinels / types shared across the layer  |
| Tests              | `<same-name>_test.go`               | `pg.userSearch_test.go`                    |

**Handlers use `rest`, never `http`** — in file names _and_ type names. Files named `http.*` and
types prefixed `Http` are legacy; rename them when they come into scope (see Active Migrations).

---

## Type names

| Kind                                       | Pattern                                | Example                                   |
| ------------------------------------------ | -------------------------------------- | ----------------------------------------- |
| Operation interface + struct               | `<Entity><Operation>`                  | `UserSearch`, `OrderCreate`               |
| DAO dependency interface (in core)         | `<Entity><Operation>Dao`               | `UserSearchDao`, `JwkSelectDao`           |
| Service dependency interface (in handlers) | `<Protocol><Entity><Operation>Service` | `RestJwkGetService`, `GrpcJwkListService` |
| Service dependency interface (in core)     | `<Entity><Operation>Service<Role>`     | `JwkSearchServiceExtract`                 |
| Request struct                             | `<Entity><Operation>Request`           | `UserSearchRequest`                       |
| Config struct                              | Domain-named                           | `App`, `RestTimeouts`, `Database`         |
| DAO entity (bun model)                     | Entity name, singular                  | `User`, `Order`                           |
| Handler struct                             | `<Protocol><Entity><Operation>`        | `RestUserList`, `GrpcOrderCreate`         |

---

## The interface + implementation pattern

Every file in `dao/`, `core/`, and `handlers/` exports **exactly one struct implementation**;
the struct name mirrors the file name (camel-cased). The file also defines the **dependency
interfaces** that implementation needs — but not every layer has them:

| Layer       | Struct exports | Interface exports                                                                        |
| ----------- | -------------- | ---------------------------------------------------------------------------------------- |
| `dao/`      | one struct     | **none** — DAO files export no interface; the interface lives in the consuming core file |
| `core/`     | one struct     | one per dependency (the DAO interface + any sub-services)                                |
| `handlers/` | one struct     | one (the service it delegates to)                                                        |

```go
// File: core/userSearch.go

// UserSearchDao is the DAO interface this service depends on (defined here, not in dao/).
type UserSearchDao interface {
    Exec(ctx context.Context, request *dao.UserSearchRequest) ([]*dao.User, error)
}

// UserSearch searches for users matching the given criteria.
type UserSearch struct {
    dao UserSearchDao
}

// UserSearchRequest carries the inputs for [UserSearch.Exec].
type UserSearchRequest struct {
    Name string
}

func (s *UserSearch) Exec(ctx context.Context, request *UserSearchRequest) ([]*User, error) {
    // ...
}

func NewUserSearch(dao UserSearchDao) *UserSearch {
    return &UserSearch{dao: dao}
}
```

**Key rules:**

- One exported struct per file; its name mirrors the file name.
- **Interfaces proxy the imported package — always defined by the _consumer_, never the producer.**
  A service that imports `dao.PgUserSearch` does not use that concrete type directly; it declares a
  local `UserSearchDao` interface describing only the methods it needs. The DAO package has
  no interface of its own. Consumer owns the contract; producer satisfies it; neither layer forces
  the other to import it.
- **One method per interface, named `Exec`.** This is _not_ idiomatic Go (idiomatic Go names
  interface methods after what they do) — it is a deliberate project standard for consistency.
  Follow it.
- The method signature is `(ctx context.Context, request *XxxRequest) (Result, error)` — `Result`
  is a struct pointer, slice, or named type, never a bare primitive, so fields can be added later
  without breaking callers.
- **Exceptions in handlers**: gRPC method signatures are protoc-generated; REST handlers implement
  `ServeHTTP(w, r)`. Both are accepted exceptions to the `Exec` shape.
- Service and DAO types are **stateless after construction** — every field is set by `New*` and
  never mutated. This makes every type safe for concurrent use without synchronization; it is a
  requirement, not an accident.
- **Shared sentinels/types within a layer** go in that layer's single `common.go` (one per layer
  at most), never duplicated across files.

---

## DAO layer (`internal/dao/`)

Persist and retrieve data from PostgreSQL (and other external sources). No business logic, no
validation, no error translation beyond mapping database-level errors to domain sentinels.

```go
// pg.userSelect.go

//go:embed pg.userSelect.sql
var userSelectQuery string

// ErrUserSelectNotFound is returned when no user matches the requested ID.
var ErrUserSelectNotFound = errors.New("user not found")

// PgUserSelect retrieves a single active user by their ID.
type PgUserSelect struct{}

// UserSelectRequest holds the parameters for a [PgUserSelect.Exec] call.
type UserSelectRequest struct {
    ID uuid.UUID
}

func (r *PgUserSelect) Exec(ctx context.Context, request *UserSelectRequest) (*User, error) {
    ctx, span := otel.Tracer().Start(ctx, "dao.PgUserSelect")
    defer span.End()

    db, err := postgres.GetContext(ctx)
    if err != nil {
        return nil, otel.ReportError(span, fmt.Errorf("get postgres context: %w", err))
    }

    var user User
    if err = db.NewRaw(userSelectQuery, request.ID).Scan(ctx, &user); err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            err = errors.Join(err, ErrUserSelectNotFound)
        }
        return nil, otel.ReportError(span, fmt.Errorf("execute query: %w", err))
    }

    return otel.ReportSuccess(span, &user), nil
}

func NewPgUserSelect() *PgUserSelect { return &PgUserSelect{} }
```

- **SQL** lives in a companion `.sql` file embedded with `//go:embed` into a package-level variable
  — never an inline string literal. Use **`write-sql`** for all `.sql` work (parameterization,
  return patterns, formatting, the read-vs-write target rules).
- **Errors:** map a database error onto a domain sentinel by _joining_ it
  (`err = errors.Join(err, ErrXxxNotFound)`), then `otel.ReportError` it like any other failure —
  **including the not-found case**. A missing row is a real outcome the DAO encountered; whether
  it's benign is the caller's call (ultimately the handler's, by discarding it), not the DAO's.
- **Telemetry:** `otel.ReportError(span, err)` on every failure path; `otel.ReportSuccess(span,
value)` on the happy path. See the Telemetry section.
- **Entity types** (bun models) go in their own `pg.<entity>.go` file, separate from the operations
  that use them.

---

## Transaction scoping

`postgres.GetContext(ctx)` returns the current DB handle from the context — a `*bun.DB`
(auto-commit per statement) or a `bun.Tx` (open transaction). **DAOs never start transactions**;
they participate in whatever is already on the context.

**Who starts a transaction:**

- **Core** — when two or more DAO calls must succeed or fail together (one atomic unit of work).
- **`cmd/`** — for batch jobs (rotation, seeding) that should roll back wholesale on failure.
- **Handlers** — never. Transactions are a persistence concern, not a transport concern.

**Scope it right:** wrap exactly the statements that form one logical unit. Too small = splitting
an insert + its dependent update into two implicit transactions; too broad = spanning a tx across
an external HTTP/gRPC call, holding it open during long computation/file I/O, or wrapping two
unrelated operations together.

**Operations that cannot run inside a transaction** (e.g. `DB.Ping()` in a health handler) must
check the concrete type and bail out gracefully:

```go
pgdb, ok := pg.(*bun.DB)
if !ok {
    // Running inside a transaction (e.g. test isolation) — skip the ping.
    return nil
}
err = pgdb.Ping()
```

---

## Core layer (`internal/core/`)

Business logic, validation, orchestration of DAO calls. No HTTP concerns, no JSON marshalling, no
gRPC status codes — those live exclusively in handlers.

```go
func (s *UserSearch) Exec(ctx context.Context, request *UserSearchRequest) ([]*User, error) {
    ctx, span := otel.Tracer().Start(ctx, "core.UserSearch")
    defer span.End()

    err := validate.Struct(request)
    if err != nil {
        return nil, otel.ReportError(span, errors.Join(err, ErrInvalidRequest))
    }

    entities, err := s.dao.Exec(ctx, &dao.UserSearchRequest{Name: request.Name})
    if err != nil {
        return nil, otel.ReportError(span, fmt.Errorf("search users: %w", err))
    }
    // map dao entities → core models, then:
    return otel.ReportSuccess(span, results), nil
}
```

- **Validate inputs in the `Exec` body** — validation is a service responsibility, never a handler
  one. Use the project's validation library where the core layer already has one
  (`go-playground/validator` via a package-level `validate` instance, with a shared
  `ErrInvalidRequest` sentinel); otherwise plain conditional checks. Reject blank required strings,
  zero-value identifiers, out-of-range values, and values outside a known set.
- **DAO/sub-service sentinels travel up** — return them wrapped with `%w` so handlers can match
  with `errors.Is`. The service may re-export a DAO sentinel as its own (`core.ErrXxx`) when
  the handler needs to map it (see Common Pitfalls).
- Services reach DAOs **only** through the local interfaces they declare. Configuration is injected
  as a **concrete config struct** — config is static and never needs to be mocked.

---

## Handlers layer (`internal/handlers/`)

Translate transport requests into service calls and serialize the response. HTTP status codes, JSON
(un)marshalling, and gRPC status codes live here and _only_ here. Handler structs and span names
carry the protocol prefix; the service-dependency interface mirrors the handler type name.

### REST handlers

```go
// rest.userList.go

type RestUserListService interface {
    Exec(ctx context.Context, request *core.UserSearchRequest) ([]*core.User, error)
}

type RestUserList struct {
    service RestUserListService
    logger  logging.Log
}

func (h *RestUserList) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    ctx, span := otel.Tracer().Start(r.Context(), "rest.UserList")
    defer span.End()

    var request RestUserListRequest
    if err := muxDecoder.Decode(&request, r.URL.Query()); err != nil {
        httpf.HandleError(ctx, h.logger, w, span, httpf.ErrMap{nil: http.StatusBadRequest}, err)
        return
    }

    users, err := h.service.Exec(ctx, &core.UserSearchRequest{Name: request.Name})
    if err != nil {
        httpf.HandleError(ctx, h.logger, w, span, httpf.ErrMap{
            core.ErrUserNotFound:   http.StatusNotFound,
            core.ErrInvalidRequest: http.StatusUnprocessableEntity,
        }, err)
        return
    }

    httpf.SendJSON(ctx, w, span, lo.Map(users, loadUserMap))
}

func NewRestUserList(service RestUserListService, logger logging.Log) *RestUserList {
    return &RestUserList{service: service, logger: logger}
}
```

- **REST is public-facing.** Never leak internal error detail. Map each expected sentinel to a
  status with the project's error-mapping helper (`httpf.HandleError` + `httpf.ErrMap`); the
  helper's fallback covers unmapped errors as 500. `httpf.HandleError` already calls
  `otel.ReportError` on the error for you (the handler span records which error the request ended
  on, whatever status it maps to) — so you do **not** add a separate `otel.ReportError` on a path
  that goes through `httpf.HandleError`. On the gRPC side, where you do the mapping by hand, call
  `_ = otel.ReportError(span, err)` yourself before `status.Error(...)` to get the same effect.
- Conventional short names `w` / `r`. JSON in via `json.NewDecoder(r.Body)` or `gorilla/schema` for
  query params; out via the project's `httpf.SendJSON`.
- Handler type names carry the `Rest` prefix (`RestUserList`); the service interface mirrors it
  (`RestUserListService`). File: `rest.<entity><Operation>.go`. Rename legacy `http.*` when touched.

### gRPC handlers

```go
// grpc.orderCreate.go

type GrpcOrderCreateService interface {
    Exec(ctx context.Context, request *core.OrderCreateRequest) (*core.Order, error)
}

type GrpcOrderCreate struct {
    protogen.UnimplementedOrderCreateServiceServer
    service GrpcOrderCreateService
}

func (h *GrpcOrderCreate) OrderCreate(
    ctx context.Context, req *protogen.OrderCreateRequest,
) (*protogen.OrderCreateResponse, error) {
    ctx, span := otel.Tracer().Start(ctx, "grpc.OrderCreate")
    defer span.End()

    result, err := h.service.Exec(ctx, &core.OrderCreateRequest{UserID: req.GetUserId()})
    if errors.Is(err, core.ErrUserNotFound) {
        _ = otel.ReportError(span, err)
        return nil, status.Error(codes.NotFound, "create order: user not found")
    }
    if err != nil {
        _ = otel.ReportError(span, err)
        return nil, status.Error(codes.Internal, "create order: internal error")
    }

    return otel.ReportSuccess(span, &protogen.OrderCreateResponse{OrderId: result.ID.String()}), nil
}

func NewGrpcOrderCreate(service GrpcOrderCreateService) *GrpcOrderCreate {
    return &GrpcOrderCreate{service: service}
}
```

- **gRPC is internal** (service-to-service only) — never exposed to the internet. Embed
  `protogen.Unimplemented<ServiceName>Server`.
- Map core sentinels to `codes.*` via `errors.Is` + `status.Error`. Inside `status.Error` /
  `status.Errorf` use `%v`, never `%w` — gRPC status errors don't support `errors.Unwrap`, so `%w`
  is misleading. Keep status _messages_ generic enough not to leak DB/crypto detail.
- Convert proto↔core models **in the handler**. Never pass a proto type into the core layer.
- Handler type names carry the `Grpc` prefix; the service interface mirrors it. File:
  `grpc.<entity><Operation>.go`.

---

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

Only tools genuinely unavailable from dependencies or stdlib. Keep it **as small as possible —
ideally nonexistent**. Before adding anything: check stdlib/deps first, then ask the developer
whether it belongs here at all (vs. inside the relevant package). During maintenance, actively look
for `lib/` code a newer upstream now subsumes, and delete it.

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
provide a `NewClient()` that encapsulates connection setup and returns a clean interface; leak no
internal implementation detail through the public API.

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

---

## Telemetry (OpenTelemetry)

Every DAO and service operation is wrapped in a span. Three `golib/otel` helpers cover every return
path:

- `otel.ReportError(span, err)` — `RecordError` + `SetStatus(Error)`, returns `err` so it inlines in
  a `return`. Does **not** end the span — a `defer span.End()` does.
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
  calls `otel.ReportError` unconditionally before writing the HTTP status, so the REST handler
  span records the error whatever status it maps to; on the gRPC side, do the same by hand
  (`_ = otel.ReportError(span, err)` before `status.Error(...)`). The handler span thus shows
  which error each request ended on — useful, not noise. The handler is the _boundary_, not a
  layer that makes the error vanish.
- **Anti-pattern**: a `reportUnexpected(span, err)` keyed on a list of "known" sentinels, used at
  _any_ layer that still propagates or surfaces the error. It couples the layer to an error
  registry and drops real signal. The forbidden moves are: suppressing reporting based on the
  error's identity, and `return nil, ErrXxx` bare from a layer that has a span. Every span'd layer
  reports.

The "is the service broken" view is built on the **HTTP status code** (the otel HTTP
instrumentation records it) — not on span status. Span status answers "did an error occur in
processing", which is `true` even for a deliberate 404, and that is fine: spans are independent,
so the DAO span says "no row", the core span says "no row", the handler span says "no row → 404",
and the dashboard counts the 404 as a 404, not as a 500. For bulk-anomaly visibility on a specific
security sentinel (a spike of `ErrInvalidSignature`), use a counter / audit log / dedicated event
— not `span.status`.

**Span attributes** use a semantic `<entity>.<field>` scheme describing the _data_, not the Go
variable holding it: `"key.id"`, `"key.usage"`, `"key.expires_at"` — never `"request.Jwk.*"` or
`"request.Usage"`. Add request attributes with `span.SetAttributes(attribute.String(...))` for
debuggability. Do **not** add spans to constructors or config loading — only to operations that do
real work.

---

## Security

`write-go` covers secrets hygiene (never log/trace/return key material; constant-time secret
comparison; equalize auth-miss timing) and the "use established crypto, never roll your own"
rule — all of which apply here. The service-specific additions:

- **Validate at the core layer.** Handlers reject only _structurally_ malformed input
  (unparseable UUID); inputs that parse but are semantically wrong (blank required string,
  out-of-range value, value outside a known set) are rejected in the core layer with `ErrInvalidRequest`.
  Bound every length that feeds storage or a downstream system.
- **No string-built SQL — ever.** Every DAO query is `//go:embed`-ed from a `.sql` file and run
  through bun's parameterized API (`NewRaw(query, args...)`). The embed pattern keeps SQL visible
  and reviewable; do not move SQL into Go string literals to sidestep it. (See `write-sql`.)
- **No error-information disclosure in REST responses.** Map to a generic status text
  (`http.StatusText(...)`); the helper does this for you. This covers **structured bodies** too —
  a health/status JSON body must carry a binary state (`"up"` / `"down"`) and at most a stable
  error _code_, never `err.Error()`: a wrapped DB error routinely contains hostnames, ports,
  schema names, and one `err.Error()` on a downed dependency leaks topology to an unauthenticated
  caller. Log/trace the full error; return only the public shape. Richer detail goes on a separate,
  auth-gated endpoint. gRPC (internal) tolerates slightly more context in status messages but still
  no raw DB/crypto detail.
- **Transport boundaries.** REST is public — anything in a REST response may reach an end user or
  be logged by an intermediary; return only what the client is entitled to. gRPC is internal —
  route all external traffic through REST, never expose gRPC directly.

---

## Tests — layer-specific patterns

`write-go-tests` covers the common shape (table-driven, `t.Parallel()`, mockery `.EXPECT()`,
`require`, the `_test` package, cross-package fixture subpackages). These are the
service-architecture additions.

### DAO tests — real Postgres, rolled-back transaction

DAO tests run against a real PostgreSQL database inside an isolated transaction that is rolled back
after each sub-test, so cases can't interfere. No mocks — the point is to exercise the real DB.

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
- A test that verifies filtering/ordering must include enough fixtures to make the assertion
  meaningful (a `"FilterUsage"` case needs at least one matching row and one non-matching row).
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
- Decode the expected response into `any` (not a typed struct) — avoids import coupling and matches
  JSON numbers as `float64`. For empty list results use `[]any{}` (not `nil`): Go encodes a nil
  slice as `null` and a non-nil empty slice as `[]`, distinct API contracts.
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

`pkg/go` tests connect to a running service stack and exercise the exported client API. Run with
`a-novel test --type=go -y`, not `a-novel test --type=go -y`. Don't mock anything at this layer — the point is the real
integration path.

### Common test pitfalls (service-specific)

- **Mocking the database in DAO tests.** DAO tests always use the real DB via
  `postgres.RunIsolatedTransactionalTest`. Mocks belong in core and handler tests.
- **Using DAO sentinels in handler tests.** Handler tests must not import `dao`. The service mock
  in a handler test returns the _core-layer_ sentinel (`core.ErrJwkNotFound`), not the DAO
  one (`dao.ErrJwkSelectNotFound`) — that mirrors what the real service returns after translation
  and keeps the test honest about the handler's actual contract.

---

## Active migrations

Apply these when a file is already in scope — never speculatively:

| Legacy pattern                                   | Current standard                                                                 |
| ------------------------------------------------ | -------------------------------------------------------------------------------- |
| Handler files named `http.*`                     | Rename to `rest.*`                                                               |
| Type names with `Http` prefix                    | Rename to `Rest` prefix                                                          |
| Handler span name `"handler.X"`                  | `"rest.X"` / `"grpc.X"` (strip the type's protocol prefix)                       |
| Test-only preset in a production `config` file   | Move to `internal/config/configtest/`                                            |
| `.test.go` file carrying test-only globals       | Move to a `*test` subpackage / `_test.go`                                        |
| `new(T)` in a constructor                        | `&T{}`                                                                           |
| `current := item` copy inside a `for range` loop | Delete (Go 1.22+ per-iteration vars)                                             |
| `internal/services/` layer + `*Repository` deps  | `internal/core/` (`package core`, `coremocks`), `*Dao` interfaces + `dao` fields |

> **`service-narrative-engine` is a known divergence.** It went through the `services`→`core` /
> `repository`→`dao` rename in a separate session and may still carry the pre-rename naming. Do
> **not** migrate it piecemeal as a side effect of another change — leave it until its own session
> catches up.

---

## Common pitfalls

(`write-go` lists the language-level ones — context-in-struct, new deps without asking, logging
secrets, multiple `time.Now()`, bare `return nil, ErrXxx` from a layer with a span, `new(T)`,
discarding errors. These are the **service-architecture** ones:)

- **HTTP/gRPC concerns leaking into the core layer.** `http.Error`, `json.Marshal`, `status.Errorf`
  in a core file → move it to the handler.
- **`dao` imported in handlers.** Handlers depend on core components via interfaces; if a handler
  needs a DAO sentinel, the core re-exports it as `core.ErrXxx` (in `core/common.go` if shared)
  and the handler checks that. Keeps the handler decoupled from the DAO layer.
- **Inline SQL.** All SQL lives in `.sql` files embedded with `//go:embed` at package level; all
  queries are parameterized via bun — no `fmt.Sprintf`.
- **Hand-written mocks.** Mocks are `mockery`-generated from the interfaces in each file; run
  `pnpm generate:go` after adding/changing an interface.
- **`http` in new file or type names.** Use `rest` everywhere; rename legacy `http.*` / `Http*`
  when in scope.
- **Returning raw error detail in a REST response** — including in a structured JSON field. Map to a
  generic status; carry at most a stable error code.
- **Validation in handlers instead of the core layer.** Business-rule checks belong in the core
  layer; handlers reject only structurally invalid input.
- **A span on a constructor or config loader.** Spans go on operations that do real work.
- **A transaction started in a handler.** Transactions are a persistence concern — the core layer
  or `cmd/` start them, never handlers.
