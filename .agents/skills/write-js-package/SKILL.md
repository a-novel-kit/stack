---
name: write-js-package
description: >
  Maintain the JavaScript/TypeScript REST client package (pkg/js) for the JSON-keys service. Use
  whenever creating or editing files under pkg/js/ — the published library (pkg/js/rest/),
  integration tests (pkg/js/test/rest/), or package/build config. Covers API methods, type
  definitions, exports, and test cases.
---

# JS Package Writing Skill

The TypeScript REST client package at `pkg/js/` is the public JS surface for the JSON-keys service:
it mirrors the OpenAPI spec exactly and stays synchronized with the REST handlers.

**Before editing any file**, read the file first. Patterns are consistent by design — follow them
exactly. Read the OpenAPI spec (`openapi.yaml`) for every endpoint you touch; the client contract
must match it.

---

## Package Layout

```
pkg/js/
├── rest/                        # Published npm package (@a-novel/service-json-keys-rest)
│   ├── src/
│   │   ├── index.ts             # Re-exports everything — only file consumers import from
│   │   ├── api.ts               # JsonKeysApi class (HTTP plumbing + system endpoints)
│   │   └── <domain>.ts          # One file per resource domain (e.g., jwk.ts)
│   ├── dist/                    # Build artefacts — never edit manually
│   ├── package.json
│   ├── tsconfig.json            # Base TypeScript config
│   ├── tsconfig.build.json      # Declaration-only build config
│   └── vite.config.ts           # ES module bundle config
│
└── test/rest/                   # Integration test suite (private, not published)
    ├── src/
    │   ├── api.test.ts          # Tests for JsonKeysApi system endpoints
    │   └── <domain>.test.ts     # One test file per domain (mirrors library files)
    ├── package.json
    └── tsconfig.json
```

A new resource domain follows the same split: one `<domain>.ts` source file, one `<domain>.test.ts`
test file. Never merge unrelated domains into a single file.

---

## After Every Edit

Run both targets before the work is done:

```bash
pnpm lint      # check format (Prettier) + lint (ESLint + TypeScript typecheck)
a-novel test --type=pnpm -y    # integration tests against a live containerised service
```

Run `pnpm lint` first, so type errors surface before the heavier integration suite. Fix any Prettier
issue it reports with `pnpm format`, then re-run it.

`a-novel test --type=pnpm -y` orchestrates the full integration environment (container startup, port
allocation, readiness wait) via `scripts/test.pkg.js.sh`. Never run vitest directly for the
integration tests — the script wires up the required environment variables and container lifecycle.

---

## Synchronization Rule

The JS client, the OpenAPI spec (`openapi.yaml`), and the Go REST handlers are three
representations of the same contract. **They must always be changed together.**

When you edit any one of these:

| Changed         | Also update                                                    |
| --------------- | -------------------------------------------------------------- |
| OpenAPI spec    | JS client types + methods, Go handlers                         |
| JS client       | OpenAPI spec (verify parity), Go handlers if behaviour changed |
| Go REST handler | OpenAPI spec, JS client                                        |

A PR that updates one of the three without the others must justify the omission explicitly.
Divergence between the spec, the client, and the handlers is a bug.

---

## Library: `pkg/js/rest/src/`

### `api.ts` — The `JsonKeysApi` Class

`JsonKeysApi` owns all HTTP plumbing. It holds the `_baseUrl` and exposes two primitives:

```typescript
/** Fire-and-forget — discards the response body. Throws on non-2xx. */
async fetchVoid(input: string, init?: RequestInit): Promise<void>

/** JSON response — deserialises body as T. Throws on non-2xx or invalid JSON. */
async fetch<T>(input: string, init?: RequestInit): Promise<T>
```

Domain functions always delegate to one of these two, never to the global `fetch` — a direct `fetch`
call bypasses base-URL composition and error handling.

