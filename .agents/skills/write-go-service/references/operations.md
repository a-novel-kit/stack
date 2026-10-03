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
- **DAO/sub-service sentinels travel up** — return them wrapped with `%w` so handlers can match
  with `errors.Is`. The service may re-export a DAO sentinel as its own (`core.ErrXxx`) when
  the handler needs to map it (see Common Pitfalls).
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
  helper's fallback covers unmapped errors as 500. `httpf.HandleError` marks the span failed for a
  5xx and leaves a 4xx unset, so never add a separate report on a path that goes through it (see
  Telemetry).
- Conventional short names `w` / `r`. JSON in via `json.NewDecoder(r.Body)` or `gorilla/schema` for
  query params; out via the project's `httpf.SendJSON`.
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
        _ = otel.ReportError(span, err)
        return nil, status.Error(codes.Internal, "create order: internal error")
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
- Convert proto↔core models **in the handler**. Never pass a proto type into the core layer.
- Handler type names carry the `Grpc` prefix; the service interface mirrors it. File:
  `grpc.<entity><Operation>.go`.
