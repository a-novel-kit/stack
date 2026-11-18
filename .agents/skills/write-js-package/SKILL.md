---
name: write-js-package
description: >
  Write or review service JavaScript/TypeScript REST clients under pkg/js, including exports,
  package/build configuration, and integration tests. Also load write-frontend.
---

# JS Package Writing Skill

Every backend service publishes a TypeScript REST client from `pkg/js/`. It is the service's public
JS surface: it mirrors the OpenAPI spec exactly and stays synchronized with the REST handlers.

**Before editing any file**, read the file first. Every service is scaffolded from
`service-template` and the layout is identical across all of them, so follow the patterns you find
rather than inventing a local variant. Read the OpenAPI spec (`openapi.yaml`) for every endpoint you
touch; the client contract must match it.

---

## Package Layout

```
pkg/js/
├── rest/                        # Published npm package (@a-novel/<service-slug>-rest)
│   ├── src/
│   │   ├── index.ts             # Re-exports everything — only file consumers import from
│   │   ├── api.ts               # <Service>Api class (RestClient + system endpoints)
│   │   └── <domain>.ts          # One file per resource domain (e.g., item.ts)
│   ├── dist/                    # Build artefacts — never edit manually
│   ├── package.json
│   ├── tsconfig.json            # Base TypeScript config, NodeNext resolution
│   ├── tsconfig.build.json      # Declaration-only build config
│   └── vite.config.ts           # ES module bundle config
│
└── test/rest/                   # Integration test suite (private, not published)
    ├── src/
    │   ├── api.test.ts          # Tests for <Service>Api system endpoints
    │   └── <domain>.test.ts     # One test file per domain (mirrors library files)
    ├── package.json
    └── tsconfig.json
```

Every name derives from the repo slug, so a reader can predict all of them from the repo alone: the
published package is `@a-novel/<service-slug>-rest`, the private test project is
`<service-slug>-rest-test-project`, and the client class is the slug in PascalCase without its
`service-` prefix — `service-narrative-engine` gives `NarrativeEngineApi`. Both packages are pnpm
workspace members, listed in `pnpm-workspace.yaml` and in the root `package.json` `workspaces`
array. The test project declares no dependency of its own, workspace links included: the root
`package.json`, which Renovate watches, declares everything it imports, the client as `workspace:*`
too. Renovate's `:ignoreModulesAndTests` preset skips `**/test/**`, so a version pinned there would
never move, and one form for every dependency holds across the fleet.

`vite.config.ts` is one call to `ViteLibrary` from `@a-novel-kit/nodelib-config/vite-library`:

```ts
import manifest from "./package.json" with { type: "json" };

import { ViteLibrary } from "@a-novel-kit/nodelib-config/vite-library";

export default ViteLibrary(import.meta.dirname, { src: "src/index.ts" }, manifest);
```

It keeps every dependency and peer out of the bundle, subpaths included. Rollup matches a plain
`external` list by exact specifier, so a list of package names bundles
`@a-novel-kit/nodelib-browser/http` while excluding `@a-novel-kit/nodelib-browser`. The client then
carries a stale copy of the peer's classes, and `instanceof` checks and new fields silently diverge
from the consumer's version.

A new resource domain follows the same split: one `<domain>.ts` source file, one `<domain>.test.ts`
test file. Never merge unrelated domains into a single file.

A service whose tests need helpers that other repos also use publishes them as a third package,
`pkg/js/rest-test/` (`@a-novel/<service-slug>-rest-test`). Helpers used only by this repo's own
suite stay beside the tests that use them.

`service-authentication` ships one with the sign-up helpers other suites import. The suite's vitest
`globalSetup` calls `registerSuperAdmin`, and a test signs in with `superAdminAccessToken`,
`userAccessToken` or `anonymousAccessToken`, each taking an `AuthenticationApi` client. A suite that
signs callers in imports them instead of copying them. Any other Mailpit read uses `searchMail` and
`mailLinks` from `@a-novel-kit/nodelib-test/mailpit`, polling with `vi.waitFor`.

---

## After Every Edit

Run both targets before the work is done:

```bash
pnpm lint:js                   # Prettier check + TypeScript typecheck + ESLint + spec lint
a-novel test --type=pnpm -y    # integration tests against a live containerised service
```

Run `pnpm lint:js` first, so type errors surface before the heavier integration suite. Fix any
Prettier issue it reports with `pnpm format:js`, then re-run it.

`a-novel test --type=pnpm -y` brings up the whole integration environment — container startup, port
allocation, readiness wait — and exports the `REST_URL` the tests read. Never run vitest directly
for the integration tests: nothing starts the service, and every test fails on an unset `REST_URL`.

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

### `api.ts` — The `<Service>Api` Class

`<Service>Api` extends `RestClient` from `@a-novel-kit/nodelib-browser/http`, which owns the HTTP
plumbing (`fetch`, `fetchVoid`, `fetchBlob`, `fetchResponse`) and takes an optional `fetcher`. The
subclass adds only `ping()` and `health()` for its API version, typed with nodelib's
`HealthDependency`, which the package re-exports with `export type { HealthDependency } from`:

