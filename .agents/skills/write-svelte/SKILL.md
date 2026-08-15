---
name: write-svelte
description: >
  Svelte 5 and SvelteKit conventions for a-novel frontend applications and shared components. Load
  it whenever creating, editing, reviewing, or debugging a .svelte file, .svelte.ts module,
  SvelteKit route/load/action/hook, Svelte package export, or Svelte compiler configuration. ALWAYS
  load `write-frontend` alongside it; add `write-frontend-tests` for tests or stories and
  `write-design-system` for uikit components or tokens.
---

# Svelte Conventions

Load `write-frontend` first. Read the installed Svelte/SvelteKit versions, compiler configuration,
nearby components, and official documentation for the installed major version before choosing an
API. New code uses Svelte 5 idioms unless an existing compatibility boundary requires legacy syntax.

## Component boundaries

- Give a component one clear semantic responsibility. Split by behavior or ownership, not by an
  arbitrary line count.
- Keep state in the lowest component that owns it. Pass explicit data and callbacks; use context only
  for stable, tree-wide capabilities that prop threading would obscure.
- Build small semantic components that compose like bricks. Use props for behavior and stable state;
  use typed snippets, children, and neighboring components for optional content and arrangement.
  Split an API that grows presentation booleans or position switches.
- Let one content prop accept text shorthand or a typed snippet when both are valid. Collection items
  keep a plain text name for accessibility and typeahead and use a separate snippet for visual content.
- Preserve the native element API where practical. Wrapper components must not silently remove form,
  focus, keyboard, or accessibility behavior.
- Keep route/business orchestration out of reusable visual components. Keep platform-only imports out
  of publishable packages.
- Let shared layout components own only geometry, responsive behavior, and typed composition points.
  Keep landmarks, navigation, headings, labels, surfaces, route orchestration, and domain workflow in
  the consuming application unless one is the explicit semantic responsibility of a smaller primitive.

## Svelte 5 reactivity

- Declare a typed props shape and destructure `$props()`. Give defaults at the boundary.
- Use `$bindable` only when two-way ownership is part of the public contract; ordinary props remain
  one-way.
- Use `$state` for owned mutable state and `$derived` for values computed from state. Do not mirror a
  derivation through `$effect`.
- Reserve `$effect` for synchronization with an external system. Return cleanup for subscriptions,
  observers, timers, and listeners.
- Keep effects narrowly dependent and safe to repeat. Never depend on effect ordering to make state
  valid.
- Use callback props for component events in new Svelte 5 code. Type payloads and callback return
  values; do not dispatch an event merely to mutate parent state indirectly.
- Use event attributes such as `onclick` in new Svelte 5 code. Do not mix legacy `on:` directives
  with event attributes inside one component.
- Use snippets and `{@render ...}` for new composition APIs. Follow an existing public legacy-slot
  contract until a planned breaking change migrates consumers.

## Markup and accessibility

- Apply `write-frontend` native-semantics rules inside every template. Prefer native controls over
  event handlers on generic elements.
- Treat every Svelte accessibility compiler warning as a defect. Suppress only a verified false
  positive with the narrow rule name and an explanation of the satisfied behavior.
- Do not use `{@html}` for untrusted or user-controlled content. A type annotation is not sanitization.
- Use stable IDs for labels, descriptions, and errors. Avoid IDs derived from list indexes when items
  can move.
- Key each block by stable identity whenever instances can be inserted, removed, or reordered.
- Keep focus transitions intentional for dialogs, menus, route changes, validation, and async content.

## Rendering and lifecycle

- Keep module initialization and template expressions free of side effects.
- Guard `window`, `document`, storage, observers, and browser-only libraries behind browser-aware
  lifecycle or environment boundaries so SSR and prerendering remain valid.
- Do not repair hydration mismatches by disabling SSR. Make server and client input deterministic.
- Clean up timers, subscriptions, observers, and global listeners when ownership ends.
- Use actions or attachments for reusable DOM integration; keep their setup and teardown symmetric.
- Avoid imperative component APIs unless integration with a non-Svelte host requires them.

## Styling

- Keep component styles scoped. Use `:global` only at an explicit application-shell or third-party
  integration boundary, with the reason visible beside it.
- Consume design tokens instead of declaring product values. Load `write-design-system` when a token
  or reusable component contract changes.
- Prefer class/state attributes and CSS pseudo-classes over inline style strings. CSS custom
  properties are the supported escape hatch for calculated values.
- Preserve logical properties, responsive content flow, reduced motion, forced colors, and visible
  focus behavior from `write-frontend`.

## SvelteKit applications

- Put secrets, privileged calls, and server-only dependencies in server-only modules and server load
  functions/actions. Never import them through a module reachable by browser code.
- Use framework `load`, form actions, error, and redirect contracts rather than duplicating routing
  or request lifecycle logic in components.
- Return serializable data across the server/client boundary. Validate URL, form, cookie, and API
  input at runtime.
- Use progressive-enhancement helpers without making the enhanced path the only functional path.
- Keep layouts responsible for shared page structure and route files responsible for route data;
  extract reusable presentation and behavior into components/modules.

## Published Svelte packages

- Export only intentional public entry points. Keep internal modules unreachable through package
  `exports`.
- Add concise JSDoc/TSDoc to exported components, props, snippet contracts, callbacks, and non-obvious
  invariants. Describe usage and behavior without restating the type; keep maintainer comments for
  decisions the code cannot make evident.
- Compile or lint documentation examples as Svelte/HTML/TypeScript with the declared language. Keep
  setup imports separate from illustrative component composition.
- Declare Svelte as a compatible peer dependency and emit the documented `svelte` export condition.
- Keep application, Storybook, fixtures, tests, and source-only tooling out of the tarball. Inspect
  the packed archive before release.
- Ship README, license, declarations, and required styles/assets. Test consumption from the package
  root rather than private source paths.
- Avoid module-level browser state in libraries; multiple consumers and SSR requests must remain
  isolated.

## Validation

Run `svelte-check` through the repository lint/typecheck script, component tests through
`a-novel test --type=pnpm -y`, and the production/package build through
`a-novel build --type=pnpm -y`. For a published package, also inspect the package archive and verify
that every documented import resolves.
