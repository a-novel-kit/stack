---
name: write-openapi
description: >
  Write, review, and maintain the OpenAPI 3.1 specification for Agora backend services.
  Use whenever editing openapi.yaml — adding endpoints, parameters, schemas, responses,
  or updating descriptions and examples. Covers the REST public API only; gRPC contracts
  belong to the write-proto skill.
---

# OpenAPI Specification Skill

`openapi.yaml` is the public contract for the REST API, consumed by documentation generators,
client code generators, and API testing tools. Every field name, type, and status code is a
durable commitment once published.

**Before touching `openapi.yaml`**, read the entire file, and read the Go handler code for
every endpoint you are about to change. The spec must match exactly what the server returns,
not what you think it should return.

---

## After Every Edit

Run these in order after any change to `openapi.yaml`:

```bash
pnpm format         # runs Prettier over all files
pnpm lint:openapi   # validates the spec with Redocly
```

Never ship a change that fails `pnpm lint:openapi`. Warnings are not errors, but document
any known, intentional one (see Suppressed Warnings).

Then update the TypeScript types in the JS client `pkg/js/rest/src/` to match the spec
change, and run `pnpm lint:typecheck` to confirm.

---

## Project Layout

```
openapi.yaml         # The single-file OpenAPI 3.1 spec (edit this)
pkg/js/rest/src/     # JS client that must stay in sync with the spec
```

Everything lives in `openapi.yaml`; there is no multi-file splitting. Use `$ref` for
reusable components defined under `components/` in the same file:

```yaml
$ref: "#/components/schemas/jwk"
$ref: "#/components/responses/notFound"
$ref: "#/components/parameters/jwkID"
```

---

## Toolchain

Linting runs **Redocly CLI** via `pnpm redocly lint openapi.yaml`. A `redocly.yaml` at the
project root extends the `recommended` ruleset with project-specific overrides; `recommended`
is opinionated but not maximally strict, and some of its rules warn rather than error.

Rule overrides live in `redocly.yaml`:

```yaml
extends: [recommended]
rules:
  operation-4xx-response: off # suppress for endpoints with no 4xx
```

---

## Versioning

The spec version (`info.version`) must match `package.json` `"version"` and is updated
automatically by the publish scripts. **Never change `info.version` by hand** — a manual
edit diverges the YAML from `package.json`.

---

## Breaking vs Non-Breaking Changes

The REST API is public, so any change that breaks existing callers needs a major version
bump and coordination across consuming services.

### Breaking — never do without a major version bump

| Change                                                  | Why it breaks callers                                 |
| ------------------------------------------------------- | ----------------------------------------------------- |
| Remove an endpoint (`/jwk`, `/jwks`, etc.)              | Callers get 404                                       |
| Remove or rename a required response field              | Callers accessing the field get `undefined`/panic     |
| Change a field's type (e.g., `string` → `integer`)      | Callers fail to deserialize                           |
| Change a `2xx` status code (e.g., 200 → 201)            | Callers checking the code break                       |
| Add `required: true` to a previously optional parameter | Callers omitting it now get 400                       |
| Rename an `operationId`                                 | Code generators and clients using `operationId` break |
| Remove a named `$ref` component that is used            | Generated clients break                               |

### Non-breaking — always safe

| Change                                 | Notes                                         |
| -------------------------------------- | --------------------------------------------- |
| Add a new endpoint                     | Old clients never call it                     |
| Add an optional response field         | Old clients ignore unknown fields             |
| Add a new optional query parameter     | Old callers don't send it; server still works |
| Add a new error status code (4xx, 5xx) | Old callers already handle unknown errors     |
| Change a description or example        | No runtime impact                             |
| Add a new `$ref` component             | Unused until referenced                       |
| Mark a field `deprecated: true`        | Signals intent without breaking callers       |

**Deprecation process:** add `deprecated: true` to the field or operation, add a
description note explaining the replacement, then remove in the next major version. Never
remove a deprecated item in the same release it was deprecated.

---

## OpenAPI 3.1 Specifics

This project uses OpenAPI **3.1**, which aligns fully with JSON Schema Draft 2020-12.
Key differences from 3.0 that affect writing this spec:

### Nullable fields

Do **not** use `nullable: true`, a 3.0 extension removed in 3.1. Use type unions instead:

