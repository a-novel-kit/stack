---
name: write-platform
description: >
  Application-architecture conventions for a-novel client-side PLATFORM repos (`app/platform-*`):
  SvelteKit route and server boundaries, app-owned screen composition, Storybook-first delivery,
  URL and browser-state ownership, secure sessions, localization, container health, and CI. Load it
  whenever creating, editing, reviewing, or debugging a platform application. ALWAYS load
  `write-frontend`; add `write-svelte` for Svelte/SvelteKit files and `write-frontend-tests` for
  tests or stories. Use `write-design-system` only when changing reusable uikit contracts.
---

# Platform Application Conventions

This skill is the platform counterpart to `write-go-service`: `write-frontend` owns universal web
rules, `write-svelte` owns framework mechanics, `write-frontend-tests` owns tests and stories, and
this skill owns the terminal application's architecture and delivery sequence.

Load `plan-ui-design` before choosing a visual direction or interaction language. Load
`choose-dependency` before adding a package, `write-dockerfiles` before changing the root
`Dockerfile`, `write-github-actions` before changing CI, and `write-project-docs` before changing
root project documentation. Use `plan-client-server-boundary` when a platform composes or changes a
backend capability.

## Ownership boundaries

A platform is one deployable SvelteKit application and one root package. It consumes published
`@a-novel-kit/uikit` foundations and components; it does not publish app code or copy generic
components locally.

Keep responsibilities explicit:

- `src/routes/` owns URLs, layouts, server loads, form actions, redirects, and HTTP endpoints.
- `src/lib/ui/` owns app-specific pure screen and shell compositions. It may depend on uikit, but
  not on route globals, storage, cookies, environment secrets, or live network clients.
- `src/lib/application/` owns pure workflow state, URL codecs, commands, and orchestration contracts.
- `src/lib/client/` owns browser adapters such as preference persistence and history integration.
- `src/lib/server/` owns sessions, privileged backend clients, environment configuration, and
  server-only policy. Nothing browser-reachable imports it.

Use the nearest established equivalent when a repository already has a coherent structure; do not
rename working boundaries merely to match these directory names. Dependencies point inward: routes
and adapters compose pure application/UI code, never the reverse.

Application shells, route models, product labels, and workflow compositions stay in the platform.
When a missing piece is generic across products, change uikit through its own planned, versioned PR
and consume the released package; do not grow a second private design system inside the app.

Decompose branches and PRs by one user-visible capability or one application boundary. Platform
work does not use a backend service's layer-by-layer branch stack.

## Rendering and configuration

Use `@sveltejs/adapter-node` and SSR as the default. Opt a private route family out with
`export const ssr = false` only when its browser-only behavior is deliberate and tested; never
disable SSR globally to hide unsafe module initialization or a hydration bug.

Read secrets and privileged endpoints only from SvelteKit's server-only environment modules. Expose
an explicitly public value through the framework's `PUBLIC_` contract only when it is safe to ship
in source. A secret-shaped `VITE_*` variable is a build failure, not runtime configuration.

## Storybook-first delivery

Build every user-visible screen in this order:

1. Define a serializable view model and explicit callbacks for the pure UI boundary.
2. Implement the screen or shell composition without route, session, storage, or network imports.
3. Add deterministic stories for the meaningful state matrix: loading, empty, error, success,
   permissions, narrow/wide layouts, long translations, and interaction states as applicable.
4. Start Storybook with `BROWSER=none` and `--no-open`; inspect the exact stories in the integrated
   browser at narrow and wide viewports and keep the verified link for the handoff.
5. Add component and interaction tests at the pure boundary.
6. Wire routes, server actions, sessions, and browser adapters; unit-test their parsers and state
   transitions independently.
7. Add an end-to-end test only for a critical browser or cross-boundary journey that smaller tests
   cannot prove. Any committed browser test runs in CI.

Stories are an executable design surface, not production fixtures. Keep mocks typed, local,
deterministic, and incapable of contacting real services. Route containers translate backend and
framework results into the same view models the stories exercise.

## State ownership

Every state has one canonical owner:

- **Path and search parameters** own shareable, navigation-relevant UI state. Centralize codecs,
  validate every value, choose deliberate defaults, and preserve unrelated parameters.
- **Local storage** owns non-sensitive, device-local preferences and recoverable user-specific
  drafts. Namespace and version keys, migrate or discard unknown versions, and read them only behind
  an SSR-safe client adapter.
- **Server session cookies** own authentication credentials. Use `HttpOnly`, `Secure` in production,
  and an intentional `SameSite` policy; tokens never enter browser storage or serialized load data.
- **Component memory** owns transient interaction and form input that should not survive navigation.

