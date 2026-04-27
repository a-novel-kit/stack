---
name: write-go-code
description: >
  Write, review, and refactor Go code for Agora backend services. Use this skill whenever writing or
  modifying Go source files — new features, bug fixes, refactors, new layers, new services, or new
  handlers. Applies to all internal layers (dao, services, handlers, config, lib), models (SQL, proto),
  exported packages (pkg/go), and command entrypoints (cmd/).
---

# Go Code Writing Skill

This skill governs how to write Go code in Agora backend services. The goal is coherent, idiomatic,
minimal, and maintainable code that strictly respects the project's clean architecture.

**Before touching any file**, read it. **Before touching any layer**, understand the layer's full
context: adjacent files, the interfaces it depends on, the interfaces it exposes. Always look for
existing patterns and follow them strictly — coherence across the codebase takes precedence over
personal preference.

---

## After Every Edit

Run these in order after any change to Go files:

```
make generate   # when interfaces or .proto files changed; runs go generate, wipes protogen/, regenerates mocks and proto stubs
make format     # always
make lint       # always
```

Never skip `format` or `lint`. If `lint` reports an issue you did not introduce, fix it anyway
while in scope — leave the code cleaner than you found it.

Then, in order:

1. Invoke the **`write-go-tests` skill** to write or update tests for every file you created or
   modified. Run the appropriate test target (`make test-unit` or `make test-pkg`) and confirm
   all tests pass before moving on. Tests are not optional — they are part of the same change.

2. Invoke the **`document-code` skill** to write or update documentation for every file you
   created or modified. Documentation is not optional — it is part of the same change.

3. If you modified a REST handler in a way that changes the public API contract (new endpoint,
   changed parameters, changed response shape, added or removed status codes), invoke the
   **`write-openapi` skill** to update `openapi.yaml` to match. The spec is a durable public
   commitment — letting it drift breaks clients and documentation consumers.

---

## Dependencies

**Stdlib first.** If the standard library covers a need, use it — never reach for a package when
`net/http`, `encoding/json`, `context`, `fmt`, `errors`, `time`, or `crypto/*` already does the job.

**External packages: be conservative.** When stdlib isn't enough:

- Audit the package's GitHub repository: activity, open issues, test coverage, maintenance signals.
- Prefer packages already present in `go.mod`. Reuse before adding.
- Keep the total number of external dependencies as small as possible.
- **Always ask the developer before introducing a package that is not already in `go.mod`.**
  This is a firm rule — no exceptions, even for small utilities.

**Remove, don't accumulate.** During maintenance, actively look for packages that can be removed.
If existing code in `lib/` duplicates something a dependency now provides, remove the duplicate.

**Look it up online.** Before writing or changing code that uses an external package or stdlib API,
search for the official documentation, release notes, and real-world usage examples. Prefer official
`pkg.go.dev` docs, the package's GitHub README, and high-quality blog posts or Stack Overflow threads.
This applies to both new and familiar packages — APIs evolve, better patterns emerge, and the first
approach that comes to mind may not be idiomatic. Looking up the docs takes seconds and routinely
prevents subtle misuse. Do this before touching the code, not after.

---

## Project Structure

```
cmd/                   # Main entrypoints (one binary per subdirectory)
internal/
  config/              # Static configuration types + env-driven presets
  lib/                 # Minimal internal utilities (keep as small as possible)
  dao/                 # Data access layer (postgres, external sources)
  services/            # Business logic layer
  handlers/            # Transport layer (REST, gRPC)
  models/
    migrations/        # SQL migrations (up/down pairs)
    proto/             # Protobuf definitions
pkg/go/                # Exported Go client library (optional — only if sharing logic externally)
```

**Layer import direction is one-way and strict:**

```
config  →  (no imports from internal layers)
lib     →  (no imports from internal layers)
dao     →  config, lib
services → config, lib, dao (via interfaces only)
handlers → config, lib, services (via interfaces only)
cmd     → all layers (wires everything together)
```

Handlers never import `dao` directly. Services never import `handlers`. No circular imports.

---

## Package Naming