```yaml
# WRONG (3.0 style):
type: string
nullable: true

# CORRECT (3.1):
type: [string, "null"]
```

### Examples

Three placement options exist, from most to least specific:

1. **Media-type level** (`content.{media}.examples`) — named examples object, the most
   flexible; Redocly renders all of them. Use for multiple named examples on an operation
   response.

   ```yaml
   content:
     application/json:
       examples:
         active:
           value: { "status": "up" }
         degraded:
           value: { "status": "down", "err": "connection refused" }
   ```

2. **Schema level** (`schema.examples`) — JSON Schema array; only the first item is
   typically rendered. Use for property-level inline examples, or when one example is enough.

   ```yaml
   schema:
     type: string
     examples: [auth, auth-refresh]
   ```

3. **Schema `example`** (singular, 3.0 compat) — single value, lower priority than the
   array form. Avoid in new code; use `examples` instead.

**Rule:** prefer media-type level `examples` for full response/request bodies. Use
schema-level `examples` (array) for individual parameters and schema properties.

### `const` and `enum`

Prefer `const` over single-value `enum` arrays in 3.1:

```yaml
# WRONG (verbose):
enum: [sig]

# CORRECT (3.1):
const: sig
```

Use `enum` when there are two or more values.

### `additionalProperties`

Default is `true` (any extra properties allowed). Be explicit:

- Use `additionalProperties: true` for schemas that intentionally allow algorithm-specific
  extra fields (like `jwk`, which carries EdDSA `x`/`crv`, RSA `n`/`e`, etc.).
- Use `additionalProperties: false` for schemas where no extra fields should be present.
- Never omit it for top-level response/request schemas — silence is ambiguous to code
  generators.

**Typed `additionalProperties` for dynamic-key objects.** When a response is a map whose keys
are dynamic but whose values all share a single shape, use a `$ref` (or inline schema) as the
value of `additionalProperties`. Generators then know the value type without every key listed:

```yaml
health:
  type: object
  required: ["client:postgres"]
  additionalProperties:
    $ref: "#/components/schemas/healthStatus" # every key maps to this type
  properties:
    "client:postgres": # explicitly document known keys
      $ref: "#/components/schemas/healthStatus"
```

The `properties` block calls out the key that must always be present (matched by `required`);
`additionalProperties` covers future keys added without a spec change. On a map with typed
values, `additionalProperties: true` reads as `any` to generators and loses type safety.

---

## Naming Conventions

### Paths

Use lowercase `kebab-case` for path segments. This project uses flat paths (`/jwk`,
`/jwks`, `/ping`, `/healthcheck`) — follow this pattern for new endpoints.

### `operationId`

Use `camelCase`. Follows the pattern `<entity><Operation>` for resource operations:

| Operation   | `operationId` |
| ----------- | ------------- |
| List JWKs   | `jwkList`     |
| Get one JWK | `jwkGet`      |
| Ping        | `ping`        |
| Healthcheck | `healthcheck` |

`operationId` values must be unique across the entire spec. Code generators use them to name
functions, so treat them like function names. Renaming one is breaking; add a new operation
instead if a rename is truly needed.

### Schema names

`camelCase` for reusable schemas under `components/schemas`, matching the `$ref` keys the fleet's
specs already use:

| Name           | Use                           |
| -------------- | ----------------------------- |
| `jwk`          | A public JSON Web Key         |
| `jwkID`        | The UUID identifier of a key  |
| `healthStatus` | Status of a single dependency |

An initialism keeps its casing after the first segment (`jwkID`, not `jwkId`).

### Parameter names

`camelCase` for the `$ref` key under `components/parameters`; `snake_case` for the
actual `name` in the query string (to match what the Go handler reads with
`r.URL.Query().Get("id")`):

```yaml
components:
  parameters:
    jwkID: # camelCase $ref key
      name: id # snake_case query param name
      in: query
```

### Response names

`camelCase`:

```yaml
components:
  responses:
    jwkList:
    jwkGet:
    notFound:
    badRequest:
    internalError:
```

### Tags

Use lowercase single-word tags to group operations in documentation: `health`, `jwk`.
Define tags at the top level under `tags:` with descriptions if more than two exist.

---

## Documentation Requirements

