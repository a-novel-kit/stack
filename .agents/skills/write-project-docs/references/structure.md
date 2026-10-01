# Fleet document structure

Read this reference when routed here by [write-project-docs](../SKILL.md). Its rules apply to the selected work.

Contents:

- [Fleet standard (current)](#fleet-standard-current)
- [Header — identical shape across all repos](#header--identical-shape-across-all-repos)
- [Section order — ONE order, every repo](#section-order--one-order-every-repo)
- [Contributing section + links (fix the fleet-wide 404s)](#contributing-section--links-fix-the-fleet-wide-404s)

## Fleet standard (current)

This fleet standard is **authoritative and supersedes older guidance in the
[templates](templates.md) and [editorial principles](editorial.md)** where they conflict.
It applies to EVERY repo in the `a-novel` and `a-novel-kit` orgs — backend services, the Go
library (`golib`), the JS/TS packages (`nodelib`), the reusable Actions repo (`workflows`),
and the `stack` CLI. Reference implementations: the
[`service-json-keys`](https://github.com/a-novel/service-json-keys) README (service) and the
[`golib`](https://github.com/a-novel-kit/golib) README (library).

### Header — identical shape across all repos

```
# <Title>

<one concise line describing what this is>      ← ALWAYS present, directly under the title

[![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/agorastoryverse)](https://twitter.com/agorastoryverse)
[![Discord](https://img.shields.io/discord/1315240114691248138?logo=discord)](https://discord.gg/rp4Qr8cA)

<hr />

<tech badges — vary by repo type, see table>

<codecov sunburst image — ONLY if the repo reports coverage>
```

The one-line description under the title is **mandatory** (older service READMEs omitted it and
put badges where the description should be — add the line). Social badges, then a literal
`<hr />`, then the tech-badge block. The codecov badge + sunburst image appear ONLY for repos
whose CI uploads coverage.

| Repo type                | Tech badges (in order)                                                       | Codecov? |
| ------------------------ | ---------------------------------------------------------------------------- | -------- |
| Go service (`service-*`) | go-mod version · file count · code size · CI status                          | yes      |
| Go library (`golib`)     | go-mod version · file count · code size · CI status                          | no       |
| JS package (`nodelib`)   | file count · code size · CI status                                           | yes      |
| Actions (`workflows`)    | file count · code size · CI status                                           | no       |
| CLI (`stack`)            | go-mod version (`?filename=cli/go.mod`) · file count · code size · CI status | no       |

"Codecov if applicable" = the repo's CI calls the `generic-actions/codecov` action. Verify
before adding the badge: `grep -rl generic-actions/codecov <repo>/.github/workflows`. Today
services + `nodelib` report coverage; `golib`, `workflows`, `stack` do not.

### Section order — ONE order, every repo

Every README uses the same five slots in the same order. Only the _content_ of each slot
changes with repo type; **the order never does.** Frequently-referenced material comes first,
rarely-needed material last, so readers scroll as little as possible.

| #   | Slot             | Always contains                                                                                                                                          |
| --- | ---------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | **Header**       | Title, one-line description, badges (+ codecov sunburst if the repo reports coverage).                                                                   |
| 2   | **Role**         | What it is / does: the noun it owns, who it serves, the surface. 1–3 short paragraphs of prose — never an inline capability list (that lives in slot 4). |
| 3   | **Use it**       | The primary how-to-use, leading with the _expected_ path (see hard rules).                                                                               |
| 4   | **Reference**    | Detailed material — comes _after_ the slot-3 example, never before it.                                                                                   |
| 5   | **Contributing** | The onboarding-guide link + `./CONTRIBUTING.md`, nothing else — contribution rules live in CONTRIBUTING.md. Always last.                                 |

Use these exact slot headings per repo type:

| Repo type       | Slot 2 (role)     | Slot 3 (use it)                           | Slot 4 (reference)                                                   |
| --------------- | ----------------- | ----------------------------------------- | -------------------------------------------------------------------- |
| Backend service | `## What it does` | `## Deploying` (+ `### Configuration`)    | `## Using the client packages`, then `## Running locally`            |
| Go / JS library | `## What this is` | `## Installation` (`go get` / `pnpm add`) | `## Sub-packages` (Go) / `## Packages` (JS)                          |
| Actions repo    | `## What this is` | `## Using an action`                      | `## Action catalog`                                                  |
| CLI (stack)     | `## What this is` | `## Install`                              | operational sections (`## The UI`, `## Non-interactive commands`, …) |

**Hard rules — no exceptions:**

1. **Role (slot 2) ALWAYS precedes use / install / deploy (slot 3).** The header's one-line
   description does NOT replace it — slot 2 is its own `##` section. A `go get` / `pnpm add`
   one-liner sitting before the first `##` heading is a violation; put it inside slot 3.
2. **Slot 3 leads with the EXPECTED / production path.** For a service that is the production
   split-image composition (one canonical compose block + an image-role table) plus the OpenTofu
   forward-note below. The standalone single-command dev compose is relegated to `## Running
locally`, the LAST section before Contributing. `### Configuration` lives inside `## Deploying`:
   required env vars visible, optional groups (REST tuning, OTel) under `<details>`.
   > **OpenTofu modules are the planned canonical deployment path.** Until they land, deploy the
   > images with any container orchestrator — the composition below is the reference for which
   > images to run, how they wire together, and the environment they expect.
3. **Contributing (slot 5) is always the last section.**
4. **Describe in prose; enumerate in tables.** The role section (slot 2) is prose — never inline a
   capability list (sub-packages, packages, endpoints, env vars) in a sentence, and never pause an
   explanation to catalog its members. Each list lives once, in its slot-4 reference table or
   wherever it already exists; point there instead of repeating it (principles 6 and 19). Rule of
   thumb: four-plus comma-separated items belong in a table, not a sentence. Lead with the
   plain-English purpose and let the table, intellisense, or the API reference carry the inventory.
   Cut every word that adds no information, and drop boilerplate ("if you have questions or run into
   issues", "check existing issues"). Cutting filler is not cutting explanation: an unfamiliar concept
   or a non-obvious rationale earns the words to make it clear, in plain language.
5. **Contribution rules live in `CONTRIBUTING.md`, not the README.** The README Contributing
   section is only the two links. The "what belongs here / bar for additions" policy, review
   norms, and any other contributor guidance go in CONTRIBUTING.md, phrased naturally.
6. **Rationale over surface.** Explain why a thing exists, what kind of logic belongs in it, and
   how a dev should approach it — the doc is a guide, not a second copy of the API. Note
   large-scale facts that are hard to spot at a glance (a service's env vars, that OTel ships
   local and GCP exporters, the deployment images), but never an inventory of functions, methods,
   or client calls (principle 6). A package or sub-package description says what it is FOR, not
   which symbols it exports.
7. **Concrete versions live only in copy-paste code blocks.** A real tag (`@v1.0.3`, image
   `:v2.3.1`) belongs only where the reader copies the block verbatim — a `uses:`, compose, or
   install snippet. In prose, placeholder examples, and inline references, use a generic `@<tag>`
   or link the latest release. A hard-coded version in prose is redundant with the repo and goes
   stale.

**The one documented exception:** `service-template` MAY prepend a `## Using this template`
section before slot 2 — its primary reader is forking it. No other repo reorders the five slots.

### Contributing section + links (fix the fleet-wide 404s)

Every repo ends with a short `## Contributing` section that points at TWO things:

1. The **developer onboarding guide** for platform setup (toolchain, the `a-novel` CLI, daily
   usage): `https://github.com/a-novel-kit/.github/blob/master/README.md`. This is the single
   canonical onboarding doc for BOTH orgs.
2. The repo's own `./CONTRIBUTING.md` for repo-specific concepts.

Each org's `.github` repo does carry a `CONTRIBUTING.md` — the **concepts** doc holding what is true
of every repo of a kind (see `contributing-doc-dedup` in practice: the layer model, service anatomy,
the libraries/tooling taxonomy). Link it when a repo doc needs a concept rather than restating it.
Platform **setup** is the onboarding guide above, which is a different document. The legacy
`a-novel-kit/.github/.../contributing/readme.md` path 404s — never link it.

`CONTRIBUTING.md` itself: intro (link the onboarding guide + "read the README first") →
repo-specific concepts → `## Questions?` (issues link). Never restate platform setup or the
service role there.