Each directory is a separate Go package. Package names are **short, lowercase, single words** —
no underscores, no camelCase: `package dao`, `package services`, `package handlers`, `package config`.
The package name is what callers write in imports; keep it unambiguous and collision-free.

---

## File Naming

File names encode both the layer context and what they do. Always look at existing files first and
follow the established pattern for that service exactly.

| Layer              | Pattern                             | Example                                   |
| ------------------ | ----------------------------------- | ----------------------------------------- |
| DAO                | `pg.<entity>.<operation>.go`        | `pg.user.go`, `pg.userSearch.go`          |
| DAO SQL            | `pg.<entity>.<operation>.sql`       | `pg.userSearch.sql`                       |
| Services           | `<entity><Operation>.go`            | `userSearch.go`, `orderCreate.go`         |
| Handlers           | `<protocol>.<entity><Operation>.go` | `rest.userList.go`, `grpc.orderCreate.go` |
| Config             | `<subject>.config.go`               | `app.config.go`, `users.config.go`        |
| Config defaults    | `<subject>.config.default.go`       | `app.config.default.go`                   |
| Shared layer types | `common.go`                         | errors and types shared across a layer    |
| Tests              | `<same_name>_test.go`               | `pg.userSearch_test.go`                   |

**Protocol naming in handlers:** always use `rest` (not `http`). This is the canonical spelling
in both file names and code. Existing files using `http` are legacy — rename them when touched.

---

## The Interface+Implementation Pattern

Every file in `dao/`, `services/`, and `handlers/` exports **exactly one struct implementation**.
The struct name matches the file name (camel-cased). The file also defines the **dependency
interfaces** that implementation requires — but not all layers need them:

| Layer       | Struct exports | Interface exports                                                                           |
| ----------- | -------------- | ------------------------------------------------------------------------------------------- |
| `dao/`      | one struct     | **none** — DAO files export no interface; the interface lives in the consuming service file |
| `services/` | one struct     | one per dependency (DAO repo + any sub-services)                                            |
| `handlers/` | one struct     | one (the service it delegates to)                                                           |

```go
// File: services/userSearch.go

// UserSearchRepository is the DAO interface this service depends on (defined here, not in dao/).
type UserSearchRepository interface {
    Exec(ctx context.Context, request *dao.UserSearchRequest) ([]*dao.User, error)
}

// UserSearch searches for users matching the given criteria.
type UserSearch struct {
    repository UserSearchRepository
}

// UserSearchRequest carries the inputs for [UserSearch.Exec].
type UserSearchRequest struct {
    Name string
}

func (s *UserSearch) Exec(ctx context.Context, request *UserSearchRequest) ([]*User, error) {
    // ...
}

func NewUserSearch(repository UserSearchRepository) *UserSearch {
    return &UserSearch{repository: repository}
}
```

**Key rules:**

- One exported struct per file. The struct name mirrors the file name.
- **Interfaces proxy the imported package — they are always defined by the consumer, never the
  producer.** A service that imports `dao.PgUserSearch` does not use that concrete type directly;
  instead it declares a local `UserSearchRepository` interface that describes only the methods it
  needs. The DAO package has no interface of its own. This is Go's dependency inversion in
  practice: the consumer owns the contract, the producer satisfies it, and neither layer forces
  the other to import it.
- Each interface exposes **one method only**. By project convention, that method is named `Exec`.
  This is **not** standard Go — idiomatic Go names interface methods after what they do
  (`Searcher.Search`, `Creator.Create`). `Exec` is a deliberate project standard for consistency;
  follow it inside Agora services.
- The method always takes `(ctx context.Context, request *XxxRequest)` and returns `(Result, error)`.
  `Result` is a struct pointer, slice, or named type — not a bare primitive. This contract exists
  so fields can be added later without breaking callers.
- **Exceptions** in handlers: gRPC method signatures are generated by protoc and cannot follow
  the `Exec` pattern. REST handlers implement `ServeHTTP(w, r)`. Both are acceptable exceptions.
- Service and DAO types are **stateless after construction** — all fields are set by `New*` and
  never mutated. This is a design requirement, not an accident: it makes every type safe for
  concurrent use without synchronization.

