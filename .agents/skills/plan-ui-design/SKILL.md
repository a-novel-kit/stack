---
name: plan-ui-design
description: >
  Product UI/UX and design-system planning gate for browser interfaces. Use before creating or
  materially reshaping user flows, pages, navigation, forms, data views, interaction patterns,
  component families, visual foundations, design tokens, or product-facing interface content; also
  use when comparing visual directions or reviewing whether proposed UI is coherent, distinctive,
  accessible, reusable, and ready for implementation. Produces a rendered, testable design contract
  before `write-frontend` or `write-design-system` implementation.
---

# Plan UI Design

Work one layer above frontend code. Turn a product need into an interaction and visual contract that
can be challenged before implementation. Keep the workflow proportional: amend an existing contract
for a local change; run every stage for a new flow, pattern, component family, or foundation.

Load [references/visual-language.md](references/visual-language.md) when extracting a reusable
visual direction from screenshots or other references. Load [references/sources.md](references/sources.md)
when selecting an unfamiliar interaction pattern, accessibility behavior, token model, color method,
or external design-system precedent. Prefer current primary sources over remembered advice.

## Position in the workflow

- Let `plan-feature` own technical architecture, cross-repository scope, and the planning issue.
- Own user intent, task flow, information hierarchy, interaction behavior, content, visual language,
  component need, and validation evidence here.
- Hand the approved contract to `write-frontend`; add `write-design-system` for foundations or reusable
  components, `write-svelte` for Svelte, and `write-frontend-tests` for stories and tests.
- Do not let a mockup silently decide product behavior, and do not let implementation convenience
  silently decide the user experience.

Use this authority order when constraints conflict:

1. The explicit brief and established product contract.
2. Observed user needs, realistic content, and product evidence.
3. Existing design-system components and patterns.
4. Normative accessibility and web standards.
5. Maintained FOSS guidance from established organizations.
6. Aesthetic preference.

## 1. Frame the job and evidence

Write down:

- the primary audience, their situation, and the single job this interface must help complete;
- the observable success outcome and the cost of failure;
- device, input, environment, privacy, latency, localization, and content constraints;
- facts backed by research or existing behavior, assumptions still unverified, and the smallest way
  to test each risky assumption.

Use real domain content as early as possible. Never present invented personas, metrics, or preferences
as research. If the brief leaves one decision open, make and label a reversible assumption instead of
blocking; ask when the choice would materially change the product.

### Study visual references as evidence

When screenshots, recordings, or admired products inform the direction:

- group frames into sequences and distinguish a demonstrated rule from a finished visual example;
- record role, hierarchy, geometry, contrast, boundary, state, and motion observations before naming
  a style or sampling a value;
- treat capture color as uncalibrated, especially for HDR, wide-gamut, compressed, photographed, or
  color-managed sources. Preserve relationships, then re-measure in the target browser;
- translate each useful observation into a product rule and mark whether to retain, adapt, or reject
  it. Do not copy assets, brand signatures, or arbitrary measurements;
- verify accessibility and platform behavior against primary standards. A designer reference is
  art-direction evidence, not normative authority.

## 2. Map the task before the screen

Describe the shortest successful sequence as entry → comprehension → decision → action → feedback →
recovery or exit. Include only branches that change what the person must understand or do.

For every meaningful step, account for:

- first use, return use, loading, empty, partial, stale, success, error, offline, permission-denied,
  and unavailable states as applicable;
- cancellation, undo, retry, and recovery for destructive or expensive actions;
- keyboard and focus movement, touch target behavior, assistive-technology announcements, and URL or
  history behavior;
- long, translated, user-generated, sensitive, and malformed content.

Keep vocabulary stable through the flow. Name controls by the outcome people recognize, keep the same
verb in confirmation and feedback, and make errors identify both the problem and the next action.

## 3. Pass the reuse and contribution gate

Inventory the existing product, UI kit, native platform, and established interaction patterns before
proposing a new component. Prefer composition or a documented variant when the existing contract fits.

Add a pattern only when it is:

- **useful**: evidence shows a recurring user or product need;
- **unique**: an existing component or composition cannot meet that need cleanly;
- **usable**: realistic research or evaluation shows people can complete the task;
- **consistent**: it reuses system behavior, language, and foundations where those remain valid;
- **versatile**: its contract survives the credible contexts that justify making it reusable.

