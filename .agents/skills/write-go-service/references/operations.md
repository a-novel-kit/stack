# Core and transport operations

Read this reference when routed here by [write-go-service](../SKILL.md). Its rules apply to the selected work.

Contents:

- [Core layer (`internal/core/`)](#core-layer-internalcore)
- [Handlers layer (`internal/handlers/`)](#handlers-layer-internalhandlers)
- [REST handlers](#rest-handlers)
- [gRPC handlers](#grpc-handlers)

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
    return results, nil
}
```

- **Validate inputs in the `Exec` body** — validation is a service responsibility, never a handler
  one. Use the project's validation library where the core layer already has one
  (`go-playground/validator` via a package-level `validate` instance, with a shared
  `ErrInvalidRequest` sentinel); otherwise plain conditional checks. Reject blank required strings,
  zero-value identifiers, out-of-range values, and values outside a known set.
- **Validate first.** `validate.Struct` runs before anything derives a value from the request, so
  later code may assume the tags hold. A list's `Limit` is `validate:"min=0,max=N"`, and only zero
  takes the default page size; a negative limit fails instead of falling back to it. The default
  goes straight into the request field, with no local copy:

  ```go
  if request.Limit == 0 {
      request.Limit = SceneListLimitDefault
  }
  ```

- **A cap on a request field is a validator tag**, never a config value or a hand-written check.
  `validate:"required,max=2097152"` caps a `json.RawMessage` at 2 MiB, and `validate:"max=500"` caps
  a slice. When no built-in tag expresses the rule, register a custom one on the package's
  `validate` instance with `RegisterValidation`, as `notblank` is. A cap the schema already holds
  reuses its value and needs no setting of its own. Rate and concurrency limits stay configuration
  (see [request limits](request-limits.md)).
- **A known set is a Go slice behind a custom tag**, never `oneof`. `ProjectLanguages` lists the
  languages, and the `language` tag checks a value against it, so code and doc comments name the
  set by one variable. A set a registry already holds validates against the registry: the
  `layerkind` tag accepts a kind when `schemas.Content(kind)` finds its content schema.

  ```go
  var ProjectLanguages = []string{"en", "fr"}

  func ValidateLanguage(fl validator.FieldLevel) bool {
      return slices.Contains(ProjectLanguages, fl.Field().String())
  }
  ```

- **A create, a delete and a content write take a list** of at most 100 items,
  `validate:"required,min=1,max=100,dive"`, and apply all of it or none, as `SceneCreate`,
  `SceneDelete` and `CellWrite` do. One item is a list of one, so each operation has one route and
  no separate batch route. The client chooses how much each request carries, and balances bandwidth
  and load itself. A list of IDs adds `unique` before `dive`; a list of items that must not collide
  gets a custom tag, such as `uniquecells`. A delete compares how many rows its DAO deleted with how
  many IDs it named, and a miss returns not-found inside the transaction, which rolls the whole
  delete back. `write-sql`'s [batch writes](../../write-sql/SKILL.md#batch-writes) give the
  statements.

  ```go
  deleted, err := service.dao.Exec(ctx, &dao.SceneDeleteRequest{ProjectID: request.ProjectID, IDs: request.IDs})
  if err != nil {
      return fmt.Errorf("delete scenes: %w", err)
  }

  // A miss rolls the whole delete back.
  if len(deleted) != len(request.IDs) {
      return fmt.Errorf("%w: %d of %d scenes", ErrSceneNotFound, len(request.IDs)-len(deleted), len(request.IDs))
  }
  ```

- **Each operation does one job.** Project creation inserts the project and the layers every
  project must hold, and no scene. The client adds the first scenes through the scene create next,
  as it adds any later one.
- **DAO/sub-service sentinels travel up** — return them wrapped with `%w` so handlers can match
  with `errors.Is`. The service may re-export a DAO sentinel as its own (`core.ErrXxx`) when
  the handler needs to map it (see Common Pitfalls).
- **Remap a miss only where a request can cause it.** Once a preflight proves a row exists, a later
  miss of that row is a plain error. The ownership check runs before the project lock, and the
  kind read runs before the layer delete, so neither the lock nor the delete maps its not-found
  sentinel again. Only a concurrent delete could reach that path, and a 500 answers it.
- **A preflight repeated across services becomes a helper** in the layer's `common.go`. It takes the
  dependency and an options struct, and opens its own span, as every core function that takes a
  context does (see [telemetry](telemetry.md)). The ownership check is one, and each service still
  declares its own DAO interface, which satisfies the helper's:

  ```go
  // projectOwnerDao checks that a user owns a project.
  type projectOwnerDao interface {
      Exec(ctx context.Context, request *dao.ProjectOwnedRequest) (bool, error)
  }

  type projectOwnershipOpts struct {
      projectID uuid.UUID
      ownerID   uuid.UUID
  }

  func checkProjectOwnership(ctx context.Context, daoOwner projectOwnerDao, opts projectOwnershipOpts) error {
      ctx, span := otel.Tracer().Start(ctx, "core.checkProjectOwnership")
      defer span.End()

      owned, err := daoOwner.Exec(ctx, &dao.ProjectOwnedRequest{ID: opts.projectID, OwnerID: opts.ownerID})
      if err != nil {
          return otel.ReportError(span, err)
      }

      if !owned {
          return otel.ReportError(span, ErrProjectNotFound)
      }

      return nil
  }
  ```

- **A rule over a set of values reads one table.** `requiredLayers` maps each kind a project always
  holds a layer of to the name a new project gives that layer. Project creation seeds one layer per
  entry, in sorted kind order since Go randomizes map iteration, and every delete checks the same
  table, answering one generic `ErrRequiredLayer`. A check against one hard-coded kind, with a
  sentinel named after it, turns the next kind into a hunt through every file that tested the
  first.
- **Keep the last required item, not the item type.** A project may hold several layers of a
  required kind, and a delete refuses only when it would remove every remaining one. The count is
  mutable state, so the core reads it under the project lock: read before it, two concurrent
  deletes could each leave one layer and together leave none. `PgLayerKind` returns
  `{ID, Kind, Count}` for each layer the delete names, `Count` being how many layers of the project
  share the kind:

  ```go
  var requiredLayers = map[string]string{
      LayerKindManuscript: "Manuscript",
  }

  func keepRequiredLayers(kinds []*dao.LayerKind) error {
      deleting := lo.CountValuesBy(kinds, func(kind *dao.LayerKind) string { return kind.Kind })

      for _, kind := range kinds {
          _, required := requiredLayers[kind.Kind]
          if required && deleting[kind.Kind] == kind.Count {
              return fmt.Errorf("%w: the last %s layer", ErrRequiredLayer, kind.Kind)
          }
      }

      return nil
  }
  ```

- **The client owns positions.** A create or a move of an ordered row takes the target position the
  client sends, never an anchor such as `after`. The client keeps its order in sync, so the core
  reads no neighbor to place the row. A position past the end appends on create and moves the row
  last on move. The DAO clamps it in the statement that shifts the other rows (see `write-sql`'s
  [bounded reads](../../write-sql/SKILL.md#bounded-reads)).
- Services reach DAOs **only** through the local interfaces they declare. Configuration is injected
  as a **concrete config struct**: it is static and never needs mocking.

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

    err := muxDecoder.Decode(&request, r.URL.Query())
    if err != nil {
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

    httpf.SendJSON(ctx, w, span, lo.Map(users, func(user *core.User, _ int) User { return loadUser(user) }))
}

func NewRestUserList(service RestUserListService, logger logging.Log) *RestUserList {
    return &RestUserList{service: service, logger: logger}
}
```

- **REST is public-facing.** Never leak internal error detail. Map each expected sentinel to a
  status with the project's error-mapping helper (`httpf.HandleError` + `httpf.ErrMap`); the
  helper's fallback covers unmapped errors as 500. `httpf.HandleError` marks the span failed for a
  5xx and leaves a 4xx unset, so never add a separate report on a path that goes through it (see
  Telemetry).
- Conventional short names `w` / `r`. JSON in via `json.NewDecoder(r.Body)` or `gorilla/schema` for
  query params; out via the project's `httpf.SendJSON`.
- **A list of UUIDs in the query needs a converter.** A delete names its rows with a repeated `id`
  parameter, decoded into `[]uuid.UUID`. gorilla/schema decodes a single `uuid.UUID` through its
  `UnmarshalText`, but fails a slice of them with "converter not found", so every request carrying
  the parameter answers 400. `rest.decoder.go` registers one on the package decoder:

  ```go
  var muxDecoder = newMuxDecoder()

  func newMuxDecoder() *schema.Decoder {
      decoder := schema.NewDecoder()
      decoder.RegisterConverter(uuid.UUID{}, func(value string) reflect.Value {
          id, err := uuid.Parse(value)
          if err != nil {
              return reflect.Value{} // An invalid value fails the decode.
          }

          return reflect.ValueOf(id)
      })

      return decoder
  }
  ```

- Handler type names carry the `Rest` prefix (`RestUserList`); the service interface mirrors it
  (`RestUserListService`). File: `rest.<entity><Operation>.go`.

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
        return nil, status.Error(codes.NotFound, "create order: user not found")
    }
    if err != nil {
        return nil, grpcf.Fault(codes.Internal, "create order: internal error", otel.ReportError(span, err))
    }

    return &protogen.OrderCreateResponse{OrderId: result.ID.String()}, nil
}

func NewGrpcOrderCreate(service GrpcOrderCreateService) *GrpcOrderCreate {
    return &GrpcOrderCreate{service: service}
}
```

- **gRPC is internal** (service-to-service only) — never exposed to the internet. Embed
  `protogen.Unimplemented<ServiceName>Server`.
- Map core sentinels to `codes.*` via `errors.Is` + `status.Error`. Inside `status.Error` /
  `status.Errorf` use `%v`, never `%w`: gRPC status errors don't support `errors.Unwrap`, so `%w`
  misleads. Keep status _messages_ generic enough not to leak DB/crypto detail.
- Answer a server fault (`Internal`, `Unavailable`) with `grpcf.Fault(code, msg, cause)`. The caller
  receives the status alone, and the fault keeps its cause for Sentry, which groups faults by it. A
  bare `status.Error` wraps nothing and puts every fault of a method in one Sentry issue.
- Convert proto↔core models **in the handler**. Never pass a proto type into the core layer.
- Handler type names carry the `Grpc` prefix; the service interface mirrors it. File:
  `grpc.<entity><Operation>.go`.
