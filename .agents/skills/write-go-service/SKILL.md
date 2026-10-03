---
name: write-go-service
description: >
  Write or review Go services with cmd/internal/pkg layering. Own consumer interfaces,
  transactions, telemetry, security, and layer tests; load with write-go and write-go-tests.
---

# Go — Backend Services (clean architecture)

This skill governs Go in the a-novel backend services: repos shaped as
`cmd/` + `internal/{config,lib,dao,core,handlers,models}` + (optionally) `pkg/`. Aim for coherent,
idiomatic, minimal code that strictly respects the layered architecture.

**This skill is layered on `write-go`.** Everything there — read-before-edit, the
`pnpm format:go`/`pnpm lint:go` discipline, dependency policy, naming of packages/files/variables,
constructors, error sentinels and `%w` wrapping, context rules, time-capture-once, secrets
hygiene, the span-reporting rule — applies here unchanged; this skill adds the
**service-architecture-specific** rules on top. Load `write-go` and `write-go-tests` (and
`document-code` when documenting) alongside it. For shared libraries under `a-novel-kit` (`golib`,
`jwt`, …) load `write-go-kit` instead of this skill.

**Before touching any layer**, read its sibling files, the interfaces it depends on, and the
interfaces it exposes. These services are deliberately consistent — copy the established pattern
exactly; coherence outranks preference.

---

## Load the affected layer

Keep the architecture, interface, and security rules in this entry point active. Before planning,
editing, or reviewing service behavior, read each reference matching an affected layer. Read the
layer rules when testing its contract too; crossing layers requires each matching reference.

- DAO operations or transaction ownership, including core/job transactions: [data access](references/data-access.md).
- Core validation/orchestration or REST/gRPC handlers: [operations](references/operations.md).
- Config, internal helpers, models, exported clients, or command wiring: [wiring](references/wiring.md).
- New or changed DAO/core operations or handlers, and any other operation with a span:
  [telemetry](references/telemetry.md). Apply this to reviews even when the existing code lacks a span.
  Every operation that fails reports its error on its own span.
- Tests, regression coverage, or test review: [layer test patterns](references/testing.md), alongside
  `write-go-tests`. DAO integration tests and generated mocks remain required where applicable.

Consumer-owned interfaces and the mandated layer boundaries earn their place even with one
implementation. Simplify within them using `prefer-small-solutions`; do not flatten the architecture.

## After every edit

Run the base loop from `write-go` (`pnpm generate:go` when interfaces/proto changed →
`pnpm format:go` → `pnpm lint:go`), then:

1. **`write-go-tests`** — follow `develop-feature` for timing: focused checks and necessary tests
   during drafting, full relevant behavioral coverage after issue scope approval. Run affected
   suites before readiness; do not add tests simply because a file was touched.
2. **`document-code`** — doc comments for every symbol added or changed.
3. If a REST handler change alters the public API contract (new endpoint, changed parameters,
   changed response shape, added/removed status codes), invoke **`write-openapi`** to update
   `openapi.yaml`. The spec is a durable public commitment; drift breaks clients.

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

## Static data definitions

**Never inline a production static data definition in Go.** Keep JSON Schemas, prompt templates,
policy documents, and other declarative payloads in their native-format files, then embed them with
`//go:embed`. Go owns loading and typing; the native file owns the definition. Small scalar constants
remain code.

Place embedded production definitions by audience:

- **`internal/config/`** — definitions internal to service behavior and not exposed to users.
- **`internal/models/`** — definitions that shape user-facing data or contracts.

Expose each definition through a small package near its files. The binary remains self-contained;
production code never reads definitions relative to its working directory.

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
interfaces** that implementation needs, though not every layer has them:

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
  no interface of its own. The consumer owns the contract and the producer satisfies it, so neither
  layer forces the other to import it.
- **One method per interface, named `Exec`.** Idiomatic Go names interface methods after what they
  do; this is a deliberate project standard that trades that for consistency. Follow it.
- The method signature is `(ctx context.Context, request *XxxRequest) (Result, error)` — `Result`
  is a struct pointer, slice, or named type, never a bare primitive, so fields can be added later
  without breaking callers.
- **Exceptions in handlers**: gRPC method signatures are protoc-generated; REST handlers implement
  `ServeHTTP(w, r)`. Both are accepted exceptions to the `Exec` shape.
- Service and DAO types are **stateless after construction** — every field is set by `New*` and
  never mutated, so every type is safe for concurrent use without synchronization. This is a
  requirement.
- **Shared sentinels/types within a layer** go in that layer's single `common.go` (one per layer
  at most), never duplicated across files.

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
  (`http.StatusText(...)`); the helper does this for you. **Structured bodies** count too —
  a health/status JSON body carries a binary state (`"up"` / `"down"`) and at most a stable
  error _code_, never `err.Error()`: a wrapped DB error routinely contains hostnames, ports and
  schema names, so one `err.Error()` on a downed dependency leaks topology to an unauthenticated
  caller. Log/trace the full error; return only the public shape. Richer detail goes on a separate,
  auth-gated endpoint. gRPC (internal) tolerates slightly more context in status messages but still
  no raw DB/crypto detail.
- **Transport boundaries.** REST is public — anything in a REST response may reach an end user or
  be logged by an intermediary; return only what the client is entitled to. gRPC is internal —
  route all external traffic through REST, never expose gRPC directly.

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

> A repo that has not yet taken the rename is migrated in its own session. Do **not** migrate one
> piecemeal as a side effect of another change.

---

## Common pitfalls

(`write-go` lists the language-level ones — context-in-struct, new deps without asking, logging
secrets, multiple `time.Now()`, bare `return nil, ErrXxx` from a layer with a span, `new(T)`,
discarding errors. These are the **service-architecture** ones, each stated in full in its section
above:)

- **HTTP/gRPC concerns in a core file** — `http.Error`, `json.Marshal`, `status.Errorf` → move to
  the handler.
- **`dao` imported in handlers.** Handlers depend on core components via interfaces; a handler that
  needs a DAO sentinel checks the core re-export `core.ErrXxx` (in `core/common.go` if shared).
- **Inline SQL** instead of a `//go:embed`-ed `.sql` file, or a query built with `fmt.Sprintf`
  instead of bun parameters.
- **Hand-written mocks.** Mocks are `mockery`-generated from the interfaces in each file; run
  `pnpm generate:go` after adding/changing an interface.
- **`http` in new file or type names** instead of `rest`.
- **Raw error detail in a REST response**, including in a structured JSON field.
- **Validation in handlers** instead of the core layer.
- **A span on a constructor or config loader.**
- **A transaction started in a handler** instead of the core layer or `cmd/`.