```typescript
import { type HealthDependency, RestClient } from "@a-novel-kit/nodelib-browser/http";

export class TemplateApi extends RestClient {
  async ping(): Promise<void> { ... }
  async health(): Promise<Record<string, HealthDependency>> { ... }
}
```

The constructor takes the base URL, without a trailing slash. A server-side caller also passes a
`fetcher`, which wraps `fetch` to add its service token or a deadline. Every method takes a path
under the base URL. `fetch` decodes the JSON body through the Zod schema it is given, `fetchVoid`
discards the body, and `fetchBlob` reads it as a `Blob`; each throws an `HttpError` on a non-2xx
status. `fetchResponse` returns the response whatever its status. The client's `package.json` sets
its `@a-novel-kit/nodelib-browser` peer to `^2.13.0`, the first release with `RestClient`. A client
that needs a later nodelib feature narrows the peer to the first release that has it: Renovate
never narrows a peer range.

Domain functions always call one of these methods, never the global `fetch`: a direct `fetch` call
bypasses base-URL composition, response validation, and error handling.

System endpoints (`/ping`, `/healthcheck`) live in `api.ts` as methods on the class: `ping()`
resolves once the service is reachable, and `health()` returns `Record<string, HealthDependency>`
keyed by dependency name. Resource endpoints live in separate domain files as standalone functions.
The class is intentionally minimal: a resource endpoint always becomes a standalone function in a
domain file, never a new `<Service>Api` method.

### Domain Files (`<domain>.ts`)

Each domain file exports:

- A Zod schema for every request and response shape used in that domain, and the type inferred from
  it (`export type Item = z.infer<typeof ItemSchema>`)
- Standalone async functions that take `api: <Service>Api` as their first argument, named
  `<domain><Verb>` — `itemCreate`, `itemList`, `tokenRefresh`

Function signatures, from `service-template`'s `item.ts`:

```typescript
export async function itemGet(api: TemplateApi, id: string): Promise<Item>;
export async function itemList(api: TemplateApi, limit?: number, offset?: number): Promise<ItemPage>;
```

A domain file may also hold schemas and constants the other domains share rather than a resource of
its own — `service-authentication` keeps its field bounds in `const.ts` and the validators built
from them in `form.ts`.

**Response validation**: pass the response schema as the second argument to `api.fetch`. A paged
list passes a page schema, such as `ItemPageSchema`, which holds `z.array(ItemSchema)` beside the
total. Omit it only where the service defines no shape to validate, such as the healthcheck map.

**URL construction**: use `URLSearchParams` for query parameters — never interpolate parameters
directly into URL strings.

```typescript
const params = new URLSearchParams();
if (filter) params.set("filter", filter);
const query = params.toString();
return await api.fetch(`/resource${query ? `?${query}` : ""}`, ResourceSchema, { method: "GET" });
```

**Headers and body**: pass `accessToken` to send the bearer token, and `json` to send a JSON body
with its `Content-Type`. A request without a body, such as a `GET` or a `DELETE`, sends no
`Content-Type`. A call with another content type, such as a form-encoded OAuth request, passes its
own `headers` beside `accessToken`, and `body` carries a body that is not JSON, such as form data or
a blob. Never import `HTTP_HEADERS` (deprecated) or write `Authorization` by hand.

```typescript
return await api.fetch("/resource", ResourceSchema, {
  method: "PUT",
  accessToken,
  json: request,
});
```

### `index.ts` — Exports

Re-export everything from every domain file and from `api.ts`:

```typescript
export * from "./api.js";
export * from "./item.js";
```

`index.ts` is the single public surface: consumers and tests import from the package root
(`@a-novel/<service-slug>-rest`), never from a file inside it.

A new domain file needs a corresponding `export * from "./<domain>.js";` line in `index.ts`, or
consumers will not see it.

Every relative import and re-export carries `.js`, as `import type { TemplateApi } from "./api.js"`,
and the client's `tsconfig.json` sets `"module": "NodeNext"` and `"moduleResolution": "NodeNext"`,
so a missing extension fails the typecheck. The declarations keep those specifiers, and a consumer
on `nodenext` resolution follows them. Under `Bundler`, an extensionless `export * from "./api"`
type-checks in the client and exports nothing to that consumer.

### TypeScript Rules

- Strict mode is on. All types must be explicit — no `any`. A field whose members the service does
  not fix, such as the algorithm-specific members of a key, is `[key: string]: unknown`.
- Derive a domain type from its Zod schema with `z.infer` instead of declaring the shape twice. The
  schema is what validates the response, so a hand-written type drifts from it silently.
