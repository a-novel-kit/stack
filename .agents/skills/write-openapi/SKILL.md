---
name: write-openapi
description: >
  Write, review, and maintain the OpenAPI 3.1 specification for Agora backend services.
  Use this skill whenever editing openapi.yaml — adding endpoints, parameters, schemas,
  responses, or updating descriptions and examples. Applies to the REST public API only;
  gRPC contracts are governed by the write-proto skill.
---

# OpenAPI Specification Skill

This skill governs how to write and maintain `openapi.yaml` in Agora backend services.
The spec is the public contract for the REST API — it is consumed by documentation
generators, client code generators, and API testing tools. Treat every field name,
type, and status code as a durable commitment once published.

**Before touching `openapi.yaml`**, read the entire file. Read the Go handler code for
every endpoint you are about to change — the spec must match exactly what the server
actually returns, not what you think it should return. When in doubt, look at the
handler source.

---

## After Every Edit

Run these in order after any change to `openapi.yaml`:

```bash
pnpm format         # runs Prettier over all files
pnpm lint:openapi   # validates the spec with Redocly
```

Never ship a change that fails `pnpm lint:openapi`. Warnings are not errors, but
document any known, intentional warnings (see the Suppressed Warnings section below).

Then check the JS client in `pkg/js/rest/src/` to see if any TypeScript types need
updating to reflect the spec change. Run `pnpm lint:typecheck` to confirm.

---

## Project Layout

```
openapi.yaml         # The single-file OpenAPI 3.1 spec (edit this)
pkg/js/rest/src/     # JS client that must stay in sync with the spec
```

There is no multi-file splitting — everything lives in `openapi.yaml`. Use `$ref` to
reference reusable components defined under `components/` in the same file:

```yaml
$ref: "#/components/schemas/jwk"
$ref: "#/components/responses/notFound"
$ref: "#/components/parameters/jwkID"
```

---

## Toolchain

Linting is done with **Redocly CLI** via `pnpm redocly lint openapi.yaml`. A `redocly.yaml`
at the project root extends the `recommended` ruleset with project-specific overrides.
The `recommended` ruleset is opinionated but not maximally strict — some rules produce warnings
rather than errors.

To change rule overrides, edit `redocly.yaml`:

```yaml
extends: [recommended]
rules:
  operation-4xx-response: off # suppress for endpoints with no 4xx
```

---

## Versioning

The spec version (`info.version`) must match `package.json` `"version"` and is updated
automatically by the publish scripts. **Never change `info.version` by hand.**

---

## Breaking vs Non-Breaking Changes

The REST API is public. Any change that breaks existing callers is a breaking change,
which requires a major version bump and coordination across consuming services.

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

Do **not** use `nullable: true` — that is a 3.0 extension removed in 3.1. Use type
unions instead:

```yaml
# WRONG (3.0 style):
type: string
nullable: true

# CORRECT (3.1):
type: [string, "null"]
```

### Examples

Three placement options exist, from most to least specific:

1. **Media-type level** (`content.{media}.examples`) — named examples object; most
   flexible; Redocly renders all of them. Use when you need multiple named examples for
   an operation response.

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
   typically rendered. Use for property-level inline examples or when a single example
   is sufficient.

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
value of `additionalProperties`. This tells generators the value type without listing every key:

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
`additionalProperties` covers any future keys added without a spec change. This pattern avoids
the common mistake of listing `additionalProperties: true` on a map that actually has typed
values — generators treat `true` as `any`, losing type safety.

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

`operationId` values must be unique across the entire spec. They are used by code
generators to name functions — treat them like function names.

### Schema names

`PascalCase` for reusable schemas under `components/schemas`:

| Name           | Use                           |
| -------------- | ----------------------------- |
| `Jwk`          | A public JSON Web Key         |
| `JwkId`        | The UUID identifier of a key  |
| `HealthStatus` | Status of a single dependency |

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
- `description` — markdown allowed; used for nuance, caveats, and edge-case behavior.
  Always use a YAML block scalar (`|`) for multi-line descriptions.

**Examples** — every schema property that has a known range of values must include
`examples`. Parameters that accept a finite set of identifiers (like `usage`) must
show real values from the server config, not placeholder values.

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
  in `internal/config/jwks.config.yaml` (`auth`, `auth-refresh`).

**The spec does not define handler behavior — it describes it.** If the spec and the
handler disagree, fix the spec (or the handler if it is wrong), but never let them drift.

---

## Response Design

### Error responses

Use a `default` response for any status code not explicitly listed. Use specific 4xx
responses only for error conditions the handler explicitly checks and returns.

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
it. A 400 from `/jwk` (invalid UUID) is a real handler path. A 400 from `/jwks` does
not exist — the handler accepts any string for `usage` and returns an empty list if
there are no matching keys.

### The `default` response

Every operation must have a `default` response. It catches any status code not
explicitly listed — it represents internal errors and unexpected conditions.

### Reuse response components

Do not inline response schemas. Define all responses under `components/responses` and
reference them:

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

**Reason:** none of these handlers return 4xx responses by design:

- `/ping` — always returns 200 (liveness check only).
- `/healthcheck` — always returns 200 regardless of dependency status (callers read the body to assess health).
- `/jwks` — accepts any string for `usage` and returns an empty list for an unrecognized usage; it never validates input with a 400.

Do not add spurious 4xx responses to silence these warnings — that would document behavior
the server does not have.

These warnings are suppressed by the `redocly.yaml` at the project root:

```yaml
extends: [recommended]
rules:
  operation-4xx-response: off
```

---

## Common Pitfalls

- **Using `nullable: true`.** This is a 3.0 extension. In 3.1, use `type: [string, "null"]`.
- **Wrong `usage` examples.** The `usage` parameter takes named signing configuration
  identifiers (`auth`, `auth-refresh`), NOT JWK `use` values (`sig`, `enc`). Always
  verify examples match the keys in `internal/config/jwks.config.yaml`.
- **Documenting 4xx responses the handler cannot return.** Check the handler code before
  adding any error status code to the spec.
- **Inline response schemas instead of `$ref`.** Always extract reusable schemas to
  `components/` and reference them.
- **Changing `operationId`.** JS and Go client generators depend on these names. Renaming
  is breaking. Add a new operation instead if a rename is truly needed.
- **Using 3.0 `example` (singular) on new schemas.** Use the 3.1 `examples` array form.
- **Forgetting to update `pkg/js/rest/src/` types** after changing a response schema.
  Run `pnpm lint:typecheck` to catch drift.
- **Adding a required parameter to an existing endpoint.** This is a breaking change
  even if it has a sensible default — old callers that omit it will now receive errors.
- **Bumping `info.version` manually.** The publish scripts manage this. Editing it by
  hand causes the YAML and `package.json` to diverge.
- **Single-item `enum` instead of `const`.** In 3.1, `const: sig` is cleaner and more
  semantically correct than `enum: [sig]`.
- **Omitting `additionalProperties` on closed schemas.** Generators assume `true` by
  default; set `false` explicitly on schemas where extra fields should never appear.
