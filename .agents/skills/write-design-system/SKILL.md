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
evidence. Apply [the visual-language implementation rules](../plan-ui-design/references/visual-language.md)
when translating screenshot studies or a high-contrast, game-like direction into reusable tokens.

**Rendered-UI hard gate:** Start the dark-default Storybook with `BROWSER=none` and `--no-open`,
inspect the exact changed story in the integrated browser, keep it live, and repeat its freshly
verified Markdown link in every status or final handoff. Never substitute a screenshot or placeholder.

## Package boundaries

Keep responsibilities independently consumable:

- **tokens**: dependency-free CSS custom properties and documented contracts.
- **fonts/icons/assets**: self-hosted files, declarations, provenance, and third-party licenses.
- **uikit**: reusable components that consume semantic foundations.
- **storybook-config**: separately consumable development package exporting the shared theme,
  preview setup, decorators, docs blocks, addons, and test configuration.
- **storybook-workbench**: private workspace package containing this repository's stories and docs.

Never make a runtime package depend on Storybook. Keep workbench exports out of the UI package and
release target; consumers install the reusable configuration explicitly as development tooling. A
platform imports foundations and components explicitly so application code never pays for workbench
code. Deploy a static Storybook only through an explicit, reusable documentation workflow; never
publish the private workbench itself as a package.

Use one curated, maintained icon source through typed component exports. Bundle custom SVG icons and
their provenance with the asset package; do not require a remote icon service or icon font at runtime.

## Token architecture

Use three deliberate tiers:

1. **Bases and preset multipliers** define compact scale mathematics.
2. **Primitive tokens** expose generated palettes and metric scales to design-system authors.
3. **Semantic/component tokens** express intent consumed by applications and components.

Rules:

- Reduce independent hand-authored values. Derive spacing, control heights, radii, borders, focus,
  motion, type, and color scales from small bases plus named multipliers. Use named density,
  hierarchy, and major-composition ratios instead of applying the golden ratio to every dimension.
- Name semantic tokens by role, not appearance: `surface-canvas`, `text-muted`, `action-primary`, not
  `dark-gray` or `blue-9` in component CSS.
- Consume semantic tokens in components. Primitive steps are implementation detail unless a public
  foundation API explicitly exposes them.
- Keep calculations in CSS when the browser baseline supports them. Do not add a token build system
  merely to repeat CSS-native arithmetic.
- Define canonical product colors once as perceptual components and derive every palette, alpha,
  overlay, and component state from them with `calc()`, relative color syntax, or `color-mix()`.
  Do not introduce hexadecimal or RGB design values beside the canonical model. Derive transparency
  from a semantic color instead of maintaining an opaque duplicate with a hand-authored alpha.
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
- Build dark neutral ramps with a low-chroma field and a deliberately non-linear tone curve so early
  surfaces remain dark while upper text steps separate clearly. Build accent ramps with perceptual,
  gamut-aware curves that preserve vivid upper steps without making every step pastel or fluorescent.
- Set a chroma budget by semantic region. Let one accent dominate, use a second only for a stable
  distinction, and keep most area neutral; equal access to every palette family is not equal usage.
- Test actual foreground/background pair contrast and rendered vividness in every supported theme.
  Gate semantic text and control pairs, not merely adjacent primitive steps.
- Derive invisible groups and semi-opaque island surfaces from semantic surface, opacity, blur, and
  elevation tokens. Use backdrop blur as progressive enhancement and retain legible separation
  without it.
- Make overlay and interaction tokens relative to their intended backdrop. A hover layer must remain
  distinguishable on canvas, island, glass, popover, and dialog surfaces; do not reuse the same
  opaque surface token for both the backdrop and the control state.
- Define gradient interpolation in OKLCH or Oklab explicitly. When distant endpoints produce a muddy
  midpoint, add an intentional transition stop or route instead of falling back to RGB interpolation.
- Derive localized glow from the emitting object's characteristic size and the metric scale; roughly
  one eighth is a useful initial blur proportion, not a fixed rule. Layer a tighter bright halo with a
  weaker outer halo. Keep text unblurred and avoid permanent bloom on ordinary controls.
- Use `rem`-based spacing and type, unitless line heights, named duration/easing tokens, and logical
  dimensions. Zero and intrinsic keywords need tokens only when they represent a selectable public
  design decision.
- Keep dark mode the Agora default. Add themes through semantic aliases rather than branching every
  component selector.
- Change or remove a published token only through the repository breaking-change/version workflow.

## Component contracts

- Start from the native element that already owns the semantics and behavior.
- Choose boundaries in this order: composition and gap, surface or opacity shift, elevation, then a
  visible border when interaction, structure, or forced-colors fallback requires it. Controls may
  retain borders for affordance; decorative boxes do not earn them by default.
- Treat an island as a composable surface primitive, not a universal card. Keep main content groups
  transparent when spacing and hierarchy are sufficient, and use borderless semi-opaque islands for
  floating navigation, tools, overlays, or independently scannable regions.
- Keep shared layout primitives limited to generic geometry, responsiveness, and composition points.
  Applications own page landmarks, navigation models, route structure, product labels, workflow
  steps, and final shell compositions; do not ship a demo-shaped application skeleton from uikit.
