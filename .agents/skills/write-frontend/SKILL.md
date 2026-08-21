---
name: write-frontend
description: >
  Base frontend conventions for EVERY browser-facing repository in a-novel and a-novel-kit —
  semantic HTML, accessible CSS, strict TypeScript, browser security, performance, data/state
  boundaries, dependency policy, mandatory live-Storybook handoff, and validation. Load it for ANY HTML, CSS, TypeScript, browser API,
  platform-* application, uikit, Storybook, or nodelib-browser work. Load `plan-ui-design` before
  non-trivial user-flow, interaction, information-architecture, or visual-direction work. Pair with
  `write-svelte` for .svelte files, `write-frontend-tests` for frontend tests or stories, and
  `write-design-system` for tokens or reusable UI. Service REST clients under pkg/js also load
  `write-js-package`.
---

# Frontend Conventions (common)

Apply this base layer to every browser-facing change. Read the target file, its nearest siblings,
the package manifest, TypeScript config, lint config, and public exports before editing. Preserve a
coherent local pattern unless it conflicts with a rule below or a current platform standard.

This skill owns generic implementation quality, not repository placement. In the Agora workspace,
use `write-platform` for terminal application shells and product policy, `write-design-system` for
uikit visual contracts, and nodelib for reusable non-visual client runtime or tooling configuration.

Load `plan-ui-design` before deciding a new or materially changed flow, interaction pattern,
information hierarchy, component family, or visual language. This skill owns implementation quality;
`plan-ui-design` owns the human-facing contract that implementation must preserve.

**Rendered-UI hard gate:** Start Storybook with `BROWSER=none` and `--no-open`, inspect the exact
changed story in the integrated browser, keep the server live, and put its freshly verified actual
Markdown link in every status or final handoff. A screenshot or placeholder is never a substitute.

Use this authority order when guidance conflicts:

1. Repository contracts and supported-browser policy.
2. Normative web standards and WCAG.
3. Official framework or tool documentation for the installed version.
4. Maintained FOSS guidance from standards bodies and established organizations.
5. Local convention and personal preference.

Read [references/standards.md](references/standards.md) before choosing a browser API, accessibility
pattern, security boundary, design-token model, or unfamiliar framework feature. Verify versioned
APIs against current official documentation rather than relying on memory.

## After every edit

Use repository scripts as declared; do not invent parallel commands:

```bash
pnpm format                         # write formatting when the repo provides it
pnpm lint                           # formatting check + types + ESLint
a-novel test --type=pnpm -y         # all pnpm tests discovered by the workspace CLI
a-novel build --type=pnpm -y        # production/package builds
```

Run the narrowest package or test target while iterating, then run all applicable gates before the
change is ready. Load `use-a-novel-cli` whenever running test or build commands. A production build
is mandatory for routing, SSR, package-export, bundler, or environment-boundary changes.

## Live UI review — mandatory handoff

Treat Storybook as a mandatory review surface for every change that affects rendered UI. Do not
declare UI work complete from source inspection, unit tests, screenshots, or a static build alone.

The handoff contract is non-negotiable:

1. Add or update the smallest story or docs page that renders the changed UI and its meaningful
   states. If the repository has no usable Storybook, establish it within the agreed scope or report
   the missing review surface as a blocker.
2. Start the repository's Storybook command with `BROWSER=none` and `--no-open`. The process must
   not launch an external browser tab and must stay live for the operator unless they ask to stop it.
3. Wait for readiness, discover the actual listening URL and port, and verify that the exact changed
   story or docs route responds.
4. Open that route in the integrated browser and inspect it. Prefer it over the Storybook root when
   handing off a specific component.
5. Before every status or final response that hands UI work back, include a clickable inline link in
   the form `[Button — Storybook](http://127.0.0.1:6006/?path=/docs/button--docs)`, substituting the
   actual live URL and route. Repeat it even when an earlier response already contained it.
6. Re-resolve the URL immediately before sending the response. If the server stopped or changed,
   restart it and verify the new link.

A screenshot, placeholder such as “visual preview,” stale URL, or instruction to find an earlier
link does not satisfy this contract.

## Dependencies