**Shared errors and types within a layer** go in `common.go`. When a sentinel error or type is
produced or used by multiple files in the same layer, define it in `common.go` rather than
duplicating it across files. Each layer may have at most one `common.go`.

---

## DAO Layer (`internal/dao/`)

Responsibilities: persist and retrieve data from PostgreSQL (and other external sources). No business
logic, no validation, no error translation beyond mapping database-level errors to domain sentinel
errors.

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
            return nil, ErrUserSelectNotFound
        }
        return nil, otel.ReportError(span, fmt.Errorf("execute query: %w", err))
    }

    return otel.ReportSuccess(span, &user), nil
}

func NewPgUserSelect() *PgUserSelect {
    return &PgUserSelect{}
}
```

- **SQL:** embed in a companion `.sql` file with `//go:embed`. The embedded variable must be
  package-level (not inside a function). Never inline SQL as a raw string. Use the
  **`write-sql` skill** for all `.sql` file work — parameterization syntax, return patterns,
  formatting, and the read-vs-write target rules (`active_keys` vs `keys`).
- **Errors:** map database sentinel errors (e.g., `sql.ErrNoRows`) to domain sentinel errors.
  Do not call `otel.ReportError` on sentinel errors you intentionally surface — those are
  expected conditions, not failures.
- **Telemetry:** use `otel.ReportError(span, err)` on failure paths and `otel.ReportSuccess(span, value)`
  on the happy path. See the Telemetry section for the full pattern.
- **Entity types:** define the bun model struct in a dedicated `pg.<entity>.go` file, separate
  from the operations that use it.

---

## Transaction Scoping

`postgres.GetContext(ctx)` returns the current database handle from the context — either a `*bun.DB`
(plain connection, auto-commit per statement) or a `bun.Tx` (open transaction). DAOs never start
transactions themselves; they participate in whatever is already in the context.

**Where to start a transaction:**

- **Services**: when two or more DAO calls must succeed or fail together (atomic unit of work).
- **`cmd/`**: for batch operations (e.g., key rotation, seeding) where the whole job should be
  rolled back on failure.
- **Handlers**: never. Transactions are a persistence concern, not a transport concern.

**Scoping rules:**

| Too small                                                                                                                                       | Too broad                                                         |
| ----------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| Each individual SQL query in its own implicit transaction when they form a logical unit (e.g., insert + cascade update that must be consistent) | Spanning a transaction across an external HTTP or gRPC call       |
|                                                                                                                                                 | Wrapping two unrelated service operations in a single transaction |
|                                                                                                                                                 | Holding a transaction open during long computation or file I/O    |

**Detecting transaction context:**

Operations that cannot run inside a transaction (e.g., `DB.Ping()` in health handlers) must check
the concrete type before proceeding:

```go
pgdb, ok := pg.(*bun.DB)
if !ok {
    // Running inside a transaction (e.g., test isolation) — skip the ping.
    return nil
}
err = pgdb.Ping()
```

This pattern appears in health/status handlers to handle the case where the test harness injects a
transaction via `postgres.RunIsolatedTransactionalTest`.

---

## Services Layer (`internal/services/`)

Responsibilities: business logic, validation, orchestration of DAO calls. No HTTP concerns,
no JSON marshalling, no gRPC status codes — those belong exclusively in handlers.

```go
// services/userSearch.go

// UserSearchRepository is the DAO interface this service depends on.
type UserSearchRepository interface {
    Exec(ctx context.Context, request *dao.UserSearchRequest) ([]*dao.User, error)
}

type UserSearch struct {
    repository UserSearchRepository
}

// UserSearchRequest carries the inputs for [UserSearch.Exec].
type UserSearchRequest struct {
    Name string
}

func (s *UserSearch) Exec(ctx context.Context, request *UserSearchRequest) ([]*User, error) {
    ctx, span := otel.Tracer().Start(ctx, "services.UserSearch")
    defer span.End()

    if request.Name == "" {
        return nil, otel.ReportError(span, fmt.Errorf("search users: name is required"))
    }

    entities, err := s.repository.Exec(ctx, &dao.UserSearchRequest{Name: request.Name})
    if err != nil {
        return nil, otel.ReportError(span, fmt.Errorf("search users: %w", err))
    }

    // transform and return...
}
```

