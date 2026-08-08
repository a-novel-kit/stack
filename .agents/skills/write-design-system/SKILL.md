---
name: write-design-system
description: >
  Design-system conventions for Agora uikit foundations and reusable frontend components — CSS
  design tokens, calculated scales, semantic aliases, typography, themes, accessibility,
  component APIs, Storybook documentation, package boundaries, and publication hygiene. Load for
  any tokens, fonts, icons, theme, Storybook foundation, or shared UI component change. ALWAYS load
  `write-frontend`; add `write-svelte` for Svelte components and `write-frontend-tests` for stories
  and tests.
---

# Design-System Conventions

Load `write-frontend` first. For Svelte components load `write-svelte`; for every component story or
test load `write-frontend-tests`. Treat the design system as a public compatibility layer: visual
choices may evolve, but token names, component APIs, behavior, accessibility, and package exports
are consumer contracts.

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
- Use OKLCH for perceptual color relationships when supported by the declared baseline. Derive
  families from shared lightness/chroma/hue controls; test actual pair contrast because a scale
  position is not an accessibility guarantee.
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
- Forward the native attributes consumers reasonably need without permitting invalid state
  combinations. Keep defaults safe, especially button type and form behavior.
- Define hover, active, focus-visible, disabled, loading, invalid, selected, and high-contrast states
  as applicable. A component is not complete when only its resting screenshot works.
- Keep interactive target size at least the WCAG 2.2 AA minimum; use a larger touch target where the
  product context allows it.
- Preserve long labels, localization, RTL, text zoom, narrow containers, reduced motion, and forced
  colors.
- Do not expose implementation selectors as API. Expose a documented CSS custom property only when
  consumer theming is an intentional contract.

## Storybook as the review surface

- Document every public component and every foundation that affects rendering.
- For a component, include intent, API, variants, sizes, composition, behavior, accessibility
  contract, and meaningful edge states.
- For foundations, render color, typography, spacing, shape, border, elevation, iconography, and
  motion systems as applicable. Show token names beside rendered values.
- Prefer official maintained Storybook blocks/addons. Write a small local block only when official
  tooling cannot express the contract; do not add a dependency for a decorative docs widget.
- Default manager, docs, and canvas to the Agora dark theme. Keep a contrasting background option
  for component verification.
- Start local Storybook with both `BROWSER=none` and `--no-open`; opening a browser is the operator or
  agent caller's decision.
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
- Semantic names survive a palette or density change.
- Every interaction state is keyboard-operable, visibly focused, and represented in stories/tests.
- Foundation and component docs render the actual package code.
- Storybook remains private and passive on startup.
- Package archives contain only supported consumer files and correct licenses.
