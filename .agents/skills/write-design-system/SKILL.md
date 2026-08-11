---
name: write-design-system
description: >
  Design-system conventions for Agora uikit foundations and reusable frontend components — CSS
  design tokens, calculated scales, semantic aliases, typography, themes, accessibility,
  component APIs, mandatory live Storybook review and handoff links, package boundaries, and publication hygiene. Load for
  any tokens, fonts, icons, theme, Storybook foundation, or shared UI component change. Load
  `plan-ui-design` before non-trivial visual direction, interaction-pattern, component-family, or
  foundation architecture. ALWAYS load `write-frontend`; add `write-svelte` for Svelte components
  and `write-frontend-tests` for stories and tests.
---

# Design-System Conventions

Load `write-frontend` first. For Svelte components load `write-svelte`; for every component story or
test load `write-frontend-tests`. Treat the design system as a public compatibility layer: visual
choices may evolve, but token names, component APIs, behavior, accessibility, and package exports
are consumer contracts.

Load `plan-ui-design` first when deciding what a flow, pattern, component family, or visual foundation
should be. Keep implementation-only maintenance here; do not reopen settled product design without
evidence.

**Rendered-UI hard gate:** Start the dark-default Storybook with `BROWSER=none` and `--no-open`,
inspect the exact changed story in the integrated browser, keep it live, and repeat its freshly
verified Markdown link in every status or final handoff. Never substitute a screenshot or placeholder.

## Package boundaries

Keep responsibilities independently consumable:

- **tokens**: dependency-free CSS custom properties and documented contracts.
- **fonts/icons/assets**: self-hosted files, declarations, provenance, and third-party licenses.
- **uikit**: reusable components that consume semantic foundations.
- **storybook**: private workspace package containing stories, docs, and development-only addons.

Never make a published package depend on Storybook. Set the Storybook package `private: true`; keep
its exports out of the UI package and release target. A platform imports foundations and components
explicitly so application code never pays for workbench code.

## Token architecture

Use three deliberate tiers:

1. **Bases and preset multipliers** define compact scale mathematics.
2. **Primitive tokens** expose generated palettes and metric scales to design-system authors.
3. **Semantic/component tokens** express intent consumed by applications and components.

Rules:

- Reduce independent hand-authored values. Derive spacing, control heights, radii, borders, focus,
  motion, type, and color scales from small bases plus named multipliers.
- Name semantic tokens by role, not appearance: `surface-canvas`, `text-muted`, `action-primary`, not
  `dark-gray` or `blue-9` in component CSS.
- Consume semantic tokens in components. Primitive steps are implementation detail unless a public
  foundation API explicitly exposes them.
- Keep calculations in CSS when the browser baseline supports them. Do not add a token build system
  merely to repeat CSS-native arithmetic.
- Treat color as five linked contracts: hue topology, tone/chroma scaling, output gamut and fallback,
  semantic foreground/background pairs, and optional graphic effects. Generate them from compact
  parameters, but expose each layer explicitly enough to test and evolve it independently.
- Use an Oklab-derived model such as OKLCH or OKHSL for perceptual color relationships when it fits
  the authoring need. Choose absolute or gamut-relative chroma deliberately, inspect every hue after
  mapping, and record rare optical corrections. Equal parameters do not guarantee equal appearance.
- Use a maintained constant-hue perceptual gamut-mapping implementation. Never clamp converted RGB
  channels as a palette algorithm: clipping can shift hue and flatten one family more than another.
- Emit a complete sRGB fallback before a Display P3 or other wide-gamut enhancement. Keep runtime
  packages free of the build-time converter and verify both outputs preserve semantic associations.
- Define grid, trace, outline, glow, gradient, and other visual treatments as semantic effect tokens
  derived from the same palette and metric scales. Keep them out of text contrast and state meaning;
  game-like effects default to static and must remain safe under reduced motion and forced colors.
- Test actual foreground/background pair contrast and rendered vividness in every supported theme.
- Use `rem`-based spacing and type, unitless line heights, named duration/easing tokens, and logical
  dimensions. Zero and intrinsic keywords need tokens only when they represent a selectable public
  design decision.
- Keep dark mode the Agora default. Add themes through semantic aliases rather than branching every
  component selector.
- Change or remove a published token only through the repository breaking-change/version workflow.

## Component contracts

- Start from the native element that already owns the semantics and behavior.
- Keep APIs small, typed, and composable. Prefer named variants/sizes and snippets over arbitrary
  styling booleans or internal-class hooks.
