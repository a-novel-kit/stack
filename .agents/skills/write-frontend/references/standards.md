# Frontend standards index

Use this index to resolve design and implementation questions. Prefer the current version of the
source over a remembered rule, and read the relevant section before using an unfamiliar API. Do not
copy example implementations blindly; adapt their contract to the installed toolchain and target
browsers.

## Normative web platform

- [HTML Living Standard](https://html.spec.whatwg.org/) — elements, forms, semantics, parsing, and
  browser behavior.
- [DOM Standard](https://dom.spec.whatwg.org/) — nodes, events, and mutation contracts.
- [Fetch Standard](https://fetch.spec.whatwg.org/) and [URL Standard](https://url.spec.whatwg.org/) —
  request, response, cancellation, and URL behavior.
- [ECMAScript specification](https://tc39.es/ecma262/) — JavaScript language semantics.
- [CSS specifications](https://www.w3.org/Style/CSS/specs.en.html) — cascade, layout, media queries,
  custom properties, color, and other CSS modules.

## Accessibility

- [WCAG 2.2](https://www.w3.org/TR/WCAG22/) — normative AA acceptance baseline.
- [ARIA in HTML](https://www.w3.org/TR/html-aria/) — allowed roles and properties on HTML elements.
- [WAI-ARIA Authoring Practices](https://www.w3.org/WAI/ARIA/apg/) — interaction patterns for widgets
  HTML does not provide. Treat examples as patterns, then verify browser and assistive-technology
  support as APG requires.
- [Svelte accessibility compiler warnings](https://svelte.dev/docs/svelte/compiler-warnings) —
  framework diagnostics; suppress only verified false positives.

## Language, framework, and compatibility

- [TypeScript `strict`](https://www.typescriptlang.org/tsconfig/strict) and the
  [TSConfig reference](https://www.typescriptlang.org/tsconfig/) — compiler guarantees and stricter
  options.
- [Svelte documentation](https://svelte.dev/docs/svelte/overview) and
  [SvelteKit documentation](https://svelte.dev/docs/kit/introduction) — use documentation matching
  the installed major version.
- [MDN Baseline](https://developer.mozilla.org/en-US/docs/Glossary/Baseline/Compatibility) — browser
  interoperability status. The repository browser policy still decides support.

## Testing and component documentation

- [Testing Library guiding principles](https://testing-library.com/docs/guiding-principles/) and
  [query priority](https://testing-library.com/docs/queries/about/) — test the DOM as users perceive
  and operate it.
- [Vitest guide](https://vitest.dev/guide/) — runner, isolation, browser mode, and mocking behavior.
- [Storybook accessibility testing](https://storybook.js.org/docs/writing-tests/accessibility-testing)
  and [UI testing](https://storybook.js.org/docs/writing-tests) — rendered
  state, interaction, and automated accessibility checks.

## Security and dependency health

- [OWASP Cross Site Scripting Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross_Site_Scripting_Prevention_Cheat_Sheet.html)
  and [Content Security Policy Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Content_Security_Policy_Cheat_Sheet.html)
  — output handling and browser policy boundaries.
- [OpenSSF Scorecard](https://scorecard.dev/) — one input when assessing a new FOSS dependency; pair
  it with maintainer, release, test, vulnerability, license, and ownership review.

## Design-system references

- [Design Tokens Community Group format](https://www.designtokens.org/tr/drafts/format/) — shared
  vocabulary and interoperability direction; Agora may use CSS-native tokens instead of its JSON
  interchange format.
- [GitHub Primer Primitives](https://github.com/primer/primitives) and
  [IBM Carbon design tokens](https://carbondesignsystem.com/elements/themes/overview/) — maintained
  FOSS examples of primitive-to-semantic token architecture. Use as comparative evidence, not as an
  Agora dependency or visual source.

## Decision record

When a decision is not dictated by repository policy or a normative standard, record in the
planning issue or PR which official/FOSS source informed it, the target-browser constraint, and the
tradeoff. Do not fossilize temporary framework workarounds as universal rules.
