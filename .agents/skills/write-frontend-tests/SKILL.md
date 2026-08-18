---
name: write-frontend-tests
description: >
  Frontend test conventions for a-novel and a-novel-kit — Vitest unit tests, Testing Library DOM
  and Svelte component tests, Storybook stories and interaction tests, accessibility checks, and
  browser end-to-end tests. Load whenever adding or modifying frontend test/spec files, stories,
  fixtures, mocks, test configuration, or behavior that needs frontend coverage. ALWAYS load
  `write-frontend`; pair with `write-svelte` for Svelte targets and `write-design-system` for uikit.
---

# Frontend Test Conventions

Load `write-frontend` and read the production behavior before writing tests. Read neighboring tests
and the official documentation for the installed testing tools. Preserve existing tests unless the
behavior they specify has been intentionally removed.

## Test at the nearest truthful layer

Use the smallest layer that proves the user-visible contract:

1. Test pure parsing, formatting, and state transitions as unit tests.
2. Test component rendering and interaction through the DOM with Testing Library.
3. Use Storybook stories as the exhaustive catalog of reusable component states; add interaction and
   accessibility assertions where supported.
4. Use real-browser end-to-end tests for routing, focus across pages, browser APIs, hydration,
   multi-component journeys, and behavior an emulated DOM cannot prove.

Do not duplicate the same assertion at every layer. Keep one strong contract test and add broader
tests only for integration risks.

## User-centered assertions

- Query elements by accessible role and name first, then label, text, or other user-visible meaning.
  Use test IDs only when no semantic query exists.
- Interact as a user would: focus, type, click, submit, and press keys. Do not call component methods
  or mutate internal state to reach a condition.
- Assert observable output, accessible state, navigation, focus, and network contract. Do not assert
  private classes, framework internals, reactive calls, or implementation sequence.
- Use async queries for async UI. Wait for the condition the user observes, not an arbitrary timeout.
- Cover keyboard behavior and focus order for every custom interaction pattern.
- Prefer explicit assertions over broad snapshots. Use a snapshot only for a stable serialized
  contract whose meaningful review is easier as a whole than as targeted assertions.

## Behavior matrix

Cover the states relevant to the changed contract:

- Default, supported variants, and boundary values.
- Loading, empty, error, retry, stale, and success.
- Disabled, readonly, required, invalid, expanded, selected, and pressed states where applicable.
- Text shorthand, composed snippets, and omitted optional regions for reusable content APIs.
- Keyboard-only operation, focus entry/exit, and focus restoration.
- Long text, missing optional content, localization, RTL, narrow viewport, and zoom/reflow.
- Dark theme, forced colors, reduced motion, and contrast-sensitive states for shared UI.
- Variant-by-state comparisons on every materially different backdrop: canvas, opaque island,
  translucent surface, popover, and dialog where applicable.
- Cancellation, out-of-order responses, and duplicate submission for asynchronous actions.

Do not manufacture cases that the public contract cannot reach.

## Isolation and doubles

- Keep tests deterministic and independent of order. Restore timers, globals, storage, fetch mocks,
  and DOM mutations after each test.
- Mock at system boundaries, not inside the unit under test. Prefer a lightweight fake with behavior
  over a deep mock that repeats implementation details.
- Use real parsing, validation, and component composition when inexpensive. Over-mocking creates a
  test for the mock graph rather than the product.
- Control clock, randomness, locale, and network explicitly when they affect output.
- Use framework-supported request interception for network tests. Never call a live third-party
  service from unit or component tests.
- Make mock failures loud: reject unhandled requests and unexpected calls.

## Vitest and Testing Library

- Use Vitest APIs documented for the installed version. Prefer typed dynamic module paths when a
  module mock is genuinely necessary.
- Keep `vi.mock` scope and hoisting rules visible. Refactor an inseparable module boundary rather
  than relying on a mock the runtime cannot intercept.
- Render DOM, not component instances. Follow Testing Library query priority and guiding principle:
  tests should resemble real use.
- Use the repository user-interaction helper when present. Reserve low-level event dispatch for the
  browser primitive being tested.
- Put native lifecycle and teardown races in a browser project, not jsdom. Exercise the real element
  API and event queue, including events delivered as a component unmounts, and assert the user-visible
  outcome rather than Svelte internals.
- Name tests as observable behavior in plain language. Group by public function, component, or user
  journey.

## Storybook

- Give every public component stories for meaningful variants and states, including interactive,
  disabled/error, long-content, and narrow-layout cases.
- Show related variants together. Include the concise text API and one realistic composed example;
  verify that optional regions can be omitted without leaving empty structure or spacing.
- Render sizes, colors, variants, and persistent/transient state combinations in aligned matrices.
  Keep docs wrappers interaction-neutral so table or tile hover styles cannot masquerade as component
  behavior.
- Keep stories deterministic, self-contained, and free of production side effects. Use loaders and
  decorators only for shared, explicit environment contracts.
- Add a docs page for every public component: intent, composition, accessibility contract, and
  examples. Use generated controls and ArgTypes for API reference instead of repeating a manual
  table. Document foundations separately from components.
- Enable accessibility analysis globally and make violations fail automated story tests when the
  installed Storybook integration supports it. Any exception must identify a documented false
  positive or intentional antipattern story.
- Run contrast assertions in the real browser against the exact rendered semantic pairs and states,
  not palette swatches alone. Automated tooling does not credit glow and may not understand every
  gradient, transparency stack, pseudo-element, or forced-colors result; inspect computed output and
  visually compare state separation in the supported surface contexts.
- Treat a near-threshold contrast violation as a real failure. Fix the owning semantic token and keep
  a rendered safety margin; do not disable the rule or add a consumer override merely because a
  calculated source value appeared to meet the minimum.
- Treat automated accessibility output as a first pass. Manually verify keyboard/focus behavior and
  relevant browser/assistive-technology combinations for complex widgets.
- Keep stories and docs in a private workbench. Keep reusable Storybook theme, preview, and test
  configuration in a separately consumable development package; never publish stories or runtime
  workbench code as part of the component package.
- Keep a representative framework or extraction fixture with the shared nodelib preset it validates.
  A platform tests its product catalogs, screen states, and wiring instead of copying that generic
  fixture locally.

## End-to-end tests

- Test critical journeys and browser contracts, not every visual variant.
- Use accessible locators and observable readiness conditions. Never use fixed sleeps.
- Keep test data isolated and clean it through supported product interfaces.
- Capture traces/screenshots on failure where the existing runner supports them; do not commit
  transient artifacts.
- Run at least the repository browser baseline for compatibility-sensitive behavior.

## Coverage and completion

Treat coverage as a map, not a target. Missing error, keyboard, security, or state-transition paths
matter; uncovered generated glue does not justify a test.

Run the narrow test during iteration, then:

```bash
pnpm lint
a-novel test --type=pnpm -y
a-novel build --type=pnpm -y
```

The change is complete only when new behavior has a regression test at the truthful layer and all
affected existing tests remain green.