- Let content-bearing components accept framework-native children or snippets so consumers can
  compose text, markup, icons, and components. Use a string-only prop only when the semantic contract
  truly requires plain text; give icon-only controls a dedicated accessible-name contract.
- Forward the native attributes consumers reasonably need without permitting invalid state
  combinations. Keep defaults safe, especially button type and form behavior.
- Define hover, active, focus-visible, disabled, loading, invalid, selected, and high-contrast states
  as applicable. A component is not complete when only its resting screenshot works.
- Define state precedence explicitly. Persistent selected, checked, expanded, invalid, loading, and
  disabled states outrank transient hover and active treatments; transient feedback must not visually
  erase or contradict the persistent state.
- Keep interactive target size at least the WCAG 2.2 AA minimum; use a larger touch target where the
  product context allows it.
- Preserve long labels, localization, RTL, text zoom, narrow containers, reduced motion, and forced
  colors.
- Do not expose implementation selectors as API. Expose a documented CSS custom property only when
  consumer theming is an intentional contract.

## Storybook as the review surface

- Document every public component and every foundation that affects rendering.
- For a component, use a short stable reference: one-line purpose, rendered examples, usage,
  variants, composition, states, accessibility, and API as applicable. Explain what exists and how
  to use it. Keep decision history, rejected alternatives, rollout notes, and nonessential rationale
  out of consumer docs.
- For foundations, render color, typography, spacing, shape, border, elevation, iconography, and
  motion systems as applicable. Show token names beside rendered values.
- Prefer official maintained Storybook blocks/addons. Write a small local block only when official
  tooling cannot express the contract; do not add a dependency for a decorative docs widget.
- Default manager, docs, and canvas to the Agora dark theme. Keep a contrasting background option
  for component verification.
- Start local Storybook for every rendered UI change with both `BROWSER=none` and `--no-open`; the
  process must not launch an external browser tab. Wait for readiness and keep it running for the
  operator's review unless they ask to stop it.
- Open the exact changed story or docs route in the integrated browser. Include that live URL as a
  clickable inline Markdown link in every status or final handoff, re-resolving and repeating it
  even when it appeared in an earlier response.
- Inspect composed content and combined or transient states in the rendered canvas: selected plus
  hovered, focused, open, loading, invalid, disabled, long-label, and narrow-container cases as
  applicable. Do not infer them from selectors or isolated snapshots.
- Run the accessibility panel for every story. Configure automated story accessibility checks to
  fail where supported, while retaining manual keyboard and assistive-technology review.

## Change sequence

1. Define or amend the smallest foundation contract.
2. Implement the component using semantic tokens and native behavior.
3. Add behavior tests and the complete story state matrix.
4. Add or update component/foundation documentation.
5. Run format, lint, pnpm tests, package/production builds, and Storybook build.
6. Start Storybook and inspect the rendered change at narrow and wide viewports, keyboard-only,
   reduced motion, and relevant theme/contrast settings.
7. Inspect each publishable package archive before release.

Do not approve a visual contract from source alone.

## Publication hygiene

- Keep package `exports` and `files` allowlists explicit. Exclude tests, stories, fixtures,
  Storybook, generated reports, and private source.
- Ship README, license, declarations, required CSS/assets, and third-party license/provenance files.
- Keep runtime dependencies minimal; load `choose-dependency` before adding any library.
- Test imports through the package root and documented subpaths. Deep source imports are not a
  supported contract.
- Build Storybook as a verification artifact, never as an automatically published package or public
  deployment unless the user explicitly requests and scopes that publication.

## Review checklist

- Component CSS contains no raw product colors, spacing, radii, borders, type, or motion values.
- Bases and multipliers are fewer than generated public choices and have documented meaning.
- Color documentation states the harmony, tone/chroma formula, gamut mapping, fallback, semantic
  pairings, and effect layer without hiding family-specific exceptions.
- Semantic names survive a palette or density change.
- Every interaction state is keyboard-operable, visibly focused, and represented in stories/tests.
- Persistent states keep their hierarchy under hover and active input; reusable content slots render
  text, icon-and-text, and component composition without API workarounds.
- Foundation and component docs render the actual package code.
- Wide-gamut and sRGB renderings preserve role, contrast, and hierarchy; decorative effects do not
  carry meaning and do not flash or move by default.
- Storybook remains private, starts without opening an external tab, stays live for review, and is
  linked in the handoff.
- Package archives contain only supported consumer files and correct licenses.