System endpoints (`/ping`, `/healthcheck`) live in `api.ts` as methods on the class. Resource
endpoints (`/jwks`, `/jwk`) live in separate domain files as standalone functions. The class is
intentionally minimal: a resource endpoint always becomes a standalone function in a domain file,
never a new `JsonKeysApi` method.

### Domain Files (e.g., `jwk.ts`)

Each domain file exports:

- Named type definitions for every request/response shape used in that domain
- Standalone async functions that take `api: JsonKeysApi` as their first argument

Function signatures:

```typescript
export async function jwkList(api: JsonKeysApi, usage?: string): Promise<Jwk[]>;
export async function jwkGet(api: JsonKeysApi, id: string): Promise<Jwk>;
```

**URL construction**: use `URLSearchParams` for query parameters — never interpolate parameters
directly into URL strings.

```typescript
const params = new URLSearchParams();
if (filter) params.set("filter", filter);
const query = params.toString();
return await api.fetch(`/resource${query ? `?${query}` : ""}`, { method: "GET", headers: HTTP_HEADERS.JSON });
```

**Headers**: use constants from `@a-novel-kit/nodelib-browser/http` — `HTTP_HEADERS.JSON` for
endpoints that return JSON, nothing extra for void responses.

**Request body**: pass serialised JSON as the body with `HTTP_HEADERS.JSON` on POST/PUT/PATCH:

```typescript
return await api.fetch("/resource", {
  method: "POST",
  headers: HTTP_HEADERS.JSON,
  body: JSON.stringify(payload),
});
```

### `index.ts` — Exports

Re-export everything from every domain file and from `api.ts`:

```typescript
export * from "./api";
export * from "./jwk";
```

`index.ts` is the single public surface: consumers and tests import from the package root
(`@a-novel/service-json-keys-rest`), never from a file inside it.

A new domain file needs a corresponding `export * from "./<domain>";` line in `index.ts`, or
consumers will not see it.

### TypeScript Rules

- Strict mode is on. All types must be explicit — no `any`. The `Jwk` type uses
  `[key: string]: unknown` for algorithm-specific fields; that is `unknown`, not `any`.
- Use `type` imports (`import type { ... }`) for types used only in type positions.
- Use `type` for domain object shapes, unions, intersections, and aliases — the domain types are
  sealed contracts, not designed for inheritance.
- ES modules only — no CommonJS (`require`, `module.exports`).
- Target: ESNext. No polyfills; the published package targets Node ≥ 23 and modern browsers.

### JSDoc

Every exported type, class, method, and function needs a JSDoc comment explaining _what it does and
why_. The published package is consumed by other services, and these tooltips are their first
documentation. Include:

- A first-line summary sentence (shown in IDE tooltips).
- Descriptions for every non-obvious parameter.
- A note on error conditions (what HTTP status causes a throw), if the function can fail on a specific HTTP status.

```typescript
/**
 * Returns all active public keys for the given usage.
 *
 * A usage identifies a named signing configuration (e.g., `"auth"`, `"auth-refresh"`).
 * Omitting `usage`, or passing an unrecognized value, returns an empty list.
 */
export async function jwkList(api: JsonKeysApi, usage?: string): Promise<Jwk[]>;
```

