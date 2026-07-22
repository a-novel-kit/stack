---
name: write-proto
description: >
  Write, review, and modify Protobuf definitions for Agora backend services. Use whenever
  creating or editing .proto files — new RPCs, messages, shared types, enums, or breaking-change
  assessment. Covers internal/models/proto/ and the buf toolchain.
---

# Protobuf Writing Skill

Proto definitions are the contract between gRPC producers and consumers: once published, they
must evolve without breaking existing callers. Treat every field number and type as a durable
commitment.

**Before touching any proto file**, read it and all files it imports. Read `buf.yaml` and
`buf.gen.yaml` — they control what gets generated and where. Read the corresponding generated Go
file in `internal/handlers/protogen/` to see what callers currently depend on.

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
`pnpm generate:go` run. Never put hand-written code there.

---

## After Every Edit

```
pnpm format:proto   # format .proto files + sync buf.lock
pnpm lint:proto     # validate against buf's STANDARD ruleset
pnpm generate:go       # wipe protogen/ and regenerate Go stubs + mocks
pnpm format:go      # goimports on the newly generated files
pnpm lint:go        # catch any issues in handler code using new types
```

**Every edit** includes a comment-only one. `protoc-gen-go` copies leading comments into the generated
Go, so rewording a `message` or `field` doc changes `protogen/` as surely as adding a field does, and
the `generated-go` job fails on the drift. Commit the regenerated output as its own `chore(gen)`
commit — `git-conventions` forbids mixing commit types, and a `docs` commit carrying generated files
hides why they changed.

Run these in order. `pnpm format:proto` must come before `pnpm generate:go` — buf formats the source
files in place, and the generated output reflects the formatted source.

After `pnpm generate:go`, update the Go handler code that uses the changed types, then run
`pnpm format:go` and `pnpm lint:go` to confirm it compiles cleanly.

Then invoke the **`document-code` skill** for every `.proto` file you created or modified. Proto
comments are the public API contract — write them, accurate and complete, before considering the
change done.

---

## Toolchain

This project uses [buf](https://buf.build) v2, not `protoc` directly. All buf operations run
through `go tool -modfile=buf.mod buf`.

buf is pinned in its **own** `buf.mod`, not in `go.mod`. It is a generator, never imported: the
emitted stubs link `google.golang.org/protobuf`, which stays a direct require of the service, while
buf's own dependency graph — more than half of `go.mod` before the split — stays out of the module
the service ships. The same holds for every tool: one modfile each, and each with its own Renovate
branch prefix.

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
`package` statements to `.proto` files manually. A manual one conflicts with the managed-mode
settings or produces a duplicate declaration.

---

## File Structure

**One service per file.** A `.proto` file that defines a `service` holds exactly that service and
its request/response messages — no other services, no unrelated types.

```
jwk_get.proto       → JwkGetService + JwkGetRequest + JwkGetResponse
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

Messages use `PascalCase`. Field names always use `snake_case`, never camelCase — the Go
generator converts them to `camelCase` getters (`GetKeyId()`, `GetUsage()`).

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

proto3 requires `_UNSPECIFIED = 0`: unset enum fields default to 0, and the application must be
able to detect "not set". Never assign 0 to a meaningful value.

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
- Once a field is removed, **reserve** its number and name. Reusing the number of a deleted field
  silently corrupts data for clients that still send the old one:
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

Renaming is never the safe refactor it looks like — buf rejects it at `FILE` level. To rename a
field, add a new field with the correct name and a new number, deprecate the old one with a
comment, then remove it in a coordinated release.

---

## Well-Known Types

Prefer proto's well-known types over raw primitives for common data shapes:

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
and must never be imported by `core/`, `dao/`, or `config/`. Handlers own all conversions
between proto types and core types.

| Proto element           | Generated Go                             | Used in                               |
| ----------------------- | ---------------------------------------- | ------------------------------------- |
| `service JwkGetService` | `protogen.JwkGetServiceServer` interface | embedded in `handlers.GrpcJwkGet`     |
| `service JwkGetService` | `protogen.JwkGetServiceClient` interface | `pkg/go/client.go` via gRPC dial      |
| `service JwkGetService` | `protogen.RegisterJwkGetServiceServer`   | `cmd/grpc/main.go`                    |
| `message JwkGetRequest` | `protogen.JwkGetRequest` struct          | handler, converted to service request |
| `enum DependencyStatus` | `protogen.DependencyStatus` const        | handler only                          |

---

## Adding a New RPC: Step-by-Step

1. **Create `internal/models/proto/<entity>_<operation>.proto`** with the service, request,
   and response messages, following the file structure and naming conventions above.
2. **Run `pnpm format:proto`** — formats the file and updates buf.lock.
3. **Run `pnpm lint:proto`** — fix any violations before generating.
4. **Run `pnpm generate:go`** — wipes `protogen/` and regenerates everything.
5. **Create `internal/handlers/grpc.<entity><Operation>.go`** with the new handler type.
   Embed `protogen.Unimplemented<ServiceName>Server`, define the service interface, implement
   the RPC method.
6. **Run `pnpm generate:go`** again if you added a new interface (regenerates mocks).
7. **Wire it up in `cmd/grpc/main.go`**: construct the handler and call
   `protogen.Register<ServiceName>Server(server, handler)`.
8. **Invoke the `document-code` skill** for the new `.proto` file and the new Go handler file.
9. **Invoke the `write-go-tests` skill** to write tests for the new handler.
10. **Run `pnpm format:go`, `pnpm lint:go`, and `a-novel test --type=go -y`** to confirm everything is clean.

---

## Common Pitfalls

**`repeated` on response fields that could be empty.** An empty `repeated` field returns a nil
slice in Go, not an empty slice. Callers must use `GetField()` (nil-safe accessor) rather than
`.Field` directly.

The rules above, as a checklist:

- Omitting `_UNSPECIFIED = 0` from an enum.
- Adding `package` or `option go_package` to a `.proto` file, which managed mode already injects.
- Editing files in `internal/handlers/protogen/`, which the next `pnpm generate:go` overwrites.
- Reusing the field number of a deleted field instead of reserving it.
- Importing `internal/handlers/protogen` from `internal/core` or `internal/dao`.
- Running `pnpm generate:go` before `pnpm format:proto`.
- Renaming a field in place instead of adding a replacement and staging the removal.
