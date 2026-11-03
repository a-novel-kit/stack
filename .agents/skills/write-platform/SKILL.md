---
name: write-platform
description: >
  Create, edit, review, or debug SvelteKit platform apps: routes, sessions, state, localization,
  app composition, health, and CI. Always load write-frontend; add Svelte and test skills as
  needed.
---

# Platform Application Conventions

This skill is the platform counterpart to `write-go-service`: `write-frontend` owns universal web
rules, `write-svelte` owns framework mechanics, `write-frontend-tests` owns tests and stories, and
this skill owns the terminal application's architecture. Load
[develop-feature](../develop-feature/SKILL.md) for stage timing: run a local app and Storybook draft,
agree on direction, review issues and PRs, complete approved-scope coverage, and tighten the
result. Full state/test matrices below are completion gates, not prerequisites to exploring a draft.

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

### What you see, where it lives

A platform has two layers, as a service has handlers, core and dao. `src/routes/` mirrors the URL:
each directory is a path segment or a route group, and holds everything only its routes use.
`src/lib/` holds what the whole app shares, split by where the code runs and named by concern, never
by route. To find anything on screen, open the directory of its URL and walk up toward
`src/routes/`; past the root, look in `src/lib/`.

Place each file in the deepest directory that holds every route using it. A route group `(name)`
holds at least one route and exists to share a layout or code among them, so a directory that holds
no route is never a group. Every platform follows this table:

| What you see                                           | Where it lives                                                                                                   |
| ------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------- |
| The page at `/a/b`                                     | `src/routes/…/a/b/+page.svelte`, which only builds the controller and sets the document title                    |
| What that page renders                                 | `screen.svelte` beside it, one per directory                                                                     |
| Its state and transitions                              | `controller.svelte.ts` beside it                                                                                 |
| A dialog or panel inside that screen                   | `<part>.svelte` beside the screen, its controller in the same `controller.svelte.ts`                             |
| What the page loads and the forms it submits           | `+page.server.ts`, which holds the load and actions unless several routes share them                             |
| A frame around several pages: shell, rail, banner      | the directory holding them: `+layout.svelte`, `+layout.server.ts`, `screen.svelte`, `controller.svelte.ts`       |
| The index page of a framed directory                   | a group named for that page, as `(app)/(home)/`, so its screen keeps a directory of its own                      |
| The error page                                         | `error-screen.svelte` beside its `+error.svelte`                                                                 |
| An address with no page: callback, logout, probe       | `+server.ts`; a `+page.server.ts` whose load redirects when a refusal must render the error page                 |
| One screen several routes render                       | the group holding them, as `ext/(short-code)/screen.svelte`                                                      |
| Code the routes under one directory share              | that directory: `<concern>.ts`, or `<concern>.server.ts` for server code the route files export                  |
| A component routes across the app render               | `src/lib/ui/<Name>.svelte`, its context in `src/lib/ui/<concern>.svelte.ts`                                      |
| App-wide pure logic: URL codecs, forms, view types     | `src/lib/application/<concern>/`                                                                                 |
| App-wide browser adapters: storage, telemetry          | `src/lib/client/`                                                                                                |
| Sessions, service clients, configuration, downtime     | `src/lib/server/<concern>/`, which nothing browser-reachable imports                                             |
| Visible text                                           | `src/lib/i18n/locales/<locale>/common.yaml`, under a top-level key named for the screen or component showing it  |
| Stories and docs                                       | `<file>.stories.svelte` and `<file>.mdx` beside the component; `story.svelte` when stories share a fixed harness |
| Tests                                                  | beside the file and named after it: `screen.svelte.test.ts`, `controller.test.ts`, `page.server.test.ts`         |
| Server code driven across routes with one real session | `request.test.ts` in the deepest directory holding those routes                                                  |
| A journey across pages in a browser                    | `tests/e2e/<journey>.spec.ts`                                                                                    |

SvelteKit reserves the `+` prefix, so the test of `+page.server.ts` is `page.server.test.ts` and the
test of `+server.ts` is `server.test.ts`. It also reads a bare route-file name such as
`error.svelte` as a missing prefix, hence `error-screen.svelte`. A route test may mock the
authentication context, so the request suite, which drives loads, actions and endpoints through the
real session with only `fetch` stubbed, keeps a file of its own. Files in a route directory keep
SvelteKit's lowercase names; components in `src/lib/ui/` are PascalCase.

