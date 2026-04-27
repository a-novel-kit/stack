---
name: write-proto
description: >
  Write, review, and modify Protobuf definitions for Agora backend services. Use this skill
  whenever creating or editing .proto files — new RPCs, new messages, shared types, enums, or
  breaking-change assessment. Applies to internal/models/proto/ and the buf toolchain.
---

# Protobuf Writing Skill

This skill governs how to write and maintain `.proto` files in Agora backend services. Proto
definitions are the contract between gRPC producers and consumers — once published, they must
evolve without breaking existing callers. Treat every field number and type as a durable commitment.

**Before touching any proto file**, read it and all files it imports. Read `buf.yaml` and
`buf.gen.yaml` if you have not already — they control what gets generated and where. Look at the
corresponding generated Go file in `internal/handlers/protogen/` to understand what callers
currently depend on.

---

## Project Layout

```
internal/models/proto/        # Source .proto files (edit these)
  <entity>_<operation>.proto  # One file per RPC — service + request/response messages
  <entity>.proto              # Shared message/enum types (no service definition)

internal/handlers/protogen/   # Generated Go stubs (never edit — always regenerated from scratch)
  <entity>_<operation>.pb.go       # Message types
  <entity>_<operation>_grpc.pb.go  # gRPC client/server interfaces
```

The entire `internal/handlers/protogen/` directory is **deleted and recreated** on every
`make generate` run. Never put hand-written code there.

---

## After Every Edit

```
make format-proto   # format .proto files + sync buf.lock
make lint-proto     # validate against buf's STANDARD ruleset
make generate       # wipe protogen/ and regenerate Go stubs + mocks
make format-go      # goimports on the newly generated files
make lint-go        # catch any issues in handler code using new types
```

Run these in order. `make format-proto` must come before `make generate` — buf formats the source
files in place, and the generated output reflects the formatted source.

After `make generate`, update the Go handler code that uses the changed types, then run
`make format-go` and `make lint-go` to confirm everything compiles cleanly.

Then invoke the **`document-code` skill** for every `.proto` file you created or modified. Proto
comments are the public API contract — they must be accurate, complete, and written before the
change is considered done. Documentation is not optional.

---

## Toolchain

