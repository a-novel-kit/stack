---
name: write-design-system
description: >
  Implement or review uikit tokens, fonts, icons, themes, Storybook foundations, and shared UI
  components. Load for these changes; always pair with write-frontend, plus applicable Svelte/tests.
  Use plan-ui-design for material visual, interaction, component-family, or foundation decisions.
---

# Design-System Conventions

Load `write-frontend` first. For Svelte components load `write-svelte`; for every component story or
test load `write-frontend-tests`, which holds the
[Vitest screenshot rules](../write-frontend-tests/SKILL.md#end-to-end-tests-and-screenshot-comparisons)
for story comparisons. Treat the design system as a public compatibility layer: visual choices may
evolve, but token names, component APIs, behavior, accessibility, and package exports are consumer
contracts.

Load `plan-ui-design` first when deciding what a flow, pattern, component family, or visual foundation
should be. Keep implementation-only maintenance here; do not reopen settled product design without
evidence. Apply [the visual-language implementation rules](../plan-ui-design/references/visual-language.md)
when translating screenshot studies or a high-contrast, game-like direction into reusable tokens.

**Rendered-UI hard gate:** Start the dark-default Storybook with `BROWSER=none` and `--no-open`,
inspect the exact changed story in the integrated browser, keep it live, and repeat its freshly
verified Markdown link in every status or final handoff, with screenshots of the changed screens sent
in the conversation. A screenshot or placeholder never substitutes for the live link.

## Package boundaries

Keep responsibilities independently consumable:

- **tokens**: dependency-free CSS custom properties and documented contracts.
- **fonts**: self-hosted files, declarations, provenance, and third-party licenses.
- **uikit**: reusable components that consume semantic foundations.
- **storybook-config**: separately consumable development package exporting the shared theme,
  preview setup, decorators, docs blocks, addons, and test configuration.
- **storybook-workbench**: private workspace package containing this repository's stories and docs.

Never make a runtime package depend on Storybook. Keep workbench exports out of the UI package and
release target; consumers install the reusable configuration explicitly as development tooling. A
platform imports foundations and components explicitly so application code never pays for workbench
code. Deploy a static Storybook only through an explicit, reusable documentation workflow; never
publish the private workbench itself as a package.

Use one curated, maintained icon source through typed component exports: uikit takes its icons from
`@lucide/svelte`. Bundle custom SVG icons and their provenance with an asset package; do not require
a remote icon service or icon font at runtime.

## Brand images

`@a-novel-kit/ui-images` publishes logo banners and icons from
[a-novel-kit/ui-assets](https://github.com/a-novel-kit/ui-assets), which releases apart from uikit,
so uikit's CI never downloads their LFS sources. uikit's Storybook documents them from the published
package. Each logo keeps its sources in `packages/images/raw/<logo>/` there: the GIMP project, its
export plug-in, the fonts, and the `exports/*.png` files the generator reads. Git LFS stores `raw/`;
follow [Git LFS](../git-conventions/SKILL.md#git-lfs) before committing there. The generated files
in `packages/images/files/` stay in plain Git, so every checkout reads them without LFS.

`scripts/generate-images.mjs` lists the logos, sizes, and variants. After changing a source, run
`git lfs pull`, then `pnpm generate:images`; `pnpm lint:generated` fails on drift. Banners are keyed
by height (`banner/{h}h/`), the one dimension every logo shares. Icons are keyed `icon/{n}x{n}/`.
File names end in a variant suffix: none for dark on transparent, `-black` for dark on black,
`-light` for light on transparent, and `-light-white` for light on white. Every ui-assets tag
republishes the package, about 10 MB, so each added size or variant ships in every release. READMEs
in both organizations link `files/` on ui-assets' `master`, so renaming or removing a published file
breaks them the moment it merges; update those READMEs with the change.

## Token architecture

Use three deliberate tiers:

1. **Bases and named ratios** define compact scale mathematics. Each step multiplies its base by a
   plain number in `calc()`, as `--space-4: calc(var(--space-base) * 4)`.
2. **Primitive tokens** expose generated palettes and metric scales to design-system authors.
3. **Semantic/component tokens** express intent consumed by applications and components.

Rules:

- Reduce independent hand-authored values. Derive spacing, control heights, radii, borders, focus,
  motion, and type scales from small bases times plain numbers; a multiplier is never a token. Use
  named density, hierarchy, and major-composition ratios instead of applying the golden ratio to
  every dimension.
- Name semantic tokens by role, not appearance: `surface-canvas`, `text-muted`, `action-primary`, not
  `dark-gray` or `blue-9` in component CSS.
- Consume semantic tokens in components. Primitive steps are implementation detail unless a public
  foundation API explicitly exposes them.
- Keep calculations in CSS when the browser baseline supports them. Do not add a token build system
  merely to repeat CSS-native arithmetic. The palette is the one generated part: the tokens
  package's `scripts/generate-colors.mjs` samples each hue's gamut and writes every primitive step
  to `colors.css` as a literal `oklch()` color. Change a step through the generator's bases, never
  by hand; `pnpm lint:generated` fails on drift.
- Define canonical product colors once as perceptual components, the generator's OKLCH bases. Derive
  every semantic alpha, overlay, and component state from the palette with `color-mix()`. Do not
  introduce hexadecimal or RGB design values beside the canonical model. Derive transparency from a
  semantic color instead of maintaining an opaque duplicate with a hand-authored alpha.
- Treat color as five linked contracts: hue topology, tone/chroma scaling, output gamut and fallback,
  semantic foreground/background pairs, and optional graphic effects. Generate them from compact
  parameters, but expose each layer explicitly enough to test and evolve it independently.
- Use an Oklab-derived model such as OKLCH or OKHSL for perceptual color relationships when it fits
  the authoring need. Choose absolute or gamut-relative chroma deliberately, inspect every hue after
  mapping, and record rare optical corrections. Equal parameters do not guarantee equal appearance.
- Use a maintained constant-hue perceptual gamut-mapping implementation. Never clamp converted RGB
  channels as a palette algorithm: clipping can shift hue and flatten one family more than another.
- Emit each palette step as a device-independent `oklch()` color, which the browser maps to the
  display's gamut at paint time. Keep runtime packages free of the build-time converter and verify
  that sRGB and Display P3 renderings preserve semantic associations.
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
- Do not tune a semantic pair to the exact WCAG threshold. Keep a deliberate safety margin and test
  computed output on every supported backdrop; color conversion, alpha composition, font rendering,
  and browser rounding can turn a mathematically borderline pair into a real rendered failure.
  Fix the shared semantic token rather than weakening a consumer accessibility gate.
- Derive invisible groups and semi-opaque island surfaces from semantic surface, opacity, blur, and
  elevation tokens. Every browser in the
  [baseline](../write-frontend/SKILL.md#performance-and-compatibility) renders backdrop blur,
  Safari before 18 through `-webkit-backdrop-filter`, so it needs no `@supports` wrapper or opaque
  fallback. Keep the prefixed line until March 2027, when the unprefixed property becomes widely
  available.
- A backdrop blur reads only what lies under its own box, so whatever crosses a frosted edge stays
  sharp there. Where something can cross it, as a button under an open list does, paint the frost
  on a layer reaching past the box by twice the blur radius, clip it back with a layer of the box's
  own size (`overflow: clip`) that sits under the box's outline, and scroll the content inside, so
  the layer stays in place. uikit's internal
  Listbox, behind Select and Combobox, shows the pattern.
- A backdrop blur is recomputed over its whole box for every frame drawn under it, whatever changed,
  and costs by area, not radius. Under a modal Dialog's full-viewport backdrop blur, the page's own
  frosted surfaces (cards, sticky bars, the consent banner, toasts) sit unseen yet still cost each
  frame, so they drop their blur while `:root:has(dialog.dialog:modal)` holds: once the backdrop has
  faded in, through a zero-length `backdrop-filter` transition delayed by `--duration-normal`, so
  none shows unfrosted, and back at once on close. One inside a dialog keeps it. A uikit frosted
  page surface carries `data-frost`; Dialog drops every such blur under a modal dialog. On
  Account's dialogs, this cut 28% of each frame under software compositing and 75% of GPU time
  (a-novel-kit/uikit#350). Any frame whose change touches a backdrop
  blur's box blurs that whole box again, and a modal backdrop's box is the viewport: a hover or a
  keystroke inside a dialog costs 35 ms or more at 1080p under software compositing, against 1.4 ms on
  a GPU, so a browser without hardware acceleration feels it most. Two rules follow. What animates on
  the page pauses under a modal dialog (`animation-play-state`), since each of its frames would blur
  the page again: a dialog over a loading page then draws no frame at rest. A fullscreen dialog's
  backdrop carries no blur, since the dialog covers it. A `filter: blur()` on the root is no
  substitute: it costs the same on a GPU and lightens the viewport's edges, and on any other element
  it unpins every fixed descendant.
- A modal dialog makes every node outside it inert, the top layer included, so a popover cannot float
  a request above it. A request the reader must stay able to answer, such as the consent banner, also
  renders inside the topmost modal Dialog (uikit's internal `overModal` list), where Account's login on
  a page that needs one would otherwise leave it unanswerable. The page's own copy stays, inert and
  under the backdrop, so the copy inside crossfades over it with the dialog; while a request waits, a
  dialog fades without rising, since a transform would carry the fixed copy along.
- Make overlay and interaction tokens relative to their intended backdrop. A hover layer must remain
  distinguishable on canvas, island, glass, popover, and dialog surfaces; do not reuse the same
  opaque surface token for both the backdrop and the control state.
- Define gradient interpolation in OKLCH or Oklab explicitly. When distant endpoints produce a muddy
  midpoint, add an intentional transition stop or route instead of falling back to RGB interpolation.
- Derive localized glow from the emitting object's characteristic size and the metric scale; roughly
  one eighth is a useful initial blur proportion, not a fixed rule. Layer a tighter bright halo with a
  weaker outer halo. Keep text unblurred and avoid permanent bloom on ordinary controls.
- Use `rem`-based spacing and type, line heights that come with their text style, named motion
  tokens, and logical dimensions. Zero and intrinsic keywords need tokens only when they represent a selectable public
  design decision.
- Keep dark mode the A-Novel default. Add themes through semantic aliases rather than branching every
  component selector.
- Change or remove a published token only through the repository breaking-change/version workflow.
- Give every step of a public scale one role, written beside its definition in `tokens.css` and in
  [the ladders](references/ladders.md). A step nothing consumes is removed: an unused step is a
  choice left open. Two names for one value are one too many, as `--color-surface-overlay` doubled
  `--color-surface-island-strong` until uikit dropped it.
- Bind choices that travel together in one token. A text style (`--text-*`) is a `font` shorthand
  value that sets weight, size, line height and family at once, so no element picks them apart.
- Hold the rules with the token contract check, `packages/tokens/scripts/check-token-contract.mjs`,
  which `pnpm lint` runs in uikit and each platform runs as `uikit-token-contract src`. It fails on a
  token nothing defines, on a property that reads outside its family, and on a raw value where a
  scale exists. A CSS custom property that is misspelled or removed fails silently in the browser:
  this check is what reports it.

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
- Center a consumer's rendered option in its row as a box: the span holding it is a flex container
  that centers its items. An inline-flex box that starts with an icon or a flag takes the icon's bottom
  edge as its baseline, so set on a text line it rides above the row's middle, as Account's language
  select did by 2px until uikit's Select, Combobox and ActionMenu centered it.
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
- Give every stateful component one external controller contract and a configurable default controller.
  Keep rendered state and semantic transitions in the controller; keep native event translation, DOM
  references, focus movement, measurements, and accessibility attributes in the component. Do not
  expose both a controller and bindable state props or generic `setState` methods.
- Export component-specific controller types even when several alias the same generic state shape, so
  consumer APIs and migration guides remain discoverable. Storybook supplies fixed-state controllers
  under the `write-frontend-tests` review contract; the component renders the accepted state even when
  a native event requests a rejected transition.
- Define state precedence explicitly. Persistent selected, checked, expanded, invalid, loading, and
  disabled states outrank transient hover and active treatments; transient feedback must not visually
  erase or contradict the persistent state.
- Keep an A-Novel control's selected treatment stable while hovered. Use a modest lightening for hover
  and localized glow for selection when the component contract calls for it; keep resting, hovered,
  selected, and disabled emphasis visibly distinct. Preserve each variant's semantic color family
  unless a cross-family state meaning is explicit.
- Hover only where a pointer hovers: every `:hover` treatment sits inside `@media (hover: hover)`. A
  finger otherwise leaves the last tapped control hovered, and a tab bar scrolled sideways lights
  the tabs it passes. `:active` keeps the pressed look on touch.
- Remove a border with `border-width: 0`, never the `border: 0` shorthand, where the element's
  border color transitions: the shorthand resets the color to the text color, and the next state
  fades from it as a light outline. The shells' rail buttons flashed one each time the rail
  collapsed.
- Reset native control appearance only within a component that fully restores focus-visible,
  disabled, invalid, autofill, high-contrast, and platform input behavior. Prefer native controls;
  implement a custom select, combobox, menu, or dialog only when the native contract cannot satisfy
  the required composition or behavior, then follow the corresponding APG keyboard and focus model.
- Compose form labels, descriptions, errors, start/end adornments, and actions as optional primitives
  or snippets around the essential control. Present a compound control through its well (uikit's
  internal `Well`), lit while its marked native control (`data-well-control`) shows focus, so a
  button inside the field never lights it. Suppress hover treatments that obscure focus or invalid
  state. Keep open lists bounded by a configurable maximum block size with overflow scrolling,
  disabled options, and an explicit empty selection when the contract permits it.
- Keep every button inside a field ghost only: no border, ring or glow in any state, just its icon,
  a background and a color. The field already outlines the control, and a second outline inside it
  reads as a separate control. Only the keyboard focus outline stays. Build such buttons from uikit's
  internal `FieldButton`, as the PasswordInput toggle and the Combobox trigger do, and place them in
  the well's end group (`data-well-end`).
- Share surface, option-row, spacing, radius, and elevation tokens across listbox, menu, and popover
  families while preserving each widget's distinct selection and keyboard model.
- A floating surface leaves its inset to its owner, since the inset sets the surface's distance from
  its source. uikit's `FloatingSurface` reset it on anchored surfaces until uikit#317, which pressed
  every list, menu and tooltip against its source. A surface the browser leaves away from its
  source, as one without anchor positioning does, or Safari on iOS 26 with its floating toolbars,
  is placed by script under its source at the owner's gap, and follows the page as it scrolls;
  never centered on the screen, where it reads as far from its field.
- Keep interactive target size at least the WCAG 2.2 AA minimum; use a larger touch target where the
  product context allows it.
- Preserve long labels, localization, RTL, text zoom, narrow containers, reduced motion, and forced
  colors.
- Do not expose implementation selectors as API. Expose a documented CSS custom property only when
  consumer theming is an intentional contract.

## The A-Novel visual direction

Design rules lead and code follows: each rule names what the eye should read, then the token or
contract that delivers it. The thesis is a writer's room at night: a quiet blue-black field, lit
slate surfaces, and one colored lamp in the corner. Warmth means a calm, relaxing read, not warmer
neutrals. Keep the canonical brand, pressure, and signal colors stable unless the brief explicitly
changes them.

The rules group by what they shape. Read the group a change touches, and the first two before
adding any surface, line or control. Before writing any value, in uikit or in a platform, find it in
[the ladders](references/ladders.md): the tables that give each text style, color, space, size, shape,
shadow, duration and layer its one use.

| Group                                                              | Decides                                                    |
| ------------------------------------------------------------------ | ---------------------------------------------------------- |
| [Light and surfaces](#light-and-surfaces)                          | What a surface is made of, and how it rises                |
| [Lines, and what holds a control](#lines-and-what-holds-a-control) | Dividers, outlines, and the canvas or island context       |
| [Page anatomy](#page-anatomy)                                      | Sections, cards, collections and sticky bars               |
| [Actions and states](#actions-and-states)                          | The lit action, and the chosen, focused and disabled looks |
| [Fields](#fields)                                                  | Wells, their lists and long choices                        |
| [Color, type, space and shape](#color-type-space-and-shape)        | Hues, text styles, spacing, radii and control sizes        |
| [Navigation, feedback and motion](#navigation-feedback-and-motion) | The rail, message boxes, motion and effects                |

### Light and surfaces

- **Contrast through elevation.** Neutrals stay blue-gray. The canvas sits near black, and each
  raised surface steps lighter, so islands stand clear of the field without borders.
- **Light comes in veils.** One veil (`--color-veil`) is the lightest neutral, an eighth strong, over
  whatever lies behind it. A raised card is three quarters of a veil over the canvas, the pointer
  adds a whole one to what it hovers, a neutral action is one over what holds it, and a line is one
  drawn thin. A veil is translucent, so each shows on every surface, a
  lamp-lit one included. Never set an opaque gray beside it for a surface, a hover or a line: it holds
  on one backdrop only, as the opaque `--color-border-subtle` vanished on a card until uikit dropped
  it.
- **Depth from light, never bevels.** Texture and depth come from the lamp, glow, and
  transparency. A raised surface stays flat: a hairline ring gives it its shape (`--shadow-ring`,
  carried by `--shadow-{sm,md,lg}`), and the lamp gives the page its volume. A surface floating over
  others at the screen's edge, such as the consent banner, takes `--shadow-floating`, which reaches
  every side: the offset of `--shadow-lg` falls off the screen there. Never draw a bright top
  line, a dark bottom line, an embossed edge, or a gradient face: skeuomorphism reads dated.
- **Cards are frosted glass.** A raised card is a light veil with a backdrop blur
  (`--color-surface-island-glass`), so the lamp shines through it as it lights the navigation. It
  takes three quarters of a veil: a full one stood too light beside the night, and a card sits a
  step under the dialog and the rail that rise above it. A
  card inside a card keeps the parent's glass and stands out by its shadow alone (`--shadow-md`):
  an opaque inner card blocks the lamp and reads colder, and a lighter one reads too bright and
  drops muted text under 4.5:1 near the lamp.
- **One lamp per app.** A radial light in the top-start corner, scaled to the viewport, tells
  people which app they are in, as JetBrains IDEs tint their toolbar with a project color. Each app
  sets `--color-lamp` on its root element, one of the three hues and never a blend of two:
  Account's amber, Studio's magenta, the template's blue (`--color-lamp-{amber,magenta,blue}`). iOS ignores a fixed background, so
  `@a-novel-kit/uikit-tokens/canvas.css` hangs the lamp (`--gradient-lamp`) on a layer fixed to the
  viewport behind the page, never on an element that scrolls. Navigation stays in the viewport's
  corner, so the light it carries (`--fill-navigation`) lines up in every browser. Anything else
  lets the canvas's light through instead of painting its own: frosted cards, and sticky bars that
  blur what scrolls under them.
- **Glows never meet a clip.** A container that clips or scrolls leaves a lit control's glow room
  inside it. A padded card clips nothing, and a dialog's body scrolls out to the dialog's sides and
  bottom, so only the dialog's own edge cuts a glow or a focus ring.
- **Effects never hide contrast.** Axe reports color contrast as incomplete for text over any
  background image, gradient, or overlapping layer, so measure the brightest point of every lit
  surface by hand and record the ratio beside its token. Keep grain and patterns off surfaces that
  carry text.

### Lines, and what holds a control

- **Every line is a veil.** A divider (`--color-border-divider`, uikit's `Separator`) parts siblings
  on one surface, a hairline wide: rows in a card, groups in a rail. An outline
  (`--color-border-default`) draws a shape that has no fill of its own, at `--border-width-outline`:
  an outline button, a ghost card, a field outside an island. An outline is the veil its shape fills
  with under the pointer, so a ghost card's ring floods the card as it lights. Only the rim of a
  checkbox, a radio button or a switch stays opaque (`--color-control-border`), to hold around its
  own small well.
- **An outline is painted last.** Whatever draws a shape's line sits above what the shape holds. A
  fill or a clipped layer painted over a line's edge eats the line's inner antialiasing: straight
  edges keep their weight while every corner thins and reads as cropped, most on a bright line over a
  dark field at one pixel per point. A line and the fill it frames share one element, as a field's
  border does over its own background. Where a layer of its own fills the shape, such as a card's
  backdrop blur or a frost that reaches past its box, the line is an `outline`, which the browser
  paints above the element's content: a ghost card's ring, and an open list's outline over its frost
  (`outline-offset` at minus its width, over a transparent border that holds its room). Never ring
  such a shape with a spread `box-shadow`, which sits under the fill, and never let a child clipped by
  `overflow` meet its parent's border: uikit's Table carries its fill on the bordered scroller for
  that reason. Judge a corner at one pixel per point, magnified: a high-density render hides the
  loss.
- **What holds a control changes how it draws.** On the canvas nothing else sets a control apart, and
  the veil is at its dimmest there: a field takes a ring, a veil outline doubles its weight, and a
  disabled action fills with half a veil. An island (a card, a dialog, an open disclosure, a
  fieldset, a message box) carries its controls on its own tone: its fields are bare wells, its
  outlines hairlines, and its disabled actions take a field's well. `tokens.css` owns that context
  in one rule, `[data-island] > *`, which resets `--color-field-border`, `--border-width-outline`
  and `--color-action-disabled-surface`. Every uikit island carries `data-island`, and so does an
  application's own. The context starts at the island's content, so the island's own outline still
  follows what holds it, as a ghost card's ring does. A colored outline button carries itself, so it stays
  a hairline everywhere. A field keeps its line's weight through its states, so focus and errors only
  color it, and a field whose content sets its height, such as a textarea, stands 2px taller on the
  canvas than in an island. The rim of a shape that carries its own fill, as an avatar's picture,
  stays opaque with a checkbox's (`--color-control-border`): a veil dims over a dark picture. A new context-dependent look adds one declaration to that rule, never a
  selector per component.

### Page anatomy

- **A section titles its cards from outside.** A page reads as sections, each a uikit `Section`: a
  title, an optional explanation and its controls, above the content. The title sits on the surface
  that holds the section, never inside the card that follows: a page's sections title on the canvas,
  and a section inside a card titles on that card. A card holds the content. Several things of one
  kind sit as sibling cards under one title, as Account's credentials hold a card per way to log in. A
  card names itself only as an item of a collection, such as a project or one way to log in, through
  a `Section` inside it one heading level down. A dialog titles itself, since nothing outside it can.
  Never write a section's heading by hand: four Account tabs once did, and their titles sat inside
  the card on two and outside on two, over three different explanation styles.
- **A collection is a mosaic of ghost cards.** Each item of a collection, such as Studio's projects, is
  a ghost card (`Card surface="ghost"`): the canvas's own color under a raised card's veil and blur,
  ringed in the veil that fills it once lit (`--color-border-default`) since it has no surface of its own,
  so the list reads as the page until the pointer or the keyboard's focus lights one card as a raised
  card, and its ring floods it. The cards
  run down one column before the next, each as tall as it needs, never aligned across columns. A
  card stops at the long readable measure (`--layout-readable-measure-long`, 80ch) plus its padding,
  and the columns center on a screen wider than they need.
- **A sticky bar frosts and overhangs.** A page's sticky part, such as a tab bar, is clear glass over
  a backdrop blur: the lamp shines through it, and only what scrolls under it frosts. A bar at the
  page's foot holds actions over whatever scrolls by, so it darkens to the canvas under its frost, as
  the navigation does. It draws no line, as no layout part does. It overhangs its column by the
  hairline ring (`--border-width-thin`) on each side, so the ring of a card as wide as the column
  never shows past its edges, and spans a phone's whole width, where its tabs center while they fit.
  A finger or a wheel scrolls its overflow, so it shows no scrollbar.

### Actions and states

- **One lit action per view.** The primary action is the brand's pair, its ink on its tint, with a
  slight gradient that lightens toward the top and a soft underglow in its own color
  (`--gradient-action-primary`, `--shadow-action-primary`), with no transparency. Every other action
  stays flat, and a neutral one writes in secondary text at rest, so the lit one leads. A short form sits
  in a compact dialog (`Dialog size="sm"`). Every action group and button layout follows one rule,
  which `ActionGroup` draws for `FormActions` and a dialog's `actions`: on a viewport narrower than
  35rem the buttons stack full width, the primary action last; from 35rem
  (`@media (min-width: 35rem)`) they fit their content, side by side and right-aligned at the line's
  end. The viewport decides, never the container, so the actions of a narrow card on a desktop keep
  their content's width.
  Secondary journeys become quiet text buttons (`Button variant="text"`): colored text at rest, a
  soft box on hover. A recovery action sits on the label row (`Field labelActions`), a journey
  switch is one footer line. Never stack ghost buttons under a form.
- **One look says "this one, of several".** The current item of a rail, a chosen ghost button and
  the tab showing all read the same: a tinted box, blue text and a glowing indicator. uikit's `Tabs`
  once underlined its tab over a divider while Account's tab bar lit a box, two looks for one job.
- **A chosen ghost button reads as the current item.** A ghost or text button never draws a border:
  a soft box under the pointer, and once chosen (`aria-pressed`) the navigation's current item, blue
  whatever its tone, with NavList's glowing indicator under its label, which no hover draws, so a
  brand toggle under the pointer never reads as chosen. Studio's column choice shows it. Only an
  outline button keeps a bordered selection, and a disabled choice stays visible, dimmed.
- **A disabled action is an inert fill.** It draws no outline and no glow: only its fill, under muted
  text (`--color-action-disabled-surface`, `--color-action-disabled-text`). On the canvas the fill is
  half a veil, a slight gray; in an island it is a field's well, so the action sits level with the
  fields around it. A disabled checkbox, radio button or switch drops its rim the same way, and a
  disabled slider keeps its value readable in the muted text's color. A ghost or text button has no
  fill to dim, so it stays bare text. A transparent border keeps each one outlined in forced-colors
  mode.
- **What scrolls away fades away.** A dialog's body that outgrows the screen fades out over its
  end inset before the footer, so a long form never ends on a line sliced against its actions. The
  inset holds nothing at rest, so a short dialog is untouched.
- **Tracks share one tone.** A progress bar, a meter, a slider and a switch draw their empty track
  as one veil over what holds it (`--color-action-neutral`), so it shows on every surface. An opaque
  tone vanishes on the surface of its own tone: a well's on the canvas, an island's in a dialog,
  where a slider once lost its track. A browser's own meter is a light bar in its greens and reds:
  restyle it.
- **Focus without noise.** A dialog focuses itself when it opens, so no control lights its focus
  ring until the reader moves; keyboard focus stays visible everywhere else. The skip link's target,
  the page's `main`, takes focus without a ring: a click inside it and a scrolling key would
  otherwise outline the whole page.

### Fields

- **Fields are wells.** A field is a translucent darkening of the island it sits in
  (`--color-surface-sunken`), with no border: only focus and errors outline it, and a transparent
  border keeps it outlined in forced-colors mode. A field outside an island, on the canvas or a sticky
  bar, takes a ghost card's ring (`--color-field-border`), which an island drops for its fields. Fields share the button
  heights, so a borderless field keeps the same visual weight as the action beside it. Every input
  is one, under a field's label above the well, never a legend on its edge: a choice among short
  options is a `ChipGroup`, tags a `TagInput` (a space ends one, in lower-kebab-case), and a file a
  `FileInput`. A chip and a tag share a badge's size and regular weight, so a choice reads as the
  tag it becomes. A file field has no button: it names the file, a folder icon at its end opens the
  picker as the whole field does, and a cross removes the choice. A chosen image always shows above
  its field, framed or not, never inside it.
- **A field's list continues the field.** An open select or combobox list reads as its focused field,
  extended: the field's focus outline and glow (`--color-control-border-focus`,
  `--shadow-focus-ring`) over the shadow that lifts it, and the focused well's color
  (`--color-control-surface-focus`), frosted over a backdrop blur. It dims what it blurs
  (`--filter-dim`), so a bright action behind it does not clash with its options, and its veil thins
  by as much, so the list lands on its field's tone over an island and what lies behind it still
  shows through: at the field's own opacity the frost reads as a solid panel. Its option text keeps 6.1:1 even over the brand color at its peak. A select keeps the focused field's look while its list
  shows, tied to its open state rather than to focus, since a tap or a click focuses a button
  without showing it. The list opens against the field's focus ring (`--border-width-strong`),
  touching it without covering it. It fades in as it drops `--space-1` from the field, and plays it
  back on close, in `--duration-fast`; uikit's `FloatingSurface` keeps a closing surface on the page
  until its exit transition ends.
- **A long choice searches and sorts.** A chip list too long to scan, such as Studio's genres, opens
  its well with a search (`ChipGroup searchLabel`). The search filters the chips by label, whatever
  their case and accents, and never changes the choice: a hidden chip keeps its choice and its
  checkbox in the form. Under it, the chips scroll past an open select's height and gather in titled
  sections a divider apart (`ChipOption.section`), each title pinned flush under the search while its
  chips pass under it. A pinned title stays plain: it paints the well's color over a dialog's, as the
  well shows inside a dialog, since the well's own translucent color would let the chips show
  through. A wheel stops at the list's end; a finger carries on to the page. On a narrow screen the
  form keeps only the choice, as badges or a placeholder, and a tap opens the search and the chips
  over the whole screen.

### Color, type, space and shape

- **A value is looked up, never picked.** What a thing is decides its token:
  [the ladders](references/ladders.md) hold one row per role, and the token contract check fails a
  value outside its family. A value no ladder holds carries an `/* off-scale: <reason> */` comment
  directly above it, as the root's 20rem floor names WCAG 1.4.10's reflow width, and a role that
  recurs gets a row first. The Storybook role table, this skill and the code once disagreed on three
  gaps, and sibling components drifted apart: `EmptyState` and `StatusState` titled the same kind of
  block in two faces, two sizes and two grays.
- **A hue is an ink or a tint.** Its ink is the neon itself (`--color-ink-{brand,pressure,signal}`),
  for what is drawn thin: text, an icon, a line. Its tint is a deep step of its ramp, as saturated
  as that depth allows (`--color-tint-{brand,pressure,signal}`), for what is filled under its ink.
  So a tag, a chosen chip and a colored action read as the tab showing does: neon text on its own
  deep color. The neon never fills behind text: it glared there and left only black or white to
  write with, and badges once wore white on a mid fill beside buttons in black on the neon, two
  looks for one palette. A mark that is on, such as a checked box, a switch or the filled part of a
  slider, is small, so the brand's ink fills it (`--color-control-on`). The magenta sits darker than
  the blue and the amber, so its ink takes a quarter of light. Each ink keeps 4.9:1 or more on its
  tint and 5.4:1 or more on a card and in a dialog. Never dull a hue to level it with another:
  capping every family at the brand's chroma washed the whole palette out. A hue keeps its gamut's
  edge, and its tint and its ink carry its strength.
- **Three hues, blue most.** Every color is a nuance of the brand blue, the pressure magenta or the signal
  amber, in that order of frequency. Blue carries actions, links and tags; magenta is rarer, for the
  second accent, such as a project's genres (`Badge tone="accent"`), and
  for failures; amber is rarest, for highlights and text selection. An even blend of two, as Studio's
  violet lamp was, reads as a fourth hue: never use one.
- **Selection in amber.** Selected text reads amber on a translucent blue veil
  (`--color-selection-{text,surface}`), a pair no other state uses. Buttons, tabs and menus are not
  selectable.
- **Text takes a style, and its color follows.** An element sets one declaration,
  `font: var(--text-<role>)`, and never a size, a line height or a family: content reads in
  `--text-body`, interface text in `--text-ui`, a compact fact in `--text-meta`, each title level in
  its own style. Color follows the role: primary for what the reader came for, secondary for what
  names or explains it, muted for what they can skip. A description under a title is secondary,
  whatever component draws it, and a message's body reads at the interface size, never smaller. A
  field's hint and its error share one style and one edge, the label's, and a field in error shows
  both: the hint often says how to fix it.
- **Weight marks action.** A label names one control, so it stays regular. Bold is kept for actions
  and group names. Small text on a fill thins out, so a badge, a chip and a tag take the medium weight
  (`--font-weight-medium`). TeX Gyre Heros ships 400 and 700 alone: uikit-fonts maps the interface
  family's 500 to 600 to Arimo's weight axis, whose widths and shapes match, under Heros's own ascent
  and descent. A synthetic stroke smudges at one pixel per point: never fake a weight with one.
- **Corners stay concentric.** A shape inside another takes the outer radius less the space
  between them, so both corners share their centers. Each `--radius-*` step doubles the one below
  (3, 6, 12, 24px). A nested card sits half its parent's radius from the edge and lands on the next
  step down at any depth, while the card's other content sits a spacing step further in (18px in a
  top card, 9px nested). A list or a menu adds its `--space-1` padding to its items' radius,
  which is off the scale.
- **Corners follow the shape.** Labeled buttons, fields, message boxes and navigation links take
  `--radius-lg`. An icon-only button or a compact navigation item keeps `--radius-md`: 12px on a
  28px square reads round. A button inside a field takes the field's radius less its inset.
- **One space per relationship.** Space inside and between elements uses eight steps, 3, 5, 6, 9, 12,
  18, 24 and 36px (`--space-{1,1-5,2,3,4,6,8,12}`), and each sets one relationship: parts of one
  thing sit 6px apart, things that work as one unit 9px, siblings 12px, groups 24px. So a label sits
  6px above its field, buttons in a group 9px apart, fields 12px apart, a section's title 12px above
  its content, sections 24px apart and form actions 24px below the last field. The in-between
  `--space-1-5` only separates chips and tags, and a negative margin only lets a hover box overhang
  its row or a nested card reach its inset. Sizes keep a 4px grid on `--size-{N}`: control heights,
  icons, avatars, badges, marks, and the reach of shadows and glows. Widths come from the
  `--layout-*` tokens, a floating surface's included. A size never takes a `--space-*` token, and a
  space never takes a size.
- **Compact controls.** Controls sit on a 4px grid at 28, 32, and 40px, and a coarse pointer adds
  `--size-1` to each. Medium is the default and takes no prop: a field, and the actions of a form, a
  dialog or a toolbar. Small is an action inside a row or beside a title, a tab or an option. Large
  is a page's only action, on a page built around it. Small and medium buttons set their label in the small text size. Empty space
  inside a button reads as bulk; keep its padding tight and its target large through height alone.

### Navigation, feedback and motion

- **Navigation is a vertical rail.** Every app uses the same shell: a collapsible rail on wide
  screens, a header one control tall and a fullscreen drawer on narrow ones, sharing one navigation
  surface. The header is fixed to the screen, so iOS stretches the page under it past the top while
  the header and its light stay in place, and the drawer, as every modal dialog, holds the page
  behind it still. The links of one group touch, as in most productivity apps, and run in order of
  importance ([plan-ui-design](../plan-ui-design/SKILL.md#2-map-the-task-before-the-screen)). A
  `Separator`, inset like the links, sets groups apart, and the account and the logout sit in
  separate groups so a click never lands on the wrong one. The top and bottom groups stay 24px
  apart (`--space-8`). Every link opens in the same tab, between Agora apps included: a reader who wants another
  tab asks their browser. A link to the page showing outside a NavList, such as the account link,
  carries `aria-current="page"` and reads as the current item (`Button` styles it); the account
  picture then shows in full color.
- **A message's mark sits on its first line.** The icon of an `Alert`, a toast or a banner centers
  on the title's line, however many lines follow, never on the box's middle, where it drifts away
  from the title it belongs to. `FeedbackIcon` owns it: the mark starts at the top and the text beside
  it starts a step lower, so a single line keeps the mark's height and every message has the same air
  above its first line and below its last.
- **Failures sit in a feedback box.** A message that reports a failure, a refusal, or something the
  reader must act on renders in a toned feedback component, never as plain text: an `Alert` inside a
  section, an `InlineMessage` beside a control, a `StatusState` when it replaces the page. The tone,
  icon, and accent bar carry its meaning at a glance; plain secondary text stays for neutral
  descriptions. A stored state rather than fresh feedback, such as a pending change, takes
  `role="note"` so it is not announced again.
- **Motion eases, never performs.** Dialogs fade in and rise a little, and play it back on close;
  the rail's width eases between its two states. Each takes `--motion-surface`, a control changing
  state takes `--motion-state`, and a motion token carries its duration and easing together.
  Reduced motion sets every duration to zero, and a browser without the transition simply switches.

## Refinement passes

Preserve that direction, along with compact metadata and vivid semantic accents. Tune the owning
semantic role and compare before/after renders across its consumers.

- Judge color with area and backdrop. A badge, a chosen chip and a colored action fill with their
  hue's tint under its ink (`--color-tint-*`, `--color-ink-*`), since a full accent glares behind
  its words. Darken
  along the ramp, never by lowering lightness at the same chroma: the color leaves the gamut and
  clips to another hue, as the genre magenta turned plain red. A large status box
  needs a quieter tint with a clear icon and accent. Improve vividness through chroma and foreground
  contrast before adding white or increasing opacity everywhere. Keep control variants in their
  semantic color family through hover and selection.
- Distinguish external margins from internal padding. Narrow screens benefit from small outer
  gutters while panels, dialogs, and buttons still need breathing room inside. Separate form actions
  from inputs, wrap long secondary labels, and preserve source order. Dense metadata can be smaller
  than controls; compact labels do not justify shrinking touch targets.
- Share anatomy where the responsibility matches: heading and trailing controls, optional supporting
  text, content, and actions. Leave omitted regions empty of spacing. Keep native behavior and
  controller ownership separate from stateless composition; ordinary body content needs no wrapper
  unless the wrapper owns a real layout invariant.
- Treat glow as light behind the emitting shape. Compare layered soft shadows with a restrained
  shape-following halo; avoid a sharp neon outline or an oversized rounded cushion. Keep ordinary
  surfaces subdued and focus indicators unambiguous.

Finish by removing compensating overrides and duplicated machinery. A coherent pass may leave most
components unchanged; change only what the rendered comparison or code ownership shows needs work.

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
- Default manager, docs, and canvas to the A-Novel dark theme. Keep a contrasting background option
  for component verification.
- Align Storybook manager and docs typography and color roles with A-Novel while retaining Storybook's
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
- Reproduce a consumer regression against the released package version, then verify the fix through
  the packed artifact or a released patch in a representative consumer. A workspace link can hide
  export, dependency, CSS-order, and surface-composition defects that appear only at the public
  package boundary.
- Build Storybook as a verification artifact, never as an automatically published package or public
  deployment unless the user explicitly requests and scopes that publication.
- When static Storybook publication is requested, use the shared CI workflow, publish only the
  generated static artifact, verify its base path and asset loading, and exclude secrets and private
  environment data.

## Review checklist

- Component CSS contains no raw product colors, spacing, radii, borders, type, or motion values, and
  the token contract check passes with every `off-scale:` exception justified.
- Text takes a `--text-*` style with the color of its role; no component sets a font size, a line
  height or a family. Each space, size, radius, shadow and duration is the one its ladder gives.
- A new or removed token changes its ladder row, its Storybook table and the comment beside its
  definition in the same change.
- Public components remain small semantic bricks; optional presentation composes without boolean or
  position-prop matrices.
- Bases and ratios are fewer than generated public choices and have documented meaning; each step
  multiplies its base by a plain number, never a multiplier token.
- Color documentation states the harmony, tone/chroma formula, gamut mapping, fallback, semantic
  pairings, and effect layer without hiding family-specific exceptions.
- Dark neutral and accent ramps preserve their intended dynamic range in rendered primitive,
  semantic, control, and navigation contexts; semantic foreground/background pairs pass their gate.
- Translucent interaction layers remain distinct across canvas, island, glass, popover, and dialog
  contexts without introducing a second hard-coded color system.
- Borders are reserved for meaningful affordance or structure. Invisible groups and translucent
  islands remain distinguishable through composition, surface, and elevation.
- Every line is the divider or the outline token, never an opaque neutral, and a surface that holds
  controls carries `data-island`. A section's title comes from `Section`, outside its cards.
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