SvelteKit refuses to bundle a `*.server.ts` module or anything under `src/lib/server/` for the
browser, so server-only code always sits behind one of the two. Dependencies point inward:
`src/lib/` never imports from `src/routes/`, and a route imports only from its own directory, the
directories above it, and `src/lib/`. Screens, their parts and `src/lib/ui/` import no route globals
(`$app/state`, `$app/navigation`), storage, cookies, environment secrets, or network clients; the
route files and adapters bring those in.

`a-novel/platform-template` is the shortcut to start a new platform, so it tracks the platforms'
conventions. A change that sets or changes a convention in a platform ships with the matching
`platform-template` change, in a pull request opened alongside it.

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

Keep the server to security, and give everything else to the browser. The server holds the session
cookies, refreshes tokens, checks authorization and CSRF, and makes every call that needs a
credential. Heavy screens, workflow state and the composition of service results run in the
browser, so one small, always-on server carries many visitors. A product feature that adds server
work needs a security reason.

Use `@sveltejs/adapter-node` and SSR as the default for the shell, public pages and the forms that
must work without JavaScript. Opt a private route family out with `export const ssr = false` when it
is a heavy browser application, such as an editor, and its browser-only behavior is deliberate and
tested; never disable SSR globally to hide unsafe module initialization or a hydration bug.

adapter-node 6 reads no `ORIGIN` at run time. The Vite config passes
`paths: { origin: process.env.ORIGIN }`, so a build for a known address, such as nodelib's
Playwright server, bakes its origin in. The published image derives the origin from each request's
`Host`, over https unless `PROTOCOL_HEADER` names a header. Production's load balancer ends TLS, so
https holds there. SvelteKit's CSRF check and `event.url` both read that origin, and a sign-in builds
its redirect URI from `event.url`.

Read secrets and privileged endpoints only from SvelteKit's server-only environment modules. Expose
an explicitly public value through the framework's `PUBLIC_` contract only when it is safe to ship
in source. A secret-shaped `VITE_*` variable is a build failure, not runtime configuration.

Under Vitest, SvelteKit fills `$app/env/private` from the committed `.env.local` too, so unit tests
run with the local-development configuration unless they mock that module. A server default that
names a production resource, such as `DOWNTIME_URL`, is then live in a unit test, and only the
offline unit project keeps the test from fetching it.

Trace the server with SvelteKit's own spans and nodelib's setup. Enable
`sveltekit({ tracing: { server: true } })`, declare `OTEL` in `src/env.ts`, and start
`@a-novel-kit/nodelib-server/telemetry` from `src/instrumentation.server.ts` with
`untracedRoutes: ["/ping"]`, flushing it on adapter-node's `sveltekit:shutdown` event. The setup
mirrors golib's OTLP preset: the deployment's standard `OTEL_EXPORTER_OTLP_*` variables pick the
backend, and without an endpoint spans print to stdout. It traces the global `fetch` into backend
services, keeps every trace even when the caller did not sample it, and redacts query values from
recorded URLs. Keep secrets in the query, never in a path segment, which is recorded as is. In
production, the server spans go to Grafana through the cluster's Alloy, as `write-infra` describes.

The server `handle` hook names SvelteKit's request span, `sveltekit.handle.root`, after its route
ID, as `GET /(studio)/account`. A request that matched no route keeps the method alone, so a path
never names a span. The hook also records the response's `http.response.status_code` and marks a
5xx response as an error. Span metrics split the traffic by that name and class each request by
the code and the span status, and SvelteKit renders a failing load as a 500 page without failing
the span.

A new platform, or a change to its routes, form actions or the services it calls, updates its
Grafana coverage in infra, as `write-infra`'s [Grafana coverage](../write-infra/references/grafana.md)
describes.