Do not repeat what is already clear from the type signature alone (e.g., "takes a string and
returns a promise").

---

## Tests: `pkg/js/test/rest/src/`

### Structure

These are integration tests against a live service. The package holds no unit tests and no mocks —
the point is to verify the real HTTP contract.

Each test file mirrors a library source file:

| Library file      | Test file                   |
| ----------------- | --------------------------- |
| `rest/src/api.ts` | `test/rest/src/api.test.ts` |
| `rest/src/jwk.ts` | `test/rest/src/jwk.test.ts` |

### Imports

```typescript
import { describe, expect, it } from "vitest";

import { expectStatus } from "@a-novel-kit/nodelib-test/http";
import { JsonKeysApi, jwkGet, jwkList } from "@a-novel/service-json-keys-rest";
```

Always import from the published package name (`@a-novel/service-json-keys-rest`), never from
relative paths: the workspace symlink that resolves it is part of what these tests verify.

### Test File Layout

One `describe` block per exported function or class method:

```typescript
describe("jwkList", () => {
  it("returns keys for a known usage", async () => { ... });
  it("returns an empty list when usage is omitted", async () => { ... });
  it("returns an empty list for an unrecognized usage", async () => { ... });
  it("filters by usage", async () => { ... });
});

describe("jwkGet", () => {
  it("returns 400 for invalid ID format", async () => { ... });
  it("returns 404 for non-existent key", async () => { ... });
  it("retrieves an existing key by ID", async () => { ... });
});
```

Test names are plain sentences describing the observable behaviour — `"returns keys for a known
usage"`, `"returns 404 for non-existent key"` — not the `"Success"` / `"Error/X"` convention used in
Go tests.

### Instantiation

Always construct `JsonKeysApi` from `process.env.REST_URL!`, which the test script sets:

```typescript
const api = new JsonKeysApi(process.env.REST_URL!);
```

Never hard-code URLs, ports, or base paths.

### Test Environment Usages

The integration test container runs `rotate-keys` at startup, seeding keys for the two default
usages defined in `internal/config/jwks.config.yaml`:

- `"auth"` — short-lived access token keys (EdDSA)
- `"auth-refresh"` — long-lived refresh token keys (EdDSA)

Tests needing actual key data must pass one of these usages. `jwkList(api)` without a usage always
returns `[]`, because the server queries `WHERE usage = ""`.

### Asserting Success

For void responses:

```typescript
await expect(api.ping()).resolves.toBeUndefined();
```

For JSON responses, use a known usage to ensure the live service returns actual data:

```typescript
const keys = await jwkList(api, "auth");
expect(keys.length).toBeGreaterThan(0);
for (const key of keys) {
  expect(key.kty).toBeTruthy();
  expect(key.kid).toBeTruthy();
}
```

When testing a retrieve-by-id operation, first list with a known usage to get a real ID:

```typescript
const keys = await jwkList(api, "auth");
expect(keys.length).toBeGreaterThan(0);
const key = await jwkGet(api, keys[0].kid);
expect(key.kid).toBe(keys[0].kid);
```

Assert `length > 0` explicitly rather than guarding with an early return like
`if (keys.length === 0) return`: such a guard silently skips every assertion when the container
failed to seed, hiding a real failure.

### Asserting HTTP Errors

`expectStatus` from `@a-novel-kit/nodelib-test/http` is the canonical way to assert an expected HTTP
error code — never `try/catch` with manual status inspection:

```typescript
await expectStatus(jwkGet(api, "not-a-uuid"), 400);
await expectStatus(jwkGet(api, "00000000-0000-0000-0000-000000000000"), 404);
```

### What to Test

For every exported function, cover:

- The happy path (valid input, expected response shape)
- Each documented error case (400 bad request, 404 not found, etc.)
- Optional parameters: one test with the parameter absent, one with it set (when filtering changes
  the result meaningfully)

Do not test internal implementation details (URL construction, header names). Test the observable
contract: what goes in, what comes out, which HTTP errors are surfaced.

---

## Common Pitfalls

Every entry is stated in full above; this list is the review checklist.

- Importing from a relative path in a test instead of `@a-novel/service-json-keys-rest`.
- Running vitest directly: `pnpm test` alone skips container setup and fails on a missing `REST_URL`.
- Adding a domain file without its `export *` line in `index.ts`.
- Diverging from the OpenAPI spec on URL paths, query parameter names, response field names, or HTTP
  methods — they must match it exactly.
- Calling global `fetch` instead of `api.fetch` / `api.fetchVoid`.
- Skipping JSDoc on an exported symbol.
- Using `any` (the `Jwk` index signature is `unknown`, and every other type is explicit).