- Load `choose-dependency` before adding, replacing, or evaluating a package.
- Prefer the web platform, an existing workspace package, or an existing dependency in that order.
- Before implementing reusable code in a terminal app, route visual contracts to uikit and
  framework-agnostic runtime or shared build/lint/test/localization configuration to nodelib.
- Require an explicit reason for every runtime dependency: capability, maintenance owner, release
  health, browser cost, license, security posture, and why the current stack cannot provide it.
- Prefer standards-body or established-organization ownership. Do not adopt an unmaintained package
  or a narrow personal utility for code that is straightforward to own.
- Import the narrowest documented public entry point. Never deep-import private package files.
- Keep browser bundles free of Node-only modules and server-only transitive dependencies.

## TypeScript and modules

- Keep `strict` enabled. Do not introduce `any`, disable strict checks, or hide errors with broad
  casts. Use `unknown`, narrow it, and validate untrusted data at the boundary.
- Model impossible states out with discriminated unions. Represent absence deliberately; do not use
  empty strings, magic numbers, or non-null assertions as state management.
- Prefer inference inside a function and explicit types at exported, callback, network, storage, and
  component boundaries. Use `satisfies` when checking a value without widening it.
- Use `type` imports for type-only dependencies and ES modules exclusively.
- Keep domain data serializable unless the boundary explicitly supports richer values.
- Treat API, URL, storage, message, and DOM data as untrusted at runtime even when TypeScript types it.
- Catch `unknown`; preserve the original cause when translating errors. Never silently discard a
  rejected promise.

## Naming and files

- Name Svelte components in PascalCase and plain TypeScript/CSS modules in lowercase camelCase or a
  single lowercase word, matching the surrounding package.
- Mirror source names in tests and stories: `Button.svelte.test.ts`, `Button.stories.svelte`, and
  `retry.test.ts`. Follow SvelteKit route filenames exactly where its router owns the convention.
- Name exported types and components in PascalCase; functions, values, and props in camelCase; and
  true constants in SCREAMING_SNAKE_CASE only when the package already uses that distinction.
- Give booleans a state or capability name (`disabled`, `hasError`, `canRetry`) and event callbacks
  the behavior they represent (`onSubmit`, `onDismiss`). Avoid generic `data`, `item`, or `handler`
  when a domain name is available.

## Components and controllers

- Keep components presentational: render semantic HTML and accessibility state, translate native events
  into semantic requests, and own only DOM mechanics such as element references, focus movement,
  measurements, and transient typeahead bookkeeping.
- Put meaningful rendered state and its transition rules in a pure controller with no DOM access,
  component rendering, route imports, storage, session, or network calls. A controller may use the
  framework reactive primitive in a `.svelte.ts` module.
- Let a stateful component accept at most one controller. Do not split its contract across bindable
  state props, change callbacks, and a controller; stateless components need no controller.
- Expose semantic controller methods (`open`, `close`, `select`, `setChecked`) rather than generic
  setters. The component reports intent; the controller may accept, transform, or reject it.
- Export the controller contract and a configurable default factory. Callers may supply another
  implementation that satisfies the same contract, including fixed-state Storybook controllers.
- Unit-test controller transitions without rendering. Test the component boundary for DOM semantics,
  accessibility behavior, and rejected transitions.

## HTML and interaction

- Use the native element with the required behavior: `button` for actions, `a` for navigation,
  labels and controls for forms, and landmarks/headings for document structure.
- Add ARIA only when native HTML cannot express the contract. A role is a promise to implement its
  keyboard interaction, focus behavior, state, and accessible name.
- Set `type="button"` on non-submit buttons. Keep form submission, validation, autofill, and error
  association available without pointer input.
- Preserve logical source order and normal tab order. Never use a positive `tabindex`.
- Expose a visible focus indicator. Do not remove outlines without an equal or stronger replacement.
- Treat persistent state as stronger than transient input. Selected, checked, expanded, invalid,
  loading, and disabled meaning must remain clear while a control is hovered, active, or focused.
- For reusable content-bearing components, prefer the framework's native composition primitive over
  a string-only label prop when text, markup, icons, or nested components are semantically safe.
- Support keyboard, pointer, touch, zoom, reflow, text spacing, and assistive technology. Do not make
  color, hover, drag, or animation the only way to understand or operate a control.