Keep a product-specific composition local until repeated evidence earns a shared abstraction.
Treat a demo or reference application as evidence for needs and behavior, not as a component map to
copy. A shared system owns generic foundations, controls, and geometry primitives; the application
owns route orchestration, product vocabulary, workflow composition, navigation landmarks, and its
final shell. Promote only the part that remains useful when those application details disappear.

## 4. Establish a visual thesis

Write one sentence connecting the product's subject, audience, and emotional register to the visual
direction. Define one memorable signature and two or three anti-goals. Derive imagery, typography,
structure, and motion from the subject's actual world rather than from a generic application template.

- Spend visual boldness in one place; keep the surrounding field disciplined. Define a chroma
  budget: which role may dominate, which role may support, and which regions stay neutral.
- Define a boundary hierarchy before drawing containers: spacing and alignment, tonal or opacity
  shift, elevation, then a visible border only when the boundary itself carries meaning.
- Make hierarchy encode meaning. Do not add numbering, charts, badges, cards, glass, gradients, pills,
  or motion unless each communicates something true.
- Correct for optical weight when geometry appears misaligned, especially for asymmetric icons and
  nested marks; mathematical centering is only the starting point.
- Treat words as interface material. Use plain, specific, active language in the product's voice.
- Match complexity to the thesis: maximal work needs orchestration; minimal work needs exceptional
  spacing, type, and state precision.
- Critique the direction against recent work. Replace any choice that would survive unchanged in an
  unrelated product brief.

When exploration is requested, render two or three intentionally different directions. Change one
major axis at a time, label the tradeoff, keep experiments easy to remove, and do not commit rejected
directions as public tokens.

## 5. Plan foundations as relationships

Define compact bases and preset multipliers before enumerating tokens. Separate primitive values from
semantic roles and component contracts. A global adjustment must not require editing scattered magic
values.

For color:

- Define agency, emphasis, feedback, surface, text, border, focus, data, and decorative roles before
  assigning hues.
- Choose a deliberate harmony with as few hue families as the product needs. Keep neutral surfaces as
  the field and reserve chromatic moments according to hierarchy.
- Design hue topology, tone/chroma scaling, output gamut and fallback, semantic pair contrast, and
  graphic effects as separate contracts. A change to one layer must not silently redefine the others.
- Use a perceptual color space for relationships, but choose absolute or gamut-relative chroma
  deliberately. Mathematical symmetry and equal parameters do not guarantee equal rendered energy.
- Map out-of-gamut colors with a maintained constant-hue perceptual algorithm. Never use raw RGB
  channel clipping for a palette contract; it can materially shift both hue and chroma.
- Treat Display P3 or another wide gamut as progressive enhancement. Define and inspect the sRGB
  fallback first-class, and do not let display capability change a color's semantic meaning.
- Judge anchors, ramps, semantic mixtures, gradients, native controls, and text in the rendered target
  theme. The same numeric lightness or saturation can read differently by hue and context.
- Build neon from high chroma, controlled lightness, a dark neutral field, and localized glow. Do not
  create energy by mixing every step toward white, maximizing every surface, or adding unrelated hues.
- For a game-interface direction, reserve each hot color for a stable association and let neutral,
  opaque surfaces carry most of the screen. Express the world through a repeatable shape language such
  as traces, brackets, grids, or telemetry—not through indiscriminate decoration.
- Keep glow, bloom, scan lines, and chromatic aberration decorative. They cannot carry text contrast,
  focus, state, or meaning; default to static effects and require a reduced-motion-safe alternative
  before adding any pulse, sweep, blink, or flash.
- Meet contrast and non-color signaling requirements in actual foreground/background pairs. A token
  index, harmony label, or color-vision simulation is not a substitute for contrast measurement.

For type, space, shape, elevation, and motion, define role, base, scale, limits, and exceptions.
Use a compact ratio ladder: a small density ratio for workstation details, a medium hierarchy ratio,
and the golden ratio only for major composition when it improves the content hierarchy. Prefer
optical correction over false arithmetic purity, but keep each correction explicit and rare.