The browser reports nothing. Production gives no platform a browser telemetry destination, so the
consent-gated `@a-novel-kit/nodelib-browser/telemetry` stays off and asks nothing. Grafana Faro
replaces it (a-novel/.github#517). Until then, keep its consent preference as it is:

- The root layout renders uikit's `ConsentBanner` first in the reading order, with the platform's
  wording, and shows it only while the device records no answer.
- The answer lives in one reactive preference the root layout shares through a context. The
  banner's visibility derives from it, and a public `/privacy` page changes it with a `Switch`.
  The browser keeps the answer per site, so every platform has its own page, open without signing
  in. The shell links it only while the platform collects browser telemetry, and no other screen
  repeats the switch.

Add no tracker, analytics or third-party script without a consent decision on the planning issue.

## Storybook-first delivery

Build every user-visible screen in this order:

1. Define the screen controller contract: serializable rendered state plus semantic transition methods.
2. Implement the screen or shell composition without route, session, storage, or network imports.
3. Add deterministic stories for the meaningful state matrix: loading, empty, error, success,
   permissions, narrow/wide layouts, long translations, and interaction states as applicable.
4. Start Storybook with `BROWSER=none` and `--no-open`; inspect the exact stories in the integrated
   browser at narrow and wide viewports and keep the verified link for the handoff.
5. Add component and interaction tests at the pure boundary.
6. Implement the pure controller and unit-test its transitions; then let the route file construct it from
   loads, actions, URL state, storage adapters, and browser adapters.
7. Add an end-to-end test only for a critical browser or cross-boundary journey that smaller tests
   cannot prove. Any committed browser test runs in CI.

Stories are an executable design surface, not production fixtures. Keep mocks typed, local,
deterministic, and incapable of contacting real services. Route files translate backend and
framework results into the same controller contracts the stories exercise.

For each routed screen or layout, keep `screen.svelte` and `controller.svelte.ts` beside the SvelteKit
route files, as [the layout table](#what-you-see-where-it-lives) places them. `screen.svelte` is the
pure UI component and accepts one screen controller. `controller.svelte.ts` contains pure reactive
state and semantic transitions without rendering or DOM access. `+page.svelte` or `+layout.svelte`
performs only final wiring plus route-owned metadata such as the document title. Do not add a
connector component that merely renders the screen.

A screen renders from its props and its controller alone. The wiring reads the app's contexts, such
as the planned downtime or the telemetry preference, and passes their values through the controller.
The localization context is the one context a screen reads itself. A story then reaches every state
with a fixed controller, and a state it cannot reach that way is hidden state to move into the
controller.

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
- **Server session cookies** own authentication credentials. Name each one with the `__Host-`
  prefix, which the browser accepts only with `Secure`, `Path=/` and no `Domain`
  ([RFC 6265bis §4.1.3.2](https://datatracker.ietf.org/doc/html/draft-ietf-httpbis-rfc6265bis#section-4.1.3.2)),
  so a sibling host cannot plant a session. Add `HttpOnly` and an intentional `SameSite` policy.
  Chromium, which runs the browser tests, accepts them from `localhost` and `127.0.0.1` over plain
  http, so local stacks keep working. Renaming a cookie signs everyone out once. Tokens never enter
  browser storage or serialized load data.
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
- Check the session once per request. SvelteKit runs the layout and page server loads in parallel,
  so a context built per call asks the authentication service once per load, and once more in an
  action. Keep one authentication context per request in a `WeakMap` keyed by `event.cookies`, which
  every hook, action and load of a request shares. A cookie write discards the cached check.
- Create an anonymous service session lazily, only when an anonymous protected operation needs it.
  An ordinary page load must not mint a token merely to render signed-out UI. Keep its cookie until
  the token expires: a visitor gets only a few anonymous tokens, and a browser restart must not
  spend one.
- Send every service call through one wrapped `fetch`. It forwards the visitor's IP in
  `X-Client-IP`, read with `event.getClientAddress()` (adapter-node's `ADDRESS_HEADER` and
  `XFF_DEPTH` behind a proxy). It also sends the platform's service token in
  `X-Service-Authorization`, read from `SERVICE_TOKEN_PATH` on every call, since the kubelet
  replaces the file before the token expires. Services rate-limit by that visitor; without it,
  every visitor shares the platform's budget.
- Map a service's `429` to a too-many-attempts message. It is neither an outage nor a signed-out
  state.
- Refresh a rejected or expiring session at one server-owned boundary. Clear cookies only after a
  definitive authentication rejection; preserve them and expose a recoverable unavailable state on
  network, timeout, or downstream 5xx failures. Do not silently turn an outage into signed-out state.
- Accounts come only from waitlist invitations while the platform earns nothing: a visitor asks for
  an invitation, an operator invites the address, and the invitation link creates the account with a
  password. Until the waitlist is retired, add no other way to create one, through a form, a service
  call or an identity provider such as Google. "Register" ("M’inscrire" in French) opens the
  invitation request on every platform. Only the account platform's page for a dead invitation link
  says "Request an invitation", which matches its offer to request a new one.
- A response rendered with a session belongs to its visitor alone: the request hook sends
  `Cache-Control: private, no-store` whenever the session cookies are present
  ([RFC 9111 §5.2.2.5](https://www.rfc-editor.org/rfc/rfc9111#section-5.2.2.5)). Every response also
  refuses framing with `X-Frame-Options: DENY` and `frame-ancestors 'none'`
  ([RFC 9700 §4.16](https://www.rfc-editor.org/rfc/rfc9700#section-4.16)).
- The minimum password length stays 4, as ruled, and is never raised. Every new-password field
  (account creation, reset, change) shows a hint recommending at least 15 characters, the
  [NIST SP 800-63B-4](https://pages.nist.gov/800-63-4/sp800-63b.html) guidance for a password used
  alone. The hint is advice the visitor may ignore, never a validation error.
- Logout ends the session through the service, then clears the cookies. The session spans every
  platform, so each one finds it ended on its next request. A failed revocation still clears the
  cookies: the visitor asked to leave this browser signed out.
- A short-code `GET` only parses a sanitized display model; it never verifies or consumes the code.
  Completion uses `POST` to the current URL so no hidden field duplicates the credential. Return
  `Cache-Control: no-store`, `Referrer-Policy: strict-origin`, and `X-Robots-Tag: noindex`. With
  `no-referrer`, a browser sends `Origin: null` on a native form post, which SvelteKit's CSRF check
  rejects.
- Never serialize the raw code, raw target, password, or token through load/action data, rendered
  HTML, stories, logs, or error copy. After completion, use POST/redirect/GET to a clean status-only
  URL; collapse rejected, expired, consumed, or unknown targets when the service cannot safely
  distinguish them.
- Map service failures to stable user-facing error categories. Log only sanitized operational
  context on the server; do not expose service internals or secrets in UI copy.

Progressive enhancement is required for essential authentication forms. JavaScript may improve
focus, pending state, and modal behavior, but it must not become the only path to submit or recover.

### Sign in through the account platform

The account platform, `account.storyverse.fr`, is the only platform that asks for a password. It
serves every sign-in, registration and account form: the sign-in dialog, the waitlist, account
creation from an invitation, password reset and email changes. Every other platform is a client: it
signs people in through the account platform with the OAuth 2.0 authorization code grant and PKCE
S256 ([RFC 6749 §4.1](https://www.rfc-editor.org/rfc/rfc6749#section-4.1),
[RFC 7636](https://www.rfc-editor.org/rfc/rfc7636)), which Authentication serves. A client's shell
keeps only these account controls:

- A sign-in link to its own `/auth/sign-in?returnTo=<page>`.
- A registration link to the account platform's `/?auth=register`, which requests an invitation.
- Once signed in, a link that opens the account platform in a new tab, and the logout form.

Infra registers every platform but the account platform in Authentication's client registry. The
platform's name is its client ID, and `/auth/callback` on its host is its one redirect URI. The
client serves three routes, as platform-studio's `src/routes/auth/sign-in.server.ts` does:

1. `/auth/sign-in` is a page whose server load draws a `state` and a PKCE verifier, keeps them with a
   same-origin `returnTo` in a 10-minute `__Host-` cookie, and
   redirects to the account platform's `/authorize`. A page renders `+error.svelte`, so a refusal
   such as a planned downtime shows the maintenance page.
2. `/auth/callback` reads a response parameter only when it is given once. It refuses the response
   before any service call unless `state` matches the cookie and `iss` is the account platform's
   origin ([RFC 9207](https://www.rfc-editor.org/rfc/rfc9207)). It exchanges the code with its
   verifier and redirect URI through the typed client, then redirects to `returnTo`, checked again
   since a planted cookie could name another site, with
   `Cache-Control: no-store` and `Referrer-Policy: no-referrer`. The code then leaves the address
   bar and reaches no referrer.
3. `/auth/sign-in/failed?returnTo=<page>` is the signed-out page a failed sign-in lands on, with a
   retry. `temporarily_unavailable`, `server_error` and outages read as unavailable, a `429` as too
   many attempts, and any other refusal as rejected.

A person already signed in on the account platform comes back signed in without a form. The
client's end-to-end stack runs the account platform, and its journeys sign in through it, as
`write-frontend-tests` describes.

## Localization

Keep source messages in static, reviewable repository files. All visible product copy, accessible
names, validation messages, titles, and metadata use message keys; logs and protocol tokens do not.

- Import the shared extraction/type/status preset and request-localization runtime from nodelib.
  Platform configuration supplies only its locales, paths, namespaces, and product exceptions.
- Keep the preset's framework extraction fixture in nodelib. Platform stories and tests exercise
  real product copy and translated UI states; they do not duplicate a generic toolchain fixture.
- Use formal address forms in static text when a locale distinguishes them (`vous` in French,
  `usted` in Spanish). Use each language's conventional action-label form for controls. A control
  whose action is about the reader (signing up, signing in or out, their account and its details)
  speaks in the reader's voice: `M’inscrire`, `Me connecter`, `Changer mon mot de passe`. A
  control that works the interface keeps the plain form, even on the reader's data:
  `Afficher le mot de passe`, `Fermer la fenêtre`. Other text names the reader's things as theirs
  and says who acts (`Nous n’avons pas pu vérifier votre session.`), never an impersonal
  `Impossible de vérifier la session.`
- Support plurals and contextual variants through the selected message format, not key
  concatenation or runtime grammar.
- Treat the source locale as authoritative. CI compiles messages and fails for missing, invalid, or
  stale translations using the repository's pinned tooling.
- When a product requires runtime message catalogs, keep the static catalogs as the runtime source
  of truth; generated type declarations or indexes are derived artifacts and are never hand-edited.
  Do not replace a requested message-catalog workflow with generated per-message functions merely
  for type safety.
- Resolve visible messages atomically where they render. Svelte components read the request- or
  tree-scoped i18next instance from the shared native context and call it with statically
  discoverable keys; they do not receive pretranslated screen-sized dictionaries.
- Declare each product message only in its static catalog. Do not mirror catalogs into `*Copy`
  interfaces, copy-builder functions, story-only dictionaries, or other programmatic message maps.
  A table of `(t) => t("key")` selectors is such a map; call `t()` with the static key where the
  text renders.
  Stable domain and action-result codes remain application state; translate them at the rendering
  boundary instead of returning visible prose from wiring.
- Install the same product-owned localization provider around the application, Storybook stories,
  and component-test render trees. Storybook global decorators and test wrappers may select locale
  and catalogs, but must not redeclare messages or substitute ad hoc copy objects.
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
runtime can hide a stale image pin. Node.js 25 and later ship no Corepack, so install the pnpm
release `packageManager` pins with `npm install --global --ignore-scripts "pnpm@<version>"`; keep
`pnpm@` literal, or hadolint cannot see the pin (DL3016). When pnpm configures a global binary
directory, put that exact directory on `PATH` before running global config commands. Pass
private-registry credentials as BuildKit secrets, remove temporary registry config in the same
layer, and prove the final image contains neither credential nor build-only tooling.

adapter-node records each client file's size, SHA-256 ETag and precompressed sizes in
`build/adapter-node.js` when it copies the client output. A file changed after `vite build` is
served with a stale length and ETag, and a file deleted from `build/` stays listed. Change client
code only while bundling, in a Vite plugin, and remove build-only files such as source maps from
`.svelte-kit/output/client` before the adapter copies it.

Expose two distinct probes:

- `GET /ping` is cheap liveness and has no downstream dependency. The container `HEALTHCHECK` uses
  it so a service outage does not restart a healthy platform process.
- `GET /healthcheck` is readiness/diagnostics. It fans out with bounded timeouts only to registered,
  typed dependencies the platform actually uses and returns a stable nested status map. Sanitize
  errors and preserve partial results so one failure does not hide the others.

Never turn a request parameter into an arbitrary healthcheck URL. Configuration selects from known
clients, and concurrency plus deadlines bound the fan-out cost.

Cover `/healthcheck` with an end-to-end request test that expects `200` against the real dependency
stack. A unit test that mocks `fetch` asserts whatever path it was written with, so it cannot catch a
probe aimed at a route the service does not serve.

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
- Every form sits on an island, a uikit `Card` or a `Dialog`, never on the page canvas: fields are
  borderless wells and vanish there ([write-design-system](../write-design-system/SKILL.md#the-a-novel-visual-direction)).
- Every failure, refusal, or warning renders in a toned feedback box (`Alert`, `InlineMessage`, or a
  page-level `StatusState`), never as plain text
  ([write-design-system](../write-design-system/SKILL.md#the-a-novel-visual-direction)).
- Static catalogs are the sole declaration of visible messages; app, Storybook, and tests use the
  same request- or tree-scoped localization provider.
- URL codecs round-trip and invalid parameters normalize predictably; persisted preferences are
  versioned and SSR-safe.
- No authentication secret reaches browser storage, logs, client bundles, or serialized page data.
- Server actions and session transitions have unit tests, including expiry and downstream failure.
- `/ping`, `/healthcheck`, production build, Storybook build, and the container are verified.
- `pnpm lint`, `a-novel test --type=pnpm -y`, and `a-novel build --type=pnpm -y` pass.
- All affected Playwright journeys and screenshot comparisons have been run and reviewed per
  `write-frontend-tests`; required end-to-end coverage is complete.
- Final cleanup follows `prefer-small-solutions`, with affected checks rerun.
- The live app/Storybook routes, developer review steps, and verification limits appear in the handoff.