- Optically align asymmetric icons and marks inside their real control context; do not expose random
  offsets as consumer props.
- Design components as small semantic bricks. A prop earns its place when it changes semantics,
  intrinsic behavior or state, or a stable visual variant the component owns. Compose optional
  content, adornments, actions, and arrangement from snippets, children, and neighboring primitives.
- Split a component when its API starts accumulating presentation toggles, position switches, or
  mutually dependent options. Do not turn common compositions into one configurable super-component.
- Let a content region accept text shorthand and a framework-native snippet through the same
  contract. Consumers must be able to compose text, markup, icons, and components without an API
  workaround. Keep plain strings where the semantic contract requires a stable accessible name or
  typeahead value; expose a separate visual renderer for collection items.
- Forward the native attributes consumers reasonably need without permitting invalid state
  combinations. Keep defaults safe, especially button type and form behavior.
- Define hover, active, focus-visible, disabled, loading, invalid, selected, and high-contrast states
  as applicable. A component is not complete when only its resting screenshot works.
- Define state precedence explicitly. Persistent selected, checked, expanded, invalid, loading, and
  disabled states outrank transient hover and active treatments; transient feedback must not visually
  erase or contradict the persistent state.
- Keep an Agora control's selected treatment stable while hovered. Use a modest lightening for hover
  and localized glow for selection when the component contract calls for it; keep resting, hovered,
  selected, and disabled emphasis visibly distinct. Preserve each variant's semantic color family
  unless a cross-family state meaning is explicit.
- Reset native control appearance only within a component that fully restores focus-visible,
  disabled, invalid, autofill, high-contrast, and platform input behavior. Prefer native controls;
  implement a custom select, combobox, menu, or dialog only when the native contract cannot satisfy
  the required composition or behavior, then follow the corresponding APG keyboard and focus model.
- Compose form labels, descriptions, errors, start/end adornments, and actions as optional primitives
  or snippets around the essential control. Present a compound control through its wrapper's
  `:focus-within` state while focus remains on the native child; suppress hover treatments that obscure
  focus or invalid state. Keep open lists bounded by a configurable maximum block size with overflow
  scrolling, disabled options, and an explicit empty selection when the contract permits it.
- Share surface, option-row, spacing, radius, and elevation tokens across listbox, menu, and popover
  families while preserving each widget's distinct selection and keyboard model.
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
- Use Storybook's generated controls and ArgTypes as the API reference; do not repeat them in a
  handwritten API table. Keep setup separate from examples, and list only packages consumers can
  actually install.
- Show related variants, sizes, colors, and states together in aligned grids or compact custom
  renderers. The wrapper must not add row, tile, or hover effects that can be mistaken for component
  behavior.
- For foundations, render color, typography, spacing, shape, border, elevation, iconography, and
  motion systems as applicable. Show token names beside rendered values.
- Prefer official maintained Storybook blocks/addons. Write a small local block only when official
  tooling cannot express the contract; do not add a dependency for a decorative docs widget.
- Default manager, docs, and canvas to the Agora dark theme. Keep a contrasting background option
  for component verification.
- Align Storybook manager and docs typography and color roles with Agora while retaining Storybook's
  default spacing and widget geometry unless a verified clash requires a narrow override.
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
- When static Storybook publication is requested, use the shared CI workflow, publish only the
  generated static artifact, verify its base path and asset loading, and exclude secrets and private
  environment data.

## Review checklist

- Component CSS contains no raw product colors, spacing, radii, borders, type, or motion values.
- Public components remain small semantic bricks; optional presentation composes without boolean or
  position-prop matrices.
- Bases and multipliers are fewer than generated public choices and have documented meaning.
- Color documentation states the harmony, tone/chroma formula, gamut mapping, fallback, semantic
  pairings, and effect layer without hiding family-specific exceptions.
- Dark neutral and accent ramps preserve their intended dynamic range in rendered primitive,
  semantic, control, and navigation contexts; semantic foreground/background pairs pass their gate.
- Translucent interaction layers remain distinct across canvas, island, glass, popover, and dialog
  contexts without introducing a second hard-coded color system.
- Borders are reserved for meaningful affordance or structure. Invisible groups and translucent
  islands remain distinguishable through composition, surface, and elevation.
- Glow is localized, stateful, proportional, and absent from text; gradient routes avoid accidental
  muddy midpoints.
- Semantic names survive a palette or density change.
- Every interaction state is keyboard-operable, visibly focused, and represented in stories/tests.
- Persistent states keep their hierarchy under hover and active input; reusable content slots render
  text, icon-and-text, and component composition without API workarounds.
- Shared layout exports are domain-agnostic geometry primitives, not application shells or workflow
  recipes.
- Foundation and component docs render the actual package code.
- Wide-gamut and sRGB renderings preserve role, contrast, and hierarchy; decorative effects do not
  carry meaning and do not flash or move by default.
- The Storybook workbench remains private, shared configuration stays development-only, and the local
  server starts without opening an external tab, stays live for review, and is linked in the handoff.
- Package archives contain only supported consumer files and correct licenses.