- Provide useful alternative text and accessible names. Decorative media stays silent.
- Set document and changed-passage language correctly. Use logical CSS properties so RTL does not
  require a second component implementation.
- Treat WCAG 2.2 AA as the minimum acceptance baseline, not a guarantee supplied by automation.

## CSS and responsive layout

- Prefer normal flow, Grid, Flexbox, logical properties, and container/media queries over measured
  JavaScript layout. Use feature queries for optional enhancements.
- Start from the smallest supported viewport and let content determine breakpoints. Avoid device-name
  breakpoints and user-agent sniffing.
- Use design tokens for product colors, spacing, type, radii, borders, elevation, and motion. A raw
  value is acceptable only for a local algorithmic constant with no design meaning.
- Use relative units for type and layout. Keep line height unitless. Reserve pixels for genuinely
  device-bound details such as a one-device-pixel hairline when the token contract calls for it.
- Keep selectors shallow and component-scoped. Avoid `!important`; fix cascade ownership instead.
- Preserve content at 200% text zoom and 400% page zoom/reflow. Do not clip user content to force a
  mockup height.
- Respect `prefers-reduced-motion`, forced colors, increased contrast where supported, and user font
  settings. Motion must not be required to understand state.
- Animate compositor-friendly properties when possible and never add animation without a purpose.

## State, data, and browser boundaries

- Give each piece of state one owner. Derive values instead of synchronizing duplicate state.
- Put shareable navigation state in the URL. Keep ephemeral interaction state local to the smallest
  component that owns it.
- Represent loading, empty, error, stale, and success states explicitly. Preserve useful content
  during refresh when the product contract allows it.
- Cancel or supersede obsolete asynchronous work. Guard against out-of-order responses.
- Keep rendering pure. Synchronize with external systems in framework lifecycle primitives, not in
  getters, templates, or module import side effects.
- Keep server-only code, environment variables, credentials, and privileged API calls out of browser
  bundles. Assume every shipped byte and source map is public.
- Use progressive enhancement for navigation and forms when the framework supports it. A network or
  JavaScript failure should degrade intentionally rather than strand the user.

## Security and privacy

- Never place secrets or long-lived credentials in client code, browser storage, logs, analytics, or
  error messages. Follow the repository authentication model.
- Do not render untrusted HTML. Use text interpolation; if rich HTML is a real product requirement,
  choose and configure a maintained sanitizer through `choose-dependency` and test bypass cases.
- Use safe URL construction and validate protocols before navigation. Do not concatenate executable
  markup, CSS, script, or query fragments from untrusted input.
- Collect and persist only required data. Do not add analytics, remote fonts, third-party scripts,
  beacons, or cross-origin calls without explicit product and security approval.
- Preserve CSP compatibility: avoid inline script generation, `eval`, and undocumented third-party
  origins.

## Performance and compatibility

- Set an explicit browser baseline in project configuration. Use feature detection and progressive
  enhancement; add a polyfill only after measuring need and bundle cost.
- Prefer semantic markup and CSS over JavaScript. Lazy-load non-critical routes and heavy features,
  not primary content or interaction affordances.
- Reserve media dimensions, serve responsive assets, and avoid layout thrashing. Batch DOM reads and
  writes only after measurement proves imperative layout is necessary.
- Measure before optimizing. Track user-facing latency and Core Web Vitals for applications; do not
  trade correctness or accessibility for an unmeasured micro-optimization.
- Handle offline, timeout, retry, and partial-response behavior at the owning boundary. Never retry a
  non-idempotent operation invisibly.

## Completion checklist

- Semantic and keyboard behavior works without a mouse.
- Focus, zoom/reflow, reduced motion, forced colors, long content, localization, and RTL were
  considered in proportion to the change.
- Persistent states remain stable under transient interactions, and reusable content APIs compose
  naturally without sacrificing native semantics.
- Trust boundaries validate runtime data and do not expose secrets.
- Loading, empty, error, and success paths are intentional.
- `write-frontend-tests` covers changed behavior.
- Storybook is running, the changed UI was inspected there, and the live link is in the handoff.
- Format, lint, pnpm tests, and production build pass.