Do not duplicate one state across URL, storage, and component memory. Decide whether a URL change
deserves a history entry: use push semantics for navigable choices and replace semantics for
normalization or high-frequency adjustments. Server rendering and first hydration must derive the
same visible state; apply client-only preferences after the browser boundary without hiding a
hydration mismatch.

Never persist passwords, short codes, access tokens, refresh tokens, or unredacted backend errors.

## Authentication and server policy

Treat the SvelteKit server as a backend-for-frontend when a platform consumes an authentication
service:

- Browser forms submit to same-origin server actions. The server validates input, calls the typed
  service client, rotates session cookies, and returns a small serializable result.
- Validate form and route input through repository-pinned Valibot schemas. Use
  `sveltekit-superforms` for non-trivial form state and progressive enhancement; uikit owns the
  rendered fields and errors, so platforms do not import Formsnap or another component layer.
- Server loads establish the session and authorization context. UI visibility is convenience;
  privileged routes and actions enforce permissions again on the server.
- Refresh an expiring session at one server-owned boundary. If refresh fails, clear the local
  session and return a recoverable signed-out state.
- A logout that only clears platform cookies is not token revocation. Describe it accurately and
  plan a backend capability when revocation is required.
- Short-code URLs may render on `GET`, but completion mutates only through `POST`. Mark responses
  `Cache-Control: no-store` and `Referrer-Policy: no-referrer`, never persist the code, and remove
  sensitive search parameters from browser history after completion.
- Map service failures to stable user-facing error categories. Log only sanitized operational
  context on the server; do not expose service internals or secrets in UI copy.

Progressive enhancement is required for essential authentication forms. JavaScript may improve
focus, pending state, and modal behavior, but it must not become the only path to submit or recover.

## Localization

Keep source messages in static, reviewable repository files. All visible product copy, accessible
names, validation messages, titles, and metadata use message keys; logs and protocol tokens do not.

- Support plurals and contextual variants through the selected message format, not key
  concatenation or runtime grammar.
- Treat the source locale as authoritative. CI compiles messages and fails for missing, invalid, or
  stale translations using the repository's pinned tooling.
- When a product requires runtime message catalogs, keep the static catalogs as the runtime source
  of truth; generated type declarations or indexes are derived artifacts and are never hand-edited.
  Do not replace a requested message-catalog workflow with generated per-message functions merely
  for type safety.
- Under SSR, create request-scoped localization state. Never keep a mutable process-global locale
  or dictionary that can leak one user's language into another request.
- Pin any framework-specific extraction adapter and prove it against a representative component in
  CI. Prefer statically discoverable keys; prohibit dynamic key construction unless an explicit
  catalog allowlist keeps missing- and unused-key checks sound.
- Do not require a hosted translation service or a network call at build or runtime.
- Exercise long translations and at least one alternate locale in stories. Preserve logical layout
  and leave an RTL path even when the initial locales are left-to-right.

## Container and health contract

Ship one multi-stage root `Dockerfile` that builds the locked root package and runs the minimal
production SvelteKit server as a non-root user. Keep build-only credentials out of image layers and
the final image. Follow `write-dockerfiles` for implementation details.

Expose two distinct probes:

- `GET /ping` is cheap liveness and has no downstream dependency. The container `HEALTHCHECK` uses
  it so a service outage does not restart a healthy platform process.
- `GET /healthcheck` is readiness/diagnostics. It fans out with bounded timeouts only to registered,
  typed dependencies the platform actually uses and returns a stable nested status map. Sanitize
  errors and preserve partial results so one failure does not hide the others.

Never turn a request parameter into an arbitrary healthcheck URL. Configuration selects from known
clients, and concurrency plus deadlines bound the fan-out cost.

## CI and publication

The required pipeline runs format/style checks, type checking, localization validation, unit and
component tests, Storybook tests with accessibility checks, the production build, the static
Storybook build, dependency audit, and container build as applicable.

Publish only the static Storybook artifact through the shared GitHub Pages action. Production
deployment remains the container's concern. A Pages workflow uses GitHub's supported artifact and
deployment actions, declares the Pages environment, and does not cancel an in-progress deployment.
Never include secrets, private environment values, source maps containing secrets, or production
API credentials in Storybook.

## Completion checklist

- Pure screens and their complete state matrix render in Storybook before route wiring lands.
- Keyboard, focus, narrow layout, long copy, reduced motion, and automated accessibility checks pass.
- URL codecs round-trip and invalid parameters normalize predictably; persisted preferences are
  versioned and SSR-safe.
- No authentication secret reaches browser storage, logs, client bundles, or serialized page data.
- Server actions and session transitions have unit tests, including expiry and downstream failure.
- `/ping`, `/healthcheck`, production build, Storybook build, and the container are verified.
- `pnpm lint`, `a-novel test --type=pnpm -y`, and `a-novel build --type=pnpm -y` pass.
- The live Storybook URL and any intentionally deferred end-to-end coverage appear in the handoff.
