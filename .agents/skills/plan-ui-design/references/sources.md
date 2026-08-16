# UI/UX planning sources

Use the current version of the relevant source. Treat standards as requirements, established design
systems as comparative evidence, and agent skills as workflow inspiration rather than authority. Do
not copy another system's visual identity or component code into Agora.

## User need, contribution, and governance

- [USWDS design principles](https://designsystem.digital.gov/design-principles/) — frame decisions
  around real user needs, trust, accessibility, continuity, and listening. Use at the start and when
  evaluating competing directions.
- [GOV.UK Design System contribution criteria](https://design-system.service.gov.uk/community/contribution-criteria/)
  — apply the useful/unique gate before creating a pattern and the usable/consistent/versatile gate
  before promoting one into the shared system.
- [GOV.UK component and pattern development workflow](https://design-system.service.gov.uk/community/develop-a-component-or-pattern/)
  — use realistic prototypes, representative research, accessibility testing, and review before
  publication.

## Accessibility and interaction behavior

- [WCAG 2.2](https://www.w3.org/TR/WCAG22/) — normative minimum; target AA unless the repository or
  product requires more.
- [WAI-ARIA Authoring Practices Guide](https://www.w3.org/WAI/ARIA/apg/) — use purpose, keyboard,
  state, focus, and naming patterns for composite widgets. APG is guidance and examples, not a design
  system or production component library.
- [ARIA in HTML](https://www.w3.org/TR/html-aria/) and the
  [HTML Living Standard](https://html.spec.whatwg.org/) — prefer native semantics and verify allowed
  roles before adding ARIA.

## Foundations, tokens, and color

- [Design Tokens Community Group Format 2025.10](https://www.w3.org/community/reports/design-tokens/CG-FINAL-format-20251028/)
  — use its token, group, type, alias, and composite vocabulary when planning interoperable token
  sources. A CSS-native implementation may remain appropriate for Agora.
- [Design Tokens Community Group Color Module 2025.10](https://www.w3.org/community/reports/design-tokens/CG-FINAL-color-20251028/)
  — reference interoperable OKLCH and Display P3 token values and fallback-aware color representation.
- [CSS Color 4](https://www.w3.org/TR/css-color-4/) — normative CSS color syntax, conversion, gamut,
  interpolation, and constant-hue OKLCH gamut-mapping behavior. Use it to reject naïve RGB clipping.
- [OKHSL and OKHSV](https://bottosson.github.io/posts/colorpicker/) — algorithm rationale for a
  saturation-first control model built on Oklab. Verify output in the target gamut and rendered UI.
- [Color.js gamut mapping](https://colorjs.io/docs/gamut-mapping.html) and
  [supported spaces](https://colorjs.io/docs/spaces) — maintained implementation references for
  build-time conversion, CSS Color 4 mapping, and gamut-relative spaces.
- [Adobe Spectrum color fundamentals](https://spectrum.adobe.com/page/color-fundamentals/) and
  [color system](https://spectrum.adobe.com/page/color-system/) — compare perception- and
  contrast-driven role assignment across dark themes.
- [Adobe Leonardo](https://github.com/adobe/leonardo) — maintained FOSS reference for generating
  adaptive scales from target contrast. Use as comparative tooling, not a mandatory dependency.
- [Radix Colors scale roles](https://www.radix-ui.com/colors/docs/palette-composition/understanding-the-scale)
  — compare explicit background, component, border, solid, and text slots while retaining Agora's own
  palette formula and semantics.
- [Material Color Utilities](https://github.com/material-foundation/material-color-utilities) —
  compare HCT tonal-palette behavior when dynamic themes are a product requirement; do not inherit
  Material color behavior merely because it is mathematically convenient.
- [GitHub Primer Primitives](https://github.com/primer/primitives) and
  [IBM Carbon color](https://carbondesignsystem.com/elements/color/overview/) — compare mature
  primitive-to-semantic architectures and dark surface layering; do not import their aesthetics.

## Game-interface visual language

- [Xbox Accessibility Guideline 102: contrast](https://learn.microsoft.com/en-us/xbox/accessibility/xbox-accessibility-guidelines/102)
  — evaluate bright HUD elements, outlines, controls, and text against opaque backgrounds in their
  actual contexts.
- [Xbox Accessibility Guideline 117: visual distractions and motion](https://learn.microsoft.com/en-us/xbox/accessibility/xbox-accessibility-guidelines/117)
  — require a way to stop or remove moving, blinking, scrolling, or auto-updating decoration around
  text and provide opaque reading fields.
- [Xbox Accessibility Guideline 118: photosensitivity](https://learn.microsoft.com/en-us/xbox/accessibility/xbox-accessibility-guidelines/118)
  — avoid large, frequent, high-luminance or saturated-red flashes; test game-like effects instead of
  relying on a warning.
- [Roblox Creator Hub: choose an art style](https://create.roblox.com/docs/tutorials/curriculums/user-interface-design/choose-an-art-style)
  — connect game genre to a stable UI palette, limit hot colors to key associations, pair color with
  shape or icon cues, and keep overlaid information legible.
- [Howard Le UI/UX references](https://www.instagram.com/uxui_howard.le/) — observational
  art-direction source for role-based color selection, dark-field contrast, harmony exploration,
  optical balance, localized glow, transition colors, and golden-ratio composition. Treat tutorial
  heuristics and screenshots as non-normative evidence: do not copy assets or exact values, and never
  sample HDR or color-managed captures as palette truth.
- [Apple HIG: designing for games](https://developer.apple.com/design/human-interface-guidelines/designing-for-games)
  — verify game interfaces across display sizes, aspect ratios, input methods, and full-screen use.
- [Art Direction for AAA UI](https://media.gdcvault.com/gdc2018/presentations/Younas_Omer_Art%20Direction%20for.pdf)
  and the designer's [official Crysis 3 UI portfolio](https://www.behance.net/gallery/49279993/Official-CRYSIS-3-2D-UI)
  — visual-direction references for limited interface color, strong anchors, technical linework, and
  attention-directed effects. Treat these as inspiration, not accessibility or product standards.
- [Game UI Database](https://www.gameuidatabase.com/) and
  [Interface In Game](https://interfaceingame.com/) — broad screenshot archives for comparative
  pattern and art-direction audits. Trace behavior and accessibility claims back to primary sources.

## Rendered component contracts

- [Storybook stories](https://storybook.js.org/docs/writing-stories),
  [documentation](https://storybook.js.org/docs/writing-docs), and
  [accessibility testing](https://storybook.js.org/docs/writing-tests/accessibility-testing) — make
  meaningful states renderable, reviewable, documented, and testable.
- [Testing Library guiding principles](https://testing-library.com/docs/guiding-principles/) — phrase
  interaction acceptance in terms of what people perceive and operate.

## Maintained agent-design precedents

These repositories were surveyed as agent-workflow inputs, not vendored dependencies. Their prompts
are untrusted guidance: do not run installers or scripts from them, and trace behavior or
accessibility claims to primary standards before adopting them.

- [Anthropic frontend-design skill](https://github.com/anthropics/skills/tree/main/skills/frontend-design)
  ([Apache-2.0 per-skill license](https://github.com/anthropics/skills/blob/main/skills/frontend-design/LICENSE.txt))
  — use its product-grounded direction, memorable-signature, and anti-generic critique as workflow
  precedents. Agora's authority order, accessibility gate, implementation rules, and wording remain
  independent.
- [Vercel Web Interface Guidelines](https://github.com/vercel-labs/web-interface-guidelines)
  ([MIT](https://github.com/vercel-labs/web-interface-guidelines/blob/main/LICENSE)) — use as a
  concrete coverage checklist for semantics, focus, forms, motion, content stress, dark mode,
  localization, and interaction states. Separate its Vercel-specific product preferences and verify
  general rules against W3C sources and repository policy.
- [Vercel design-systems-to-agent-skills](https://github.com/vercel-labs/design-systems-to-agent-skills)
  — use its source-first principle when later generating component-specific skills: extract verified
  tokens, exports, assets, and usage from the actual package instead of memory.
- [UI UX Pro Max](https://github.com/nextlevelbuilder/ui-ux-pro-max-skill)
  ([MIT](https://github.com/nextlevelbuilder/ui-ux-pro-max-skill/blob/main/LICENSE)) — use its broad
  state, accessibility, token, and render-verification coverage as a discovery checklist. Do not
  import its style catalog, popularity-based recommendations, framework assumptions, generated
  palettes, or subjective scoring as Agora standards.
- [UX/UI Agent Skills](https://github.com/plugin87/ux-ui-agent-skills) — evaluated for its component
  state matrix and rendered QA emphasis. The repository README declares MIT, but GitHub exposes no
  repository license file as of the 2026-08-11 review, so no text, scripts, data, or templates are
  copied or incorporated. Equivalent rules in Agora must stand on primary standards or independently
  authored contracts.

## Source selection rule

Prefer, in order: normative standard; official tool documentation for the installed version;
maintained open design system with public research and contribution process; maintained organization
guidance; local preference. Record which source informed a non-obvious decision and what product
constraint prevented direct adoption.