- **Validate inputs explicitly** in the `Exec` body. Validation is a service responsibility, not
  a handler responsibility. Use plain conditional checks — not struct tags or external validators —
  unless the project has already introduced a validation library for this.
- Sentinel errors from DAO travel up — wrap them with `%w` to preserve identity so handlers can
  match with `errors.Is`.
- Services interact with DAO only through the interfaces they define locally.
- When a service depends on configuration, accept it as a concrete config struct — config is
  static and does not need to be mocked.

---

## Handlers Layer (`internal/handlers/`)

Responsibilities: translate transport-layer requests into service calls and serialize responses.
HTTP error codes, JSON marshalling/unmarshalling, and gRPC status codes live here and only here.

### REST Handlers

```go
// rest.userList.go

type RestUserListService interface {
    Exec(ctx context.Context, request *services.UserSearchRequest) ([]*services.User, error)
}

type RestUserList struct {
    service RestUserListService
}

func (h *RestUserList) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    name := r.URL.Query().Get("name")

    users, err := h.service.Exec(r.Context(), &services.UserSearchRequest{Name: name})
    if err != nil {
        if errors.Is(err, services.ErrUserNotFound) {
            http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
            return
        }
        http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    if err = json.NewEncoder(w).Encode(users); err != nil {
        http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
    }
}

func NewRestUserList(service RestUserListService) *RestUserList {
    return &RestUserList{service: service}
}
```

- **REST handlers are public-facing.** Never leak internal error details in responses. Map sentinel
  errors to appropriate HTTP status codes (404, 422, 500, etc.) with explicit `errors.Is` checks.
  Always have a final fallback for unmapped errors.
- Use `w http.ResponseWriter` and `r *http.Request` as the conventional short names.
- Use `json.NewEncoder(w).Encode(...)` for JSON responses; set `Content-Type` header first.
- Handler type names include the protocol prefix: `RestUserList`, not `UserList`. The service
  interface mirrors the handler type name: `RestUserList` → `RestUserListService`.
- **File naming:** `rest.<entity><Operation>.go`. Rename any legacy `http.*` files when touched.

### gRPC Handlers

```go
// grpc.orderCreate.go

type GrpcOrderCreateService interface {
    Exec(ctx context.Context, request *services.OrderCreateRequest) (*services.Order, error)
}

type GrpcOrderCreate struct {
    protogen.UnimplementedOrderCreateServiceServer
    service GrpcOrderCreateService
}

func (h *GrpcOrderCreate) OrderCreate(ctx context.Context, req *protogen.OrderCreateRequest) (*protogen.OrderCreateResponse, error) {
    ctx, span := otel.Tracer().Start(ctx, "grpc.OrderCreate")
    defer span.End()

    result, err := h.service.Exec(ctx, &services.OrderCreateRequest{
        UserID: req.GetUserId(),
    })
    if err != nil {
        if errors.Is(err, services.ErrUserNotFound) {
            return nil, status.Errorf(codes.NotFound, "create order: %v", err)
        }
        _ = otel.ReportError(span, err)
        return nil, status.Errorf(codes.Internal, "create order: %v", err)
    }

    return otel.ReportSuccess(span, &protogen.OrderCreateResponse{OrderId: result.ID.String()}), nil
}

func NewGrpcOrderCreate(service GrpcOrderCreateService) *GrpcOrderCreate {
    return &GrpcOrderCreate{service: service}
}
```

- **gRPC handlers are internal** (service-to-service only). Never expose gRPC publicly.
- Embed `protogen.Unimplemented<ServiceName>Server` in the handler struct.
- Map service errors to `codes.*` using `status.Errorf`. Use `%v` (not `%w`) inside
  `status.Errorf` — gRPC status errors do not support `errors.Unwrap`, so `%w` has no effect
  and would be misleading. Use `errors.Is` before constructing the status to pick the right code.
- Convert between service types and proto types in the handler. Never pass proto types into
  the service layer.