For borderless compositions, specify which groups remain invisible and which become semi-opaque
islands. Require separation to survive without a one-pixel rectangle around every region. For
gradients between distant hues, define the interpolation space and intentional color route rather
than accepting a muddy default midpoint. Size glow from the emitting object and local context, keep
it localized, and reserve it for selected, focused, or intentionally luminous graphic states.

## 6. Write the component or pattern contract

Specify before coding:

- purpose, non-goals, anatomy, composition, and content ownership;
- framework-native composition points for text, markup, icons, and nested components. Do not reduce
  safe, meaningful content to a string-only label API; use a dedicated accessible contract for
  icon-only controls;
- variants and sizes justified by distinct needs, not visual possibility;
- resting, hover, active, focus-visible, disabled, loading, selected, expanded, invalid, empty, error,
  success, overflow, and high-contrast states as applicable;
- state precedence. Persistent states such as selected, checked, expanded, invalid, and disabled must
  remain legible when transient hover or active input is also present; a hover treatment must never
  visually undo the persistent state;
- native semantic element, accessible name, keyboard model, focus entry/exit, announcements, and
  pointer or touch behavior;
- responsive and container behavior, zoom/reflow, long content, localization, RTL, reduced motion,
  forced colors, and print where relevant;
- safe defaults, validation timing, destructive-action protection, and recovery behavior;
- semantic tokens and public customization points, without implementation selector leakage.

Use native HTML behavior first. For a composite widget, start from the closest WAI-ARIA APG pattern,
then document any deviation and verify it with keyboard and assistive technology.

## 7. Render and challenge the contract

Treat Storybook as the design review surface, not a gallery added after coding.

1. Render foundations and every meaningful component state with realistic content. For component
   families, use aligned variant, size, and state matrices so differences can be read at a glance;
   keep the surrounding documentation visually inert so its own hover or selection styling cannot
   contaminate the example.
2. Keep public docs stable and concise: one-sentence purpose, rendered examples, usage, states,
   accessibility, and API as applicable. Describe what exists and how to use it; omit decision
   history, rejected alternatives, rollout status, and rationale that is not needed for safe use.
3. Start Storybook with `BROWSER=none` and `--no-open`; keep it live for review.
4. Inspect the exact route in the integrated browser at narrow and wide containers. Review both the
   Storybook manager/docs chrome and the preview canvas. Exercise transient and combined states such
   as open menus, focused dialogs, selected-plus-hovered controls, and composed icon-and-text content
   instead of reviewing only resting examples. Compare surface-sensitive states on the canvas,
   opaque and translucent islands, popovers, and dialogs when those contexts are supported.
5. Exercise keyboard-only use, focus order, zoom/reflow, reduced motion, forced colors, long content,
   and the default dark theme in proportion to the change.
6. Measure rendered contrast and run automated accessibility checks, then perform the manual behavior
   checks automation cannot cover.
7. Include the freshly verified inline Storybook link in every UI status and final handoff.

Do not approve visual work from source, generated values, or a screenshot alone.

## Design contract output

Capture the result in the planning issue, Storybook docs, or the smallest durable artifact the task
already uses:

```text
Outcome and audience
Evidence and assumptions
Task flow and recovery
Content and vocabulary
Visual thesis, signature, and anti-goals
Foundation relationships and optical corrections
Component/pattern contract and state matrix
Accessibility and responsive behavior
Rendered validation plan and evidence
Open risks, owner, and next research step
```

Keep alternatives only when a real unresolved decision remains. State the recommended direction and
why it best serves the user job.

## Completion gate

- The primary task and recovery path are clear without styling.
- New shared UI passed the useful/unique/usable/consistent/versatile gate.
- Shared abstractions stop at generic behavior and geometry; application shells and workflows remain
  application-owned.
- The visual thesis is specific to this product and has explicit anti-goals.
- Foundations derive from compact relationships; optical exceptions are documented.
- Every meaningful state, input mode, content stress, and accessibility behavior has an owner.
- Persistent component states retain their meaning under hover, active, and focus interactions.
- Reusable content APIs support natural framework composition without weakening semantics.
- Storybook renders the contract, remains live, and is linked in the handoff.
- Evidence is separated from assumption, and the next research need is explicit.