Every element must be documented. No exceptions.

| Element                     | Required                                                                          |
| --------------------------- | --------------------------------------------------------------------------------- |
| `info.description`          | Multi-line markdown; explain the service's purpose and scope                      |
| Each path operation         | `summary` (one line) + `description` (markdown, explains behavior and edge cases) |
| Each parameter              | `description` explaining what the value controls                                  |
| Each response               | `description` — one sentence is sufficient for error responses                    |
| Each schema                 | `description`                                                                     |
| Each schema property        | `description`                                                                     |
| Each named `$ref` component | Documented inside the component definition                                        |

**Summary vs description:**

- `summary` — one sentence, no markdown, shown in navigation/tooling.
- `description` — markdown allowed; carries nuance, caveats, and edge-case behavior.
  Always use a YAML block scalar (`|`) for multi-line descriptions.

**Examples** — every schema property with a known range of values must include `examples`.
Parameters that accept a finite set of identifiers (like `usage`) must show real values from
the server config, not placeholders.

---

## Alignment with the Go Handler Layer

The spec must exactly mirror what the Go handlers return. Rules:

- Every `responses` status code in the spec must correspond to a reachable code path
  in the handler. If the handler can never return 422, do not document 422.
- Every response schema field must correspond to a field the handler actually serializes.
  Check the JSON tags on the Go struct.
- Parameter `required: true` must match whether the handler actually validates and
  rejects missing inputs with 400.
- The `usage` parameter values documented as examples must match the registered usages
  in `internal/config/jwks.config.yaml` (`auth`, `auth-refresh`). Those are named signing
  configuration identifiers, not JWK `use` values (`sig`, `enc`).

**The spec does not define handler behavior — it describes it.** If the spec and the
handler disagree, fix the spec (or the handler if it is wrong), but never let them drift.

---

## Response Design

### Error responses

Use specific 4xx responses only for error conditions the handler explicitly checks and
returns; everything else falls to the `default` response.

```yaml
responses:
  "200":
    $ref: "#/components/responses/jwkGet"
  "400":
    $ref: "#/components/responses/badRequest" # only if the handler validates input
  "404":
    $ref: "#/components/responses/notFound" # only if the handler returns 404
  default:
    $ref: "#/components/responses/internalError"
```

Do not document a 4xx response unless the handler code has an explicit path that returns
it. A 400 from `/jwk` (invalid UUID) is a real handler path; a 400 from `/jwks` does not
exist (see Suppressed Warnings).

### The `default` response

Every operation must have a `default` response. It catches any status code not explicitly
listed: internal errors and unexpected conditions.

### Reuse response components

Define all responses under `components/responses` and reference them; never inline a
response schema:

```yaml
responses:
  "404":
    $ref: "#/components/responses/notFound" # NOT inline
```

---

## Suppressed Warnings

Three operations intentionally have no 4xx responses. Without `operation-4xx-response: off`
in `redocly.yaml`, Redocly would flag them with:

```
openapi.yaml: Operation must have at least one `4XX` response. [operation-4xx-response]
Affected: /ping GET, /healthcheck GET, /jwks GET
```

None of these handlers returns a 4xx, by design:

- `/ping` — always returns 200 (liveness check only).
- `/healthcheck` — always returns 200 regardless of dependency status (callers read the body to assess health).
- `/jwks` — accepts any string for `usage` and returns an empty list for an unrecognized usage; it never validates input with a 400.

Do not add spurious 4xx responses to silence these warnings — that would document behavior
the server does not have.

The suppression in the root `redocly.yaml`:

```yaml
extends: [recommended]
rules:
  operation-4xx-response: off
```

---

## Common Pitfalls

Each of these is covered above; run the list as a checklist before shipping an edit.

- `nullable: true` instead of a 3.1 type union
- `usage` examples taken from JWK `use` values instead of `internal/config/jwks.config.yaml`
- A 4xx documented on a path the handler never returns
- An inline response schema instead of a `components/responses` `$ref`
- A renamed `operationId`
- 3.0 singular `example` on a new schema
- `pkg/js/rest/src/` types left stale after a response-schema change
- A new required parameter on an existing endpoint
- A hand-edited `info.version`
- Single-item `enum` where `const` fits
- `additionalProperties` omitted on a closed schema