- Handler type names include the protocol prefix: `GrpcOrderCreate`, not `OrderCreate`. The service
  interface mirrors the handler type name: `GrpcOrderCreate` → `GrpcOrderCreateService`.

---

## Context Rules

- **Never store `context.Context` in a struct.** Pass it as the first argument to every method
  that needs it. Contexts are request-scoped and must not outlive the call.
- **Contexts flow downward only** — from caller to callee. Never return a context from a function.
- Use context values only for request-scoped data that crosses API boundaries (e.g., database
  transactions, trace spans, auth tokens). Do not use context as a way to pass optional arguments.

---

## Config Layer (`internal/config/`)

Responsibilities: define configuration structs and load them from environment variables or YAML.
No logic beyond parsing and defaulting.

- **Static configuration** (feature flags, algorithm choices, topology): YAML files.
- **Dynamic configuration with defaults** (ports, timeouts, connection strings): environment
  variables loaded via the `env/` helper package.
- Group related config into named structs. Nest for sub-domains (`App.Rest`, `App.Grpc`, `App.Main`).
- Provide a `*Default()` function (in a `*.config.default.go` file) that assembles the full config
  tree from env vars and YAML. This is what `cmd/` calls at startup.
- Env var names follow: `<SERVICE_ENV_PREFIX>_<FIELD_NAME>` (screaming snake case).

---

## Lib Layer (`internal/lib/`)

Contains only tools that are genuinely unavailable from dependencies. Keep it as small as
possible — ideally nonexistent. Before adding anything here:

1. Check if an existing dependency or stdlib already covers it.
2. Ask the developer if it truly belongs here vs. inside the relevant package.

During maintenance, actively look for opportunities to delete from `lib/` by replacing with upstream.

---

## Models (`internal/models/`)

### SQL Migrations (`models/migrations/`)

Use the **`write-sql` skill** for all migration work. Key rules that span both skills:

- Always create paired `up.sql` and `down.sql` files.
- Name with a second-precise timestamp: run `date '+%Y%m%d%H%M%S'` before creating the files.
- Never modify a `.up.sql` that has been merged to `master` — create a new migration instead.
- Down migrations and up migrations only on the current branch may be freely edited.

### Protobuf (`models/proto/`)

Use the **`write-proto` skill** for all `.proto` work — naming conventions, buf toolchain,
breaking-change rules, well-known types, and the step-by-step guide for adding a new RPC.
The one rule that spans both skills: never import proto-generated types into services or DAO
layers. Handlers own all conversions between proto types and service types.

---

## pkg/go (Exported Go Client)

Optional. Only create when another service explicitly needs to consume this service's logic as a
library. When it exists:

- Export only what external consumers actually need. Minimal surface area.
- Follow all the same naming and interface rules as `internal/`.
- Provide a `NewClient()` constructor that encapsulates connection setup and returns a clean interface.
- No internal implementation details should leak through the public API.

---

## cmd/ (Entrypoints)

Each `cmd/<name>/main.go` wires the full dependency graph and starts a server or runs a job.

```
1. Load config (from env/yaml)
2. Initialize observability (OpenTelemetry)
3. Set up shared contexts (e.g., database connection, secrets)
4. Construct DAO objects
5. Construct service objects (inject DAOs via interfaces)
6. Construct handler objects (inject services via interfaces)
7. Register routes / gRPC services
8. Start server with graceful shutdown on SIGINT/SIGTERM
```

- Dependency injection is **explicit and manual** — no framework, no reflection. Wire everything
  in `main()` with clear constructor calls.
- Graceful shutdown uses `signal.Notify` with a channel — block on the channel, then stop the
  server (e.g., `server.GracefulStop()` for gRPC, `httpServer.Shutdown(ctx)` for HTTP). Do not
  call `os.Exit` directly.

```go
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit
server.GracefulStop()
```

---

## Naming Conventions

### Types

