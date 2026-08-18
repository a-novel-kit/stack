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

A platform is one deployable SvelteKit application and one root package. It is a terminal product,
not a general-purpose client library. Apply this ownership split before adding code or configuration:

- **Platform** owns its shell and UX: routes, screen and shell compositions, route models, product
  copy and catalogs, supported locales, product URL and local-storage policy, and service-specific
  authentication, session, client, and health wiring.
- **Nodelib** owns reusable non-visual client infrastructure: framework-agnostic runtime helpers and
  shared build, lint, test, SvelteKit, and localization configuration. A representative extraction
  fixture belongs beside the shared preset it validates.
- **Uikit** owns reusable visual contracts: design tokens, components, and shared Storybook theme,
  preview, decorator, docs, and visual-test defaults.

Consume released kit packages through documented public exports. Do not publish app code or keep a
platform-local copy of a generic helper, preset, visual primitive, or Storybook default.

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

Keep platform configuration entrypoints thin: import shared factories and supply only product paths,
catalog policy, environment policy, or test selection. Generic defaults and their representative
fixtures stay with the shared package; product catalogs, route models, and workflows stay in the
platform.

When a missing piece is reusable, plan and release it from the owning kit repository first: visual
contracts go to uikit, while runtime helpers and tooling presets go to nodelib. Then pin the exact
release in the platform and remove any superseded local implementation. Do not grow either a private
design system or a private client-infrastructure library inside the app.

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

Import shared Storybook preview, theme, decorators, and test defaults from uikit's development
package. Keep a local Storybook entrypoint only for product-specific context and story discovery; do
not copy the reusable preview into the platform.

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
- Reuse runtime schemas exported by the published typed service client for service-owned fields; do
  not rewrite the same contract in a second validator. Use repository-pinned Valibot schemas for
  app-owned input and `sveltekit-superforms` for non-trivial form state and progressive enhancement.
  Uikit owns rendered fields and errors, so platforms do not import Formsnap or another component layer.
- Server loads establish the session and authorization context. UI visibility is convenience;
  privileged routes and actions enforce permissions again on the server.
- Create an anonymous service session lazily, only when an anonymous protected operation needs it.
  An ordinary page load must not mint a token merely to render signed-out UI.
- Refresh a rejected or expiring session at one server-owned boundary. Clear cookies only after a
  definitive authentication rejection; preserve them and expose a recoverable unavailable state on
  network, timeout, or downstream 5xx failures. Do not silently turn an outage into signed-out state.
- A logout that only clears platform cookies is not token revocation. Describe it accurately and
  plan a backend capability when revocation is required.
- A short-code `GET` only parses a sanitized display model; it never verifies or consumes the code.
  Completion uses `POST` to the current URL so no hidden field duplicates the credential. Return
  `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, and `X-Robots-Tag: noindex`.
- Never serialize the raw code, raw target, password, or token through load/action data, rendered
  HTML, stories, logs, or error copy. After completion, use POST/redirect/GET to a clean status-only
  URL; collapse rejected, expired, consumed, or unknown targets when the service cannot safely
  distinguish them.
- Map service failures to stable user-facing error categories. Log only sanitized operational
  context on the server; do not expose service internals or secrets in UI copy.

Progressive enhancement is required for essential authentication forms. JavaScript may improve
focus, pending state, and modal behavior, but it must not become the only path to submit or recover.

## Localization

Keep source messages in static, reviewable repository files. All visible product copy, accessible
names, validation messages, titles, and metadata use message keys; logs and protocol tokens do not.

- Import the shared extraction/type/status preset and request-localization runtime from nodelib.
  Platform configuration supplies only its locales, paths, namespaces, and product exceptions.
- Keep the preset's framework extraction fixture in nodelib. Platform stories and tests exercise
  real product copy and translated UI states; they do not duplicate a generic toolchain fixture.
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

Build the real container in CI, not only the Node artifact. Run the frozen install inside the exact
image and keep its Node pin compatible with every locked dependency engine; a floating host CI
runtime can hide a stale image pin. When Corepack/pnpm configures a global binary directory, put
that exact directory on `PATH` before running global config commands. Pass private-registry
credentials as BuildKit secrets, remove temporary registry config in the same layer, and prove the
final image contains neither credential nor build-only tooling.

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