This project uses [buf](https://buf.build) v2, not `protoc` directly. All buf operations run
through `go tool buf` (pinned in `go.mod` as a tool dependency).

**`buf.yaml`** (linting and breaking-change rules):

```yaml
version: v2
modules:
  - path: internal/models/proto
lint:
  use:
    - STANDARD
  except:
    - PACKAGE_DEFINED # no proto package declarations — managed mode handles namespacing
breaking:
  use:
    - FILE # field renames/removals are breaking at the file level
```

**`buf.gen.yaml`** (code generation):

```yaml
version: v2
managed:
  enabled: true
  override:
    - file_option: go_package_prefix
      value: github.com/a-novel/service-json-keys/v2/internal/handlers/protogen;protogen
plugins:
  - remote: buf.build/protocolbuffers/go # generates message structs (.pb.go)
    out: internal/handlers/protogen
    opt: paths=source_relative
  - remote: buf.build/grpc/go # generates gRPC client/server code (_grpc.pb.go)
    out: internal/handlers/protogen
    opt:
      - paths=source_relative
inputs:
  - directory: internal/models/proto
```

Managed mode injects `option go_package` automatically — do **not** add `option go_package` or
`package` statements to `.proto` files manually.

---

## File Structure

**One service per file.** Each `.proto` file that defines a `service` contains exactly that
service and its request/response messages. No other services or unrelated types.

```
jwk_get.proto       → JwkGetService + JwkGetRequest + JwkGetResponse
jwk_list.proto      → JwkListService + JwkListRequest + JwkListResponse
claims_sign.proto   → ClaimsSignService + ClaimsSignRequest + ClaimsSignResponse
status.proto        → StatusService + StatusRequest + StatusResponse + DependencyHealth + DependencyStatus
jwk.proto           → Jwk message + JwkUsage enum (shared, no service)
```

Shared types that appear in multiple service files get their own file with no service definition.
Import them with a relative path: `import "jwk.proto";`.

---

## Naming Conventions

### Services and RPCs

| Element          | Convention                    | Example                |
| ---------------- | ----------------------------- | ---------------------- |
| Service name     | `<Entity><Operation>Service`  | `JwkGetService`        |
| RPC name         | `<Entity><Operation>`         | `JwkGet`, `ClaimsSign` |
| Request message  | `<Entity><Operation>Request`  | `JwkGetRequest`        |
| Response message | `<Entity><Operation>Response` | `JwkGetResponse`       |

The RPC name must match the Go service operation name exactly — this is what `cmd/grpc/main.go`
registers and what `pkg/go/client.go` calls.

### Messages

Messages use `PascalCase`. Field names use `snake_case` — the Go generator converts them to
`camelCase` getters (`GetKeyId()`, `GetUsage()`). Always use `snake_case` in proto; never use
camelCase field names.

### Enums

Enum type names use `PascalCase`. Enum values use `SCREAMING_SNAKE_CASE` with the type name as
a prefix, and **always start at 0 with an `_UNSPECIFIED` value**:

```proto
enum DependencyStatus {
  DEPENDENCY_STATUS_UNSPECIFIED = 0;  // required zero value — must not be used in requests
  DEPENDENCY_STATUS_UP = 1;
  DEPENDENCY_STATUS_DOWN = 2;
}
```

The `_UNSPECIFIED = 0` value is required by proto3: unset enum fields default to 0, and you must
be able to detect "not set" at the application level. Never assign 0 to a meaningful value.

---

## Comments

Document every `service`, `rpc`, `message`, and `field`. Comments go immediately above the
element, using `//` (single-line) or `/* */` (multi-line):

```proto
// JwkGetService returns a public JSON Web Key by its key ID.
// The returned key may be used by any recipient to verify a token.
service JwkGetService {
  rpc JwkGet(JwkGetRequest) returns (JwkGetResponse);
}

// JwkGetRequest identifies the key to retrieve by its key ID.
message JwkGetRequest {
  // ID of the key to retrieve. Corresponds to the "kid" field in the JWT header.
  string id = 1;
}
```

For enum values, explain what each value means (especially `_UNSPECIFIED`):

```proto
enum DependencyStatus {
  // DEPENDENCY_STATUS_UNSPECIFIED means the application has failed to, or has not yet
  // assessed the status of the given dependency.
  DEPENDENCY_STATUS_UNSPECIFIED = 0;
  // DEPENDENCY_STATUS_UP means the dependency was successfully pinged.
  DEPENDENCY_STATUS_UP = 1;
}
```

---

## Field Numbering

Field numbers are **permanent**. They are serialized in binary encoding and must never change
or be reused:

- Start at `1` for the first field. Use sequential numbers.
- Once a field is removed, **reserve** its number and name to prevent accidental reuse:
  ```proto
  message JwkGetRequest {
    reserved 2;
    reserved "legacy_field";
    string id = 1;
  }
  ```
- Adding a new field with a new, previously unused number is always safe.
- Never start numbering at 0 — proto3 uses 0 as the default for numeric types and it conflicts
  with unset detection.
- Field numbers 1–15 are encoded in one byte; 16–2047 in two bytes. Reserve 1–15 for the most
  frequently used fields.

---

## Wire-Safe Changes vs Breaking Changes

The project uses `FILE`-level breaking detection. Buf will reject:

| Change                        | Why it breaks                                          |
| ----------------------------- | ------------------------------------------------------ |
| Remove a field                | Existing callers setting that field silently lose data |
| Rename a field                | Field name affects JSON encoding and Go accessor names |
| Change a field type           | Existing serialized data becomes unreadable            |
| Rename a message              | All Go types derived from it are renamed               |
| Rename/renumber an enum value | Existing serialized values decode incorrectly          |
| Remove a service or RPC       | Existing callers receive "unimplemented" errors        |

Wire-safe (non-breaking) changes:

| Change                  | Why it is safe                                          |
| ----------------------- | ------------------------------------------------------- |
| Add a new field         | Old clients ignore unknown fields; new clients see it   |
| Add a new message       | Unused until referenced                                 |
| Add a new enum value    | Old clients receive the numeric value and can ignore it |
| Add a new RPC           | Old clients never call it                               |
| Add or change a comment | No runtime impact                                       |

---

## Well-Known Types

Prefer proto's well-known types for common data shapes rather than raw primitives:

| Use case                      | Import                            | Type                                |
| ----------------------------- | --------------------------------- | ----------------------------------- |
| Arbitrary JSON payload        | `google/protobuf/any.proto`       | `google.protobuf.Any`               |
| Timestamps                    | `google/protobuf/timestamp.proto` | `google.protobuf.Timestamp`         |
| Optional primitive (nullable) | `google/protobuf/wrappers.proto`  | `google.protobuf.StringValue`, etc. |
| Empty request/response        | `google/protobuf/empty.proto`     | `google.protobuf.Empty`             |

Example — importing and using Any (as in `claims_sign.proto`):

```proto
import "google/protobuf/any.proto";

message ClaimsSignRequest {
  string usage = 1;
  google.protobuf.Any payload = 2;
}
```

---

## Alignment with Go Layers

Proto types are **handler-layer only**. They are generated into `internal/handlers/protogen/`
and must never be imported by `services/`, `dao/`, or `config/`. Handlers own all conversions
between proto types and service types.

| Proto element           | Generated Go                             | Used in                               |
| ----------------------- | ---------------------------------------- | ------------------------------------- |
| `service JwkGetService` | `protogen.JwkGetServiceServer` interface | embedded in `handlers.GrpcJwkGet`     |
| `service JwkGetService` | `protogen.JwkGetServiceClient` interface | `pkg/go/client.go` via gRPC dial      |
| `service JwkGetService` | `protogen.RegisterJwkGetServiceServer`   | `cmd/grpc/main.go`                    |
| `message JwkGetRequest` | `protogen.JwkGetRequest` struct          | handler, converted to service request |
| `enum DependencyStatus` | `protogen.DependencyStatus` const        | handler only                          |

When you add a new service, the handler file embeds the generated `Unimplemented<Name>Server`
struct and registers with `protogen.Register<Name>Server` in `cmd/grpc/main.go`.

---

## Adding a New RPC: Step-by-Step

1. **Create `internal/models/proto/<entity>_<operation>.proto`** with the service, request,
   and response messages. Follow file structure and naming conventions above.
2. **Run `make format-proto`** — formats the file and updates buf.lock.
3. **Run `make lint-proto`** — fix any violations before generating.
4. **Run `make generate`** — wipes `protogen/` and regenerates everything.
5. **Create `internal/handlers/grpc.<entity><Operation>.go`** with the new handler type.
   Embed `protogen.Unimplemented<ServiceName>Server`, define the service interface, implement
   the RPC method.
6. **Run `make generate`** again if you added a new interface (regenerates mocks).
7. **Wire it up in `cmd/grpc/main.go`**: construct the handler and call
   `protogen.Register<ServiceName>Server(server, handler)`.
8. **Invoke the `document-code` skill** for the new `.proto` file and the new Go handler file.
9. **Invoke the `write-go-tests` skill** to write tests for the new handler.
10. **Run `make format-go lint-go test-unit`** to confirm everything is clean.

---

## Common Pitfalls

- **Missing `_UNSPECIFIED = 0` in enums.** proto3 defaults unset enum fields to 0. If 0 is a
  meaningful value, callers cannot distinguish "not set" from "set to the first value". Always
  reserve 0 as the unspecified/invalid sentinel.
- **Adding `package` or `option go_package` to .proto files.** Managed mode in buf.gen.yaml
  injects these automatically. Adding them manually will conflict with managed mode settings
  or produce duplicate declarations.
- **Editing files in `internal/handlers/protogen/`.** They are always overwritten. Any edits
  will be lost on the next `make generate`.
- **Reusing a field number.** Once a number is removed, reserve it. Reusing a number from a
  deleted field causes silent data corruption for clients that still send the old field.
- **Using proto types in services or DAO.** Proto types belong exclusively in handlers. Never
  import `internal/handlers/protogen` from `internal/services` or `internal/dao`.
- **Forgetting `make format-proto` before `make generate`.** buf formats source files in place;
  the generated output reflects the formatted source. Run format first, then generate.
- **Renaming a field as a "safe" refactor.** Field renames are breaking at FILE level — buf will
  reject them. If a field must be renamed, add a new field with the correct name and a new
  number, deprecate the old one with a comment, then remove it in a coordinated release.
- **`repeated` on response fields that could be empty.** An empty `repeated` field returns a nil
  slice in Go, not an empty slice. Callers must use `GetField()` (nil-safe accessor) rather than
  `.Field` directly.