| Kind                                       | Pattern                                | Example                                       |
| ------------------------------------------ | -------------------------------------- | --------------------------------------------- |
| Operation interface + struct               | `<Entity><Operation>`                  | `UserSearch`, `OrderCreate`                   |
| DAO dependency interface (in services)     | `<Entity><Operation>Repository`        | `UserSearchRepository`, `JwkSelectRepository` |
| Service dependency interface (in handlers) | `<Protocol><Entity><Operation>Service` | `RestJwkGetService`, `GrpcJwkListService`     |
| Service dependency interface (in services) | `<Entity><Operation>Service<Role>`     | `JwkSearchServiceExtract`                     |
| Request struct                             | `<Entity><Operation>Request`           | `UserSearchRequest`                           |
| Config struct                              | Domain-named                           | `App`, `RestTimeouts`, `Database`             |
| DAO entity (bun model)                     | Entity name, singular                  | `User`, `Order`                               |
| Proto-generated                            | As generated by protoc                 | —                                             |

### Variables and Fields

- **Be explicit**: `userRepository` over `repo`, `orderCreateService` over `svc`.
- **Short names are fine** for well-understood conventional roles: `w` and `r` for HTTP,
  `ctx` for context, `err` for errors, `i`/`k`/`v` in range loops.
- **Acronyms follow ecosystem casing**: `ID`, `URL`, `JSON`, `JWT`, `HTTP`, `REST`, `gRPC`.
  An acronym that starts an unexported name uses all-lowercase: `id`, `url`, `grpc`.
- Avoid shadowing imported package names (`json`, `http`, `context`, etc.). Rename the variable.

### Constructors

Always `New<TypeName>(dep1, dep2, ...) *<TypeName>`. Never return an interface from a constructor
unless the concrete type is truly an implementation detail hidden by design.

---

## Error Handling

- **Error strings are lowercase and do not end with punctuation.** Errors are often wrapped and
  appear mid-sentence; `errors.New("user not found")` not `errors.New("User not found.")`.
- **Sentinel errors** for expected domain conditions: `var ErrUserNotFound = errors.New("user not found")`.
  Define them in the same file as the type that produces them, or in `common.go` if shared.
- **Wrap errors with context**: `fmt.Errorf("search users: %w", err)`. The message chain should
  let a developer trace the call path by reading it alone.
- **Never discard errors.** If an error truly can be ignored, explain why with a comment.
- **Preserve identity**: always use `%w` (not `%v`) when wrapping, so callers can use `errors.Is`.
- **Layer boundaries**: handlers catch sentinel errors with `errors.Is` and translate to HTTP
  status codes or gRPC codes. Services never produce HTTP/gRPC errors.

---

## Telemetry (OpenTelemetry)

Every DAO and service operation must be wrapped in an OTEL span. Two project helpers cover the
two outcomes — use both so every return path is instrumented:

- `otel.ReportError(span, err)` — records the error on the span, sets the error status, and
  returns the error so it can be inlined in a `return` statement.
- `otel.ReportSuccess(span, value)` — marks the span as successful and returns the value.
- `otel.ReportSuccessNoContent(span)` — marks the span as successful for void operations (no
  return value). Used in handlers and jobs where there is nothing to return on success.

```go
ctx, span := otel.Tracer().Start(ctx, "dao.UserSelect")
defer span.End()

var user User
if err = db.NewRaw(query, id).Scan(ctx, &user); err != nil {
    return nil, otel.ReportError(span, fmt.Errorf("execute query: %w", err))
}

return otel.ReportSuccess(span, &user), nil
```

- Span names follow `"<layer>.<identifier>"` where the identifier depends on the layer:
  - **Handlers**: strip the protocol prefix from the type name, since the span prefix already encodes it.
    `"rest.JwkGet"` for `RestJwkGet`, `"grpc.ClaimsSign"` for `GrpcClaimsSign`. Never double the prefix:
    `"rest.RestJwkGet"` or `"grpc.GrpcStatus"` are wrong.
  - **DAO**: keep the storage prefix — it is the technology qualifier within the layer, not a redundant repeat.
    `"dao.PgJwkSearch"` for `PgJwkSearch`.
  - **Services**: no prefix to strip. `"services.JwkSearch"` for `JwkSearch`.
  - **Sub-spans** (private methods): append in parentheses: `"rest.JwkGet(parseID)"`, `"grpc.Status(reportPostgres)"`.