- Use `type` imports (`import type { ... }`) for types used only in type positions.
- Use `type` for domain object shapes, unions, intersections, and aliases — the domain types are
  sealed contracts, not designed for inheritance. A closed set of server values can be an `enum`
  instead, so consumers get named members (`Role` and `Lang` in `service-authentication`).
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
/** Lists a page of Items newest first, with their total; 100 at most when limit is omitted. */
export async function itemList(api: TemplateApi, limit?: number, offset?: number): Promise<ItemPage>;
```

Do not repeat what is already clear from the type signature alone (e.g., "takes a string and
returns a promise").

---

## Tests: `pkg/js/test/rest/src/`

### Structure

These are integration tests against a live service. The package holds no unit tests and no mocks —
the point is to verify the real HTTP contract.

Each test file mirrors a library source file:

| Library file           | Test file                        |
| ---------------------- | -------------------------------- |
| `rest/src/api.ts`      | `test/rest/src/api.test.ts`      |
| `rest/src/<domain>.ts` | `test/rest/src/<domain>.test.ts` |

### Imports

```typescript
import { describe, expect, it } from "vitest";

import { expectStatus } from "@a-novel-kit/nodelib-test/http";
import { TemplateApi, itemGet, itemList } from "@a-novel/service-template-rest";
```

Always import from the published package name (`@a-novel/<service-slug>-rest`), never from relative
paths: the tests then exercise the same surface a consumer gets. The root `vitest.config.ts` aliases
that name onto `pkg/js/rest/src/index`, so the import resolves to source with no build step.

### Test File Layout

One `describe` block per exported function or class method:

```typescript
describe("itemGet", () => {
  it("retrieves an existing item", async () => { ... });
  it("returns 400 for invalid ID format", async () => { ... });
  it("returns 404 for non-existent item", async () => { ... });
});

describe("itemList", () => {
  it("returns a page of items with their total", async () => { ... });
  it("pages newest first by offset", async () => { ... });
  it("returns an empty page past the end", async () => { ... });
});
```

Test names are plain sentences describing the observable behaviour — `"retrieves an existing item"`,
`"returns 404 for non-existent item"` — not the `"Success"` / `"Error/X"` convention used in Go
tests.

### Instantiation

Always construct `<Service>Api` from `process.env.REST_URL!`, which the test runner sets:

```typescript
const api = new TemplateApi(process.env.REST_URL!);
```

Never hard-code URLs, ports, or base paths.

### Test Data

A test that reads a record needs that record to exist. Create it through the client, assert against
it, then delete it, so the suite leaves the service as it found it and stays independent of run
order. Take every ID from a record the test created or listed; the only literal IDs are the
deliberately malformed and the deliberately absent ones used to assert 400 and 404.

Data the REST API cannot create is seeded by the integration container at startup: a service whose
records come from a job rather than an endpoint runs that job there, so the tests reading them have
something to read. Those fixtures are defined in the service's own config, so read it to learn which
values exist rather than assuming any.

### Asserting Success

For void responses:

```typescript
await expect(api.ping()).resolves.toBeUndefined();
```

For JSON responses, assert on the record the test set up:

```typescript
const created = await itemCreate(api, "item to get");
const item = await itemGet(api, created.id);
expect(item.id).toBe(created.id);
await itemDelete(api, created.id);
```

A list endpoint whose assertions loop over the response must first assert that the response holds
records:

```typescript
const { items } = await itemList(api);
expect(items.length).toBeGreaterThan(0);
for (const item of items) {
  expect(item.id).toBeTruthy();
}
```

Assert `length > 0` explicitly rather than guarding with an early return like
`if (items.length === 0) return`: such a guard silently skips every assertion when the container
failed to seed, hiding a real failure.

### Asserting HTTP Errors

`expectStatus` from `@a-novel-kit/nodelib-test/http` is the canonical way to assert an expected HTTP
error code — never `try/catch` with manual status inspection:

```typescript
await expectStatus(itemGet(api, "not-a-uuid"), 400);
await expectStatus(itemGet(api, "00000000-0000-0000-0000-000000000000"), 404);
```

### What to Test

For every exported function, cover:

- The happy path (valid input, expected response shape)
- Each documented error case (400 for a malformed request, 422 for an invalid one, 404 not found,
  etc.)
- Optional parameters: one test with the parameter absent, one with it set (when it changes the
  result meaningfully)

Do not test internal implementation details (URL construction, header names). Test the observable
contract: what goes in, what comes out, which HTTP errors are surfaced.

---

## Common Pitfalls

Every entry is stated in full above; this list is the review checklist.

- Importing from a relative path in a test instead of `@a-novel/<service-slug>-rest`.
- Running vitest directly: `pnpm test` alone skips container setup and fails on a missing `REST_URL`.
- Adding a domain file without its `export *` line in `index.ts`.
- Diverging from the OpenAPI spec on URL paths, query parameter names, response field names, or HTTP
  methods — they must match it exactly.
- Writing `vite.config.ts` by hand instead of calling `ViteLibrary`: a list of peer names in
  `external` bundles their subpath imports.
- Calling global `fetch` instead of `api.fetch` / `api.fetchVoid`.
- Building the `Authorization` header or a `JSON.stringify` body by hand instead of passing
  `accessToken` and `json`.
- Dropping the Zod validator on an `api.fetch` call whose response the service defines.
- Skipping JSDoc on an exported symbol.
- Using `any` (an open-ended field is `unknown`, and every other type is explicit or inferred from a
  schema).