- Do **not** call `otel.ReportError` on sentinel errors returned as expected conditions
  (e.g., not-found results). Only report genuine failures.
- **Span attribute naming**: use a `<entity>.<field>` scheme that describes the data, not the Go
  variable that holds it. For attributes describing a key: `"key.id"`, `"key.usage"`,
  `"key.created_at"`, `"key.expires_at"`, `"key.private"`. Never use `"request.Jwk.*"` or
  `"request.Usage"` — those are implementation-level names, not semantic ones. The attribute name
  should be meaningful to someone reading the trace without access to the source code.
- Add relevant request attributes with `span.SetAttributes(attribute.String(...))` for debuggability.
- Do not add spans to constructors or config loading — only to operations that do real work.

---

## Security

Security is not optional. Every piece of code that crosses a trust boundary must be written with
an attacker in mind. The rules below apply to all layers, with layer-specific notes where relevant.

### Input validation

Validate all inputs at the service layer before acting on them. Handlers are not responsible for
business-rule validation; they are only responsible for rejecting structurally malformed requests
(e.g., an unparseable UUID). Inputs that are structurally valid but semantically wrong (e.g., an
empty usage string, an out-of-range value) must be rejected in services.

- Reject blank required strings, zero-value identifiers, and obviously invalid values explicitly.
- Bound all lengths that feed into storage or downstream systems.
- When an input is a fixed set of known values (e.g., an algorithm name, a usage type), reject
  any value that is not in the known set rather than forwarding it downstream.

### SQL injection

Never construct SQL with string concatenation or `fmt.Sprintf`. Every DAO query is embedded from
a `.sql` file and executed with bun's parameterized query API (`NewRaw(query, args...)`). The
`//go:embed` pattern enforced in this project makes raw string SQL visible and reviewable — never
move SQL into Go string literals to sidestep this.

### Sensitive data in logs and traces

Never include private keys, secret material, tokens, raw credentials, or full payloads in log
messages, span attributes, or error strings. These end up in observability systems accessible to
many people and are not encrypted at rest.

- Log and trace identifiers (key IDs, user IDs, usage names), not values (ciphertexts, signed JWTs).
- If an error must include context about the data that caused it, use safe descriptors:
  `"key id: %s"` yes, `"private key: %s"` no.

### Error information disclosure

REST handlers are public-facing. Never return internal error details, stack traces, database messages,
or raw `error.Error()` output in HTTP responses. Map to a generic status text:

```go
http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
```

This rule applies to **structured response bodies** too, not just `http.Error` shortcuts. A health,
status, or diagnostic endpoint that returns JSON must not embed `err.Error()` in any field — the
public shape should carry a binary state (e.g. `"up"` / `"down"`) and at most a stable error _code_,
never the raw message. Wrapped database errors routinely contain hostnames, ports, or internal
schema names; one call to `err.Error()` on a downed dependency is enough to leak infrastructure
topology to an unauthenticated caller. Log or trace the full error instead; return only the shape
an external client is entitled to see. If operators need richer detail, expose it on a separate,
auth-gated endpoint — not the public one.

gRPC handlers are internal (service-to-service), so slightly more context in status messages is
acceptable, but still avoid leaking raw database errors or cryptographic details.

### Transport boundaries

- **REST is public.** Any data returned in a REST response may reach an end-user or be logged by
  an intermediary. Return only what the client legitimately needs.
- **gRPC is internal.** gRPC services must never be exposed directly to the internet. Route all
  external traffic through the REST API.

### Cryptographic material

When this service or any Agora service handles key material:

- Private keys must never appear in responses, logs, spans, or error messages.
- Do not store key material in context values beyond the minimum scope needed.
- Use established crypto packages (`crypto/*`, `golang.org/x/crypto`) for any cryptographic
  operation. Never implement cryptographic primitives by hand.

---

## Common Pitfalls

- **HTTP/gRPC concerns leaking into services.** If you see `http.Error`, `json.Marshal`, or
  `status.Errorf` in a service file, move it to the handler.
- **DAO imports in handlers.** Handlers depend on services via interfaces and never import `dao`
  directly. If a handler needs a DAO sentinel error, the service must re-export it as its own
  sentinel (defined in `services/common.go` if shared). The service translates `dao.ErrXxx` →
  `services.ErrXxx` before returning it; the handler then checks `services.ErrXxx`. This keeps
  the handler fully decoupled from the DAO layer.
- **Inline SQL.** All SQL lives in `.sql` files embedded with `//go:embed` at package level.
- **Mocks are generated** with `mockery` from the interfaces defined in each file. Run
  `make generate` after adding or changing interfaces. Never write mocks by hand.
- **`http` in new file or type names.** Use `rest` everywhere. Rename legacy `http.*` files and
  `Http`-prefixed types when they come in scope.
- **Calling `otel.ReportError` on expected conditions.** Only report genuine failures, not
  sentinel errors that represent normal domain outcomes (e.g., not-found).
- **`context.Context` stored in a struct.** Always pass it as a method argument.
- **New packages without asking.** Always get explicit developer approval before adding to `go.mod`.
- **Returning raw error details in REST responses.** Always map to a generic HTTP status text.
  The rule covers structured response fields too: a health/status JSON body must never include
  `err.Error()` — it leaks infrastructure detail (hostnames, schema names) to unauthenticated callers.
- **Logging or tracing sensitive material.** Only record identifiers, never key ciphertexts, tokens, or credentials.
- **String-formatted SQL.** All SQL must use parameterized queries via bun — no `fmt.Sprintf` in query construction.
- **Validation in handlers instead of services.** Business-rule checks belong in services. Handlers only reject structurally invalid input (e.g., unparseable UUID).
- **Multiple `time.Now()` calls in one operation.** When an operation writes or returns several
  timestamps that should share the same instant (created-at + expires-at, start + deadline),
  capture `now := time.Now()` once and reuse it. Independent calls drift by nanoseconds, break
  tests that assert exact TTLs, and muddy the semantics of the record.

---

## Time Capture

When an operation derives more than one timestamp from "now" — a created-at plus an expires-at,
a start plus a deadline, paired audit fields — capture `time.Now()` **once** at the top of the
operation and reuse the variable:

```go
// WRONG — two independent wall-clock reads; the deltas between derived
// timestamps are not exactly the configured TTL.
repo.Exec(ctx, &dao.InsertRequest{
    Now:        time.Now(),
    Expiration: time.Now().Add(cfg.TTL),
})

// CORRECT — one logical instant, reused.
now := time.Now()
repo.Exec(ctx, &dao.InsertRequest{
    Now:        now,
    Expiration: now.Add(cfg.TTL),
})
```

Two reasons: correctness (the second `time.Now()` is measurably later, so a row's
`expires_at - created_at` is not exactly the TTL), and testability (a single captured value is
easier to freeze or assert against than two independent wall-clock reads). The rule extends to any
pair of values that should share the same "now": audit logs with both `started_at` and `recorded_at`,
request traces that emit `begin` and `finish` attributes, etc. Only call `time.Now()` multiple
times when you genuinely want distinct measurements (e.g., computing a duration).

---

## Loop Variable Closures (Go 1.22+)

This project targets Go 1.22 or later. Since Go 1.22, `for` loop variables are re-created per
iteration, so closures capture the correct value without any extra copy:

```go
// WRONG (pre-1.22 workaround — no longer needed):
for usage := range keys {
    currentUsage := usage  // unnecessary copy
    fetch := func(ctx context.Context) (any, error) {
        return source.Search(ctx, currentUsage)
    }
}

// CORRECT:
for usage := range keys {
    fetch := func(ctx context.Context) (any, error) {
        return source.Search(ctx, usage)  // safe: usage is per-iteration in Go 1.22+
    }
}
```

Remove any `current := item` copies inside `for range` loops when you encounter them. They are
dead code on this codebase's minimum Go version.

---

## Active Migrations

Apply these when a file is already in scope for a change — never speculatively:

| Legacy pattern                | Current standard        |
| ----------------------------- | ----------------------- |
| Handler files named `http.*`  | Rename to `rest.*`      |
| Type names with `Http` prefix | Rename to `Rest` prefix |
