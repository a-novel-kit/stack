---
name: write-project-docs
description: >
  Write, review, and maintain the root-level project documentation — README.md, SECURITY.md,
  CONTRIBUTING.md — for any repo across the a-novel / a-novel-kit orgs (backend services, the
  golib Go library, the nodelib JS packages, the workflows Actions repo, the stack CLI). Use this skill whenever scaffolding a new
  project's docs, updating an existing README/SECURITY/CONTRIBUTING (new env var, new badge,
  new client section, security contact change), or adding sections like client usage or
  Docker examples. Does NOT cover CODE_OF_CONDUCT.md (standard Contributor Covenant, copy
  verbatim) or in-source code comments (use document-code).
---

# Project Docs

This skill governs the three root-level Markdown files that describe a project to external
readers: `README.md`, `SECURITY.md`, `CONTRIBUTING.md`. These files are the first thing
visitors see; they set expectations for usage, security contact, and how to contribute. They
must be accurate, scannable, and consistent across all Agora services.

These files carry the **global picture** — the architecture, the layer split, the principles
behind the shape of the system. That is by design: code comments defer here instead of
re-explaining the whole system from inside one source file (see `document-code`). Keep the
wide-angle view in README and CONTRIBUTING and keep it current, so the comments in the code can
stay local and specific.

The skill has two modes:

- **Scaffold** — the file does not exist yet (new project). Generate from the templates in
  this skill, substituting project-specific inputs.
- **Update** — the file exists. Edit the relevant section in place. Never overwrite a whole
  file when the ask is to change one section.

Separate concerns:

- `CODE_OF_CONDUCT.md` — **not managed by this skill.** Copy the Contributor Covenant
  verbatim from <https://www.contributor-covenant.org/version/2/1/code_of_conduct.md>
  when setting up a new project and do not edit further.
- `document-code` — governs doc comments inside source files (Go, SQL, TS, etc.), not these
  project-level Markdown files.

---

## Fleet standard (current)

This section is **authoritative and supersedes any older guidance below** where they conflict.
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
| Go service (`service-*`) | go-mod version · file count · code size · CI status · Go Report Card         | yes      |
| Go library (`golib`)     | go-mod version · file count · code size · CI status · Go Report Card         | no       |
| JS package (`nodelib`)   | file count · code size · CI status                                           | yes      |
| Actions (`workflows`)    | file count · code size · CI status                                           | no       |
| CLI (`stack`)            | go-mod version (`?filename=cli/go.mod`) · file count · code size · CI status | no       |

"Codecov if applicable" = the repo's CI calls the `generic-actions/codecov` action. Verify
before adding the badge: `grep -rl generic-actions/codecov <repo>/.github/workflows`. Today
services + `nodelib` report coverage; `golib`, `workflows`, `stack` do not.

### Section order — ONE order, every repo

Every README uses the same five slots in the same order. Only the _content_ of each slot
changes with repo type; **the order never does.** Readers scroll as little as possible —
frequently-referenced material first, rarely-needed material last.

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
4. **Describe in prose; enumerate in tables.** The role section (slot 2) is prose — never inline
   a capability list (sub-packages, packages, endpoints, env vars) in a sentence. Each list lives
   once, in its slot-4 reference table; duplicating it in prose breaks rhythm and creates a second
   place to maintain. Rule of thumb: four-plus comma-separated items belong in a table, not a
   sentence. Lead with the plain-English purpose; let the table, intellisense, or the API
   reference carry the inventory. Keep it tight — cut every word that does not add information.
   Prefer the precise word over a long phrase and short sentences over long ones; the right noun
   or verb often replaces a whole clause. Boilerplate ("if you have questions or run into issues",
   "check existing issues") is filler — drop it. Concision is careful word choice, not dropped
   grammar — keep sentences fully formed (subject + verb), not terse fragments. And cutting filler
   is not the same as cutting explanation: when a concept is unfamiliar or a rationale is
   non-obvious, spend the words to make it clear, in plain language. A doc that is short but
   cryptic has failed — clarity beats brevity. And don't pause an explanation to catalog its
   members: lead with the concept, and enumerate separately (its own section) only if the list
   adds value. If that list already lives in a catalog or table elsewhere — the README's, a
   reference table — point there instead of repeating it. A catalog dropped mid-explanation is
   verbose, breaks rhythm, isn't memorable, and drifts out of sync over time.
5. **Contribution rules live in `CONTRIBUTING.md`, not the README.** The README Contributing
   section is only the two links. The "what belongs here / bar for additions" policy, review
   norms, and any other contributor guidance go in CONTRIBUTING.md, phrased naturally.
6. **Rationale over surface.** Explain why a thing exists, what kind of logic belongs in it, and
   how a dev should approach it — the doc is a guide, not a second copy of the API. Note
   large-scale facts that are hard to spot at a glance (a service's env vars, that OTel ships
   local and GCP exporters, the deployment images), but never an inventory of functions, methods,
   or client calls — intellisense and the code already supply those. A package or sub-package
   description says what it is FOR, not which symbols it exports.
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

There is **no** org-level `CONTRIBUTING.md` in either `.github` repo. Older docs linked
`a-novel/.github/.../CONTRIBUTING.md` or `a-novel-kit/.github/.../contributing/readme.md` —
both 404. Always use the onboarding-guide URL above.

`CONTRIBUTING.md` itself: intro (link the onboarding guide + "read the README first") →
repo-specific concepts → `## Questions?` (issues link). Never restate platform setup or the
service role there.

---

## Editorial Principles

These come before the templates. The templates implement them; if a generated file looks
right but violates a principle, the principle wins.

### 1. Audience-first — name the reader before writing the section

Every section in `README.md` and `CONTRIBUTING.md` answers a question that a specific
reader is holding. Three readers exist:

| Reader                | What they want                                     | File              |
| --------------------- | -------------------------------------------------- | ----------------- |
| **Operator**          | "How do I run this service?"                       | `README.md`       |
| **Client integrator** | "How do I call this service from another service?" | `README.md`       |
| **Contributor**       | "How do I work on this codebase locally?"          | `CONTRIBUTING.md` |

A section that doesn't answer one of those questions doesn't belong. A section that
answers the same question for the same reader twice (in two files, or in two places in
one file) gets cut to one location and linked.

`README.md` serves operators and integrators only. `CONTRIBUTING.md` serves contributors
only. **Never duplicate** content across them — link instead. If the JS client install
snippet is in `README.md`, `CONTRIBUTING.md` says "see the README" rather than copying it.

### 2. Lead with the role, not the runbook

The first text after the badges in `README.md` is **what the service does and why it
exists** — one to three short paragraphs. Not "what stack it's built on", not "how to
deploy it", not the table of contents. A reader who cannot tell what the service does
from the first paragraph will not find that out by scrolling further.

A good role section answers:

- What does this service own? (the noun: "signing keys", "narrative state", "user
  identities")
- Who does it serve? (the verb: "lets other services sign tokens", "stores the in-progress
  story", "authenticates users")
- What is the surface? (REST? gRPC? both? public? internal?)

If the answer is "I don't know" for any of those, find out before drafting — the role
section _is_ the doc.

### 3. Open with what the document is about, not with a mechanism

The first sentence of a guide (a README role paragraph, a CONTRIBUTING page, any standalone doc) says
**what the document is about**, in plain purpose terms: "This document is about how a feature is planned,
built, and shipped." The reader gets the frame before any concrete detail.

Never open with an implementation fact. "Every piece of work is a GitHub issue" drops the reader into the
mechanism with no context. It reads like a machine stating a fact, not a person explaining a thing. State
the purpose first, then let the concrete pieces arrive as its answer. The unit ("an issue") lands on its
own once the reader knows why it exists: to turn an idea into buildable work.

Guide, don't assert. "This document is about…" orients the reader. "We plan before we build" commands
them. Prescriptive, first-person openers read as manifesto, not guide.

Test it on the first two sentences alone. A stranger should know what the doc covers and why, and should
not yet have met one implementation term without a reason for it.

### 4. Verify every factual claim against the source

A README that says "AES-GCM" when the code uses NaCl secretbox is worse than a README
that says nothing about encryption. Before describing any of:

- Cryptographic primitives (algorithm names, modes, key sizes)
- Configuration field names and shapes (YAML keys, env var names)
- API surface (RPC names, REST paths, status codes)
- Lifecycle states (active / expired / deleted)
- File paths referenced in prose

…open the source and confirm. The doc commit must reflect the code at the same SHA. When
the code changes one of these things, the doc update belongs in the same change, not a
follow-up.

### 5. Show the canonical, link or table the variants

When a service has several deployment shapes (REST × gRPC × standalone × split = four
combinations), do not paste four near-identical compose blocks in sequence. The reader
who wants the simplest path is forced to scan past three blocks they will not use, and
the duplication turns any future update into a four-place edit.

Pick one canonical block — the **production / expected shape** (per the Fleet standard above:
lead with production, relegate the dev one-liner to "Running locally") — show it inline, then
list the other shapes in a table or collapse them under a `<details>` block. The principle generalizes: any time two blocks
differ by one line, the second belongs in a diff, table, or collapsible block, not in line.

### 6. Reference, don't enumerate

Comprehensive lists of fields, methods, env vars, etc. are reference material. They go in
a dedicated section (or in generated reference docs like the OpenAPI viewer or godoc),
**after** the canonical example, never interleaved with prose. Readers who need the
reference jump to it; readers who don't are not forced to scroll past it to find the next
thing they came for.

For client packages, the README example shows the **minimum viable call** — install,
construct, one real operation. That is enough to unblock someone. The full surface is what
intellisense, `pkg.go.dev`, or the published API reference is for.

### 7. Edit in place; preserve unknown content

In update mode, treat existing custom sections (architecture diagrams, team notes,
release call-outs that are not in the template) as data, not noise. Read the whole file,
identify the section the user is changing, edit only that section. A "rewrite" instruction
from the user is the only override — and even then, surface anything that looks like
deliberate custom content before discarding it.

### 8. Explain by rationale, not by example

A doc earns its keep with a clear, well-phrased explanation of what a thing is, why it
exists, and how to approach it — not with examples. Case-specific examples (a particular
provider, key, service, or value) go stale as the system changes, break the reading
rhythm, and usually carry no information the surrounding prose doesn't already give. In
particular, **never import an example from the conversation that produced the doc** — it
was an aid for the author, not the reader, and lands as arbitrary and dated later.

The one legitimate example is an **analogy that makes a genuinely hard concept click** —
reach for it only then. Otherwise state the concept directly. When concrete specifics are
genuinely needed (exact commands, names, links), they are reference material, not
explanation: keep the explanatory prose general and push the specifics to where they
belong — the relevant service's own `CONTRIBUTING.md`, a reference table, or a copy-paste
code block.

### 9. Plain language, plain sentences

Write so a tired reader gets it on the first pass. Prefer the common word and the short one;
reach for an advanced or technical word only when it earns its place, when it replaces a whole
phrase or removes a real ambiguity, not for tone. State each point as a plain subject-verb-object
sentence, not a label: "The service signs tokens server-side because the private key never leaves
it," not "Why server-side: the private key." Rhetorical labels ("Why this matters:", "The
reason:", "Note:") are ceremony; cut them and let the sentence stand.

Prefer basic structure to clever construction. If the same meaning fits a simpler word and a
plainer sentence, use them: "hands most of the automation" reads better than "hands you the
automation for the rest," at no cost to meaning. Two short sentences beat one long one spliced
together. Avoid the em-dash that cuts a sentence in half, and the enumeration that only restates
what you just wrote. When a draft feels dense, revise for plainness before shipping; the plain
version is the finished one, not a step toward it.

### 10. Show the flow, don't announce it

Let the reader absorb the model by reading, not by being told its shape. A section named "The big
picture" or "The design" announces your structure instead of teaching the subject; drop the label
and let the content be the picture. Headings should name what the reader is doing or learning at
that point ("When the board needs you"), not the role the section plays in your outline.

Prefer showing to telling. A worked path teaches the model better than a list of definitions: an
idea becomes a Task, the Task becomes a Pull Request, the Pull Request merges and ships. When a
sequence of states is the point, a small text diagram beats a paragraph; keep it visual and sparse,
a few words per node, and mark what matters, like who acts or where a gate falls.

### 11. Keep it self-contained, no links to ephemeral work items

Never point a doc at a specific issue, epic, or PR, by number or by link. They close, archive, and
get renumbered, so "see #46" rots and leads nowhere. If a rationale is worth keeping, write it into
the doc itself. Commit messages and PR descriptions may reference issues; docs may not.

---

## Phase 1: Collect Required Inputs

Before scaffolding a new file, ask the user for the inputs below. When running in **update**
mode, only ask for the inputs that are relevant to the section being edited.

Collect them in a single message rather than asking one question at a time.

### 1.1 Always required

| Input                | Example                     | Default for Agora        |
| -------------------- | --------------------------- | ------------------------ |
| Project display name | `JSON Keys service`         | (ask)                    |
| Repo path (org/repo) | `a-novel/service-json-keys` | `a-novel/<service-slug>` |
| Main branch          | `master`                    | `master`                 |

### 1.2 Required for README.md

| Input               | Example                                                   | Default / Fallback                                                                                       |
| ------------------- | --------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| Main CI workflow    | `main.yaml`                                               | `main.yaml`                                                                                              |
| Codecov graph token | `almKepuGQE` (public token, safe to commit)               | Omit the `?token=…` query parameter entirely and leave a `TODO(project-docs)` comment next to the badge. |
| Twitter handle      | `agorastoryverse`                                         | `agorastoryverse`                                                                                        |
| Discord invite ID   | numeric ID `1315240114691248138` + invite code `rp4Qr8cA` | same as existing services                                                                                |

The codecov graph token is **public** — it only controls badge/graph rendering, not repo
access. Safe to commit. The private upload token lives in CI secrets, never in docs.

### 1.3 Required for SECURITY.md

| Input                       | Example                                | Default                                |
| --------------------------- | -------------------------------------- | -------------------------------------- |
| Org / project display label | `A-Novel` (used in running prose)      | `A-Novel`                              |
| Security contact email      | `geoffroy.vincent@agorastoryverse.com` | `geoffroy.vincent@agorastoryverse.com` |

### 1.4 Required for CONTRIBUTING.md

| Input                    | Example                                     | Default                                                                                                                        |
| ------------------------ | ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| Project slug             | `service-json-keys`                         | (ask — used in page title)                                                                                                     |
| Developer onboarding URL | `a-novel-kit/.github/blob/master/README.md` | `a-novel-kit/.github/blob/master/README.md` (single canonical onboarding doc for both orgs; there is NO org `CONTRIBUTING.md`) |

### 1.5 Capability flags (shape template output)

Ask the user which of these apply. Each flag turns a section of README/CONTRIBUTING on or
off:

| Flag               | What it enables                                                     |
| ------------------ | ------------------------------------------------------------------- |
| `has-grpc`         | gRPC compose examples + `grpcurl` interaction snippets              |
| `has-rest`         | REST compose examples + `curl` interaction snippets                 |
| `has-standalone`   | Standalone (all-in-one) image in addition to split images           |
| `has-go-client`    | `pkg/go` usage example in README, Go client section in CONTRIBUTING |
| `has-js-client`    | `pkg/js` usage example in README, JS client section in CONTRIBUTING |
| `has-openapi-docs` | Link to GitHub Pages docs in README + redocly/scalar mention        |
| `has-cron-jobs`    | Scheduled-job section (like rotate-keys) in CONTRIBUTING            |

When an input is unknown or the user declines to provide it, insert an HTML TODO comment
(see [Handling Missing Values](#handling-missing-values)) rather than guessing or leaving
the field blank.

---

## Phase 2: Detect Scaffold vs. Update

```bash
ls README.md SECURITY.md CONTRIBUTING.md 2>/dev/null
```

- File missing → scaffold mode: generate from template in Phase 4
- File present → update mode: read it first, edit the relevant section only (Phase 5)

Never overwrite an existing file with the full template. Users may have added custom
sections (architecture diagrams, org-specific footers, release notes) that are not in the
template — replacing the file silently drops them.

---

## Phase 3: Handling Missing Values

When an input is required but not available, write an HTML TODO comment at the exact
location where the value belongs. Format:

```markdown
<!-- TODO(project-docs): <what is missing> — <where to get it> -->
```

HTML comments do not render in GitHub's Markdown preview, so the file still looks clean to
visitors, but grep finds them instantly:

```bash
grep -rn "TODO(project-docs)" .
```

Examples:

```markdown
[![codecov](https://codecov.io/gh/a-novel/service-json-keys/graph/badge.svg)](https://codecov.io/gh/a-novel/service-json-keys) <!-- TODO(project-docs): add ?token=<graph-token> from codecov.io/gh/a-novel/service-json-keys/settings > Badge if a tokenized badge is required -->
```

```markdown
Report security bugs by emailing the lead maintainer at <!-- TODO(project-docs): security contact email -->.
```

**Do not** invent values. A fake email or a placeholder like `YOUR_TOKEN_HERE` that a
reader might mistake for real content is worse than a visible TODO.

---

## Phase 4: Scaffold Templates

All three templates below use `{{variable}}` placeholders. Substitute every placeholder
with the inputs from Phase 1 before writing the file. Do not leave `{{…}}` in the output
— unresolved placeholders are a bug.

### 4.1 README.md template

```markdown
# {{project-display-name}}

[![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/{{twitter-handle}})](https://twitter.com/{{twitter-handle}})
[![Discord](https://img.shields.io/discord/{{discord-id}}?logo=discord)](https://discord.gg/{{discord-invite-code}})

<hr />

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/{{repo-path}})
![GitHub repo file or directory count](https://img.shields.io/github/directory-file-count/{{repo-path}})
![GitHub code size in bytes](https://img.shields.io/github/languages/code-size/{{repo-path}})

![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/{{repo-path}}/{{main-workflow-file}})
[![Go Report Card](https://goreportcard.com/badge/github.com/{{repo-path}})](https://goreportcard.com/report/github.com/{{repo-path}})
[![codecov](https://codecov.io/gh/{{repo-path}}/graph/badge.svg)](https://codecov.io/gh/{{repo-path}}) <!-- TODO(project-docs): if this repo requires a tokenized Codecov badge, append `?token=<codecov-graph-token>` to the badge URL above using the value from codecov.io/gh/{{repo-path}}/settings > Badge -->

![Coverage graph](https://codecov.io/gh/{{repo-path}}/graphs/sunburst.svg) <!-- TODO(project-docs): if this repo requires a tokenized Codecov sunburst, append `?token=<codecov-graph-token>` to the image URL above -->

## What it does

<!-- Mandatory role section per Editorial Principle 2. One to three short paragraphs that
     answer:
       - what does this service own (the entity, the noun)
       - who does it serve and how (the verb)
       - what's the surface (REST? gRPC? both? public? internal?)
     Then, if relevant, a one-line note on related concepts (e.g. "Authentication and
     identity live in service-authentication; this service only manages signing keys").
-->

## Deploying

The service runs as published OCI images plus Postgres; both surfaces are stateless and scale to
multiple replicas.

> **OpenTofu modules are the planned canonical deployment path.** Until they land, deploy the
> images with any container orchestrator — the composition below is the reference for which
> images to run, how they wire together, and the environment they expect.

| Image | Role |
| ----- | ---- |

<!-- One row per PUBLISHED image. Verify the exact names against
     .github/workflows/release.yaml (image_name:) — they are often `<repo>/jobs/<name>`
     (e.g. jobs/migrations, jobs/rotatekeys), NOT a bare `<repo>/<name>`. -->

Pin every image to the same release tag — see the [latest release](https://github.com/{{repo-path}}/releases/latest).

<!-- ONE canonical PRODUCTION compose block: database -> migrations (to completion) -> the
     split server(s). This is the lead per Fleet-standard hard rule 2 — never the dev one. -->

### Configuration

<!-- Required env vars in a visible table; optional groups (REST tuning, OTel) under <details>.
     The Images column lists EVERY image that reads each var — verify against internal/config,
     and note when the rest surface maps ${REST_PORT} rather than ${GRPC_PORT}. -->

| Name | Description | Images |
| ---- | ----------- | ------ |

## Using the client packages

<!-- One minimum-viable example per client (Go, JS). Link the API reference / pkg.go.dev;
     do not enumerate the full surface (Principle 6). -->

## Running locally

<!-- LAST section before Contributing. The standalone single-command dev compose (bundles
     migrations), with the dev-only caveat. Relegated on purpose — least-retrieved. Point
     contributors at the a-novel CLI + CONTRIBUTING. -->

## Contributing

<!-- Two links only: the onboarding guide and ./CONTRIBUTING.md (see Fleet standard). -->
```

**README structure:** follow the [Section order](#section-order--one-order-every-repo) table —
authoritative. For a service that is Header → `## What it does` → `## Deploying` (with
`### Configuration`) → `## Using the client packages` → `## Running locally` → `## Contributing`.
This scaffold only fills slot content; it never reorders the slots.

**Mechanical rules:**

- Badges follow the per-repo-type set in the **Fleet standard** header table, always in the
  order shown there (socials, then `<hr />`, then the type's tech badges, then the Codecov
  sunburst only when the repo reports coverage). Deviating breaks the visual rhythm across the
  fleet.
- The `<hr />` literal (not `---`) separates the social badges from the repo metrics —
  this matches the existing Agora convention.
- Docker compose examples pin images by an explicit release tag, never `:latest`. In PROSE, link
  the latest release (`…/releases/latest`) rather than hard-coding a `(current: vX.Y.Z)` string
  that goes stale; reserve concrete tags for inside the compose blocks (where the publish stamp
  keeps them current).
- The config-vars tables use `<br/>` to stack multiple image names in a single cell —
  keeps the table narrow.
- The role section ("What it does") must name the entity, the consumers, and the
  surfaces (REST/gRPC, public/internal). If you cannot fill in those three, stop and
  collect them — see Principle 2.

### 4.2 SECURITY.md template

This file is near-boilerplate. Only `{{org-label}}` and `{{security-email}}` are
substituted. Do not rewrite the boilerplate to "improve" it — consistency across Agora
services matters more than prose polish.

```markdown
# Security Policies and Procedures

This document outlines security procedures and general policies for the `{{org-label}}`
project.

- [Reporting a Bug](#reporting-a-bug)
- [Disclosure Policy](#disclosure-policy)
- [Comments on this Policy](#comments-on-this-policy)

## Reporting a Bug

The `{{org-label}}` team and community take all security bugs in `{{org-label}}` seriously.
Thank you for improving the security of `{{org-label}}`. We appreciate your efforts and
responsible disclosure and will make every effort to acknowledge your
contributions.

Report security bugs by emailing the lead maintainer at {{security-email}}.

The lead maintainer will acknowledge your email within 48 hours, and will send a
more detailed response within 48 hours indicating the next steps in handling
your report. After the initial reply to your report, the security team will
endeavor to keep you informed of the progress towards a fix and full
announcement, and may ask for additional information or guidance.

Report security bugs in third-party modules to the person or team maintaining
the module.

## Disclosure Policy

When the security team receives a security bug report, they will assign it to a
primary handler. This person will coordinate the fix and release process,
involving the following steps:

- Confirm the problem and determine the affected versions.
- Audit code to find any potential similar problems.
- Prepare fixes for all releases still under maintenance. These fixes will be
  released as fast as possible.

## Comments on this Policy

If you have suggestions on how this process could be improved please submit a
pull request.
```

### 4.3 CONTRIBUTING.md template

```markdown
# Contributing to {{project-slug}}

For platform-wide setup (Go, Node, Podman, the `a-novel` CLI) and the day-to-day `a-novel` /
`pnpm` commands, see the
[developer onboarding guide](https://github.com/{{developer-onboarding-url}}). This file
documents what is specific to {{project-display-name}}.

---

## Quick local interactions

<!-- A handful of curl / grpcurl examples that hit the live service after `a-novel run start`.
     This is the pragmatic on-ramp for a contributor who already has the service running.
     Do NOT include compose blocks, env-var tables, or client install instructions —
     those live in the README. -->

---

## Service-specific concepts

<!-- The bespoke section. The exact subsections vary per service. Common patterns:

       - Domain invariants that aren't obvious from the code (e.g., main-vs-legacy key
         semantics, transaction scoping rules, view refresh requirements).
       - Cryptographic flows (algorithm name verified against the source, where the key
         comes from, what gets sealed and where).
       - Config schemas the contributor will actually edit. Field names taken from the
         code at the same SHA, not from memory or other docs.
       - Scheduled-job semantics if the service has them.
       - Surface table (gRPC services / REST endpoints) as quick orientation, not a full
         API spec — link to the proto file or OpenAPI doc for that.

     Each subsection has a clear "what would surprise a contributor here" hook. If you
     can't articulate that hook, the subsection is filler and should be cut.
-->

---

## Questions?

[Open an issue](https://github.com/{{repo-path}}/issues) — include logs and environment details.
```

**Note on the template body that used to live here:** earlier versions of this skill
generated a Prerequisites list, an install block, and a Common Commands table
inside CONTRIBUTING. Those were moved out — they belong in the org-wide contribution
guide that the intro paragraph already links to. Duplicating them per repo creates a
second source of truth that drifts. If you are working on a repo that still has them,
remove them as part of the next CONTRIBUTING edit.

<!-- The template above intentionally has gaps. Phase 5 (update mode) is the common case
     for CONTRIBUTING — a real CONTRIBUTING.md is mostly the bespoke "service-specific
     concepts" section, which cannot be templated. The skill's job in update mode is to
     enforce the structure (audience, no duplicated content, no platform-wide setup), not
     to generate the bespoke content. -->

**CONTRIBUTING structure and audience:**

CONTRIBUTING serves contributors only — people who will edit this codebase. It does **not**
serve operators (they read the README) or integrators (they also read the README). A
section that an operator or integrator would want is in the wrong file.

Concretely, this means:

- The "What it does" / role description belongs in the README, not here. Contributors are
  expected to have read the README first.
- Contributors already know the stack, so do NOT re-document the framework or platform itself
  (GitHub Actions mechanics, the Go clean-architecture layering, the pnpm workspace model, etc.).
  Link its official docs and spend the words only on what is specific to THIS repo — its
  conventions, directory layout, and build/release model.
- Client install snippets, deployment compose blocks, and the env-var reference tables
  belong in the README. CONTRIBUTING refers to them with a link.
- Platform-wide setup (prerequisites, generic `a-novel`/`pnpm` commands, lint/test/format) belongs
  in the org-level contribution guide that the intro paragraph already links to. When
  that link exists, **omit Prerequisites and Common Commands from CONTRIBUTING entirely**
  — they create a second source of truth that drifts from the org guide.

**What CONTRIBUTING should contain:**

1. **Intro paragraph + link to the org-wide contribution guide.** One short paragraph.
2. **Quick local interactions.** A handful of curl / grpcurl examples that hit the live
   service after `a-novel run start <service>/<target>`. This is the pragmatic on-ramp for a contributor who has the
   service running and wants to poke at it.
3. **Service-specific concepts.** The bespoke section. Examples of what belongs here:
   - Domain invariants that aren't obvious from the code (e.g., main-vs-legacy key
     semantics, transaction scoping rules, materialized-view refresh requirements).
   - Cryptographic flows (which algorithm, where the key comes from, what gets sealed).
   - Config schemas the contributor will actually edit, with field names taken from the
     code. Subject to Editorial Principle 4 — verify field names against the source.
   - Scheduled-job semantics if the service has them.
   - Surface table (gRPC services / REST endpoints) as a quick orientation, not a full
     API spec — link to the proto file or OpenAPI doc for that.
4. **Questions.** Identical across services. Where to file issues, how to ask.

What does NOT belong:

- Architecture diagrams of generic Go layering (DAO → service → handler) — that's in the
  `write-go-service` skill, not in per-project docs.
- Code examples for the published client packages — that's README integrator content.
- Prerequisites and command tables — those live in the org-wide contributing
  guide.
- Restating the role of the service — that's the README.

---

## Phase 5: Update Mode

When one of the three files already exists, `Read` it first, then `Edit` the specific
section that needs changing. Do not rewrite the file.

### Typical update operations

| Request                                | Action                                                                                             |
| -------------------------------------- | -------------------------------------------------------------------------------------------------- |
| "Bump the Docker image tags in README" | Edit every `image: ghcr.io/.../…:vX.Y.Z` line to the new version                                   |
| "Add env var FOO to README"            | Add a new row to the matching config-vars table                                                    |
| "Change security contact"              | Edit the existing email address on the `Report security bugs...` line in SECURITY.md               |
| "New gRPC service added"               | Add row to the gRPC services table in CONTRIBUTING.md                                              |
| "Project got a JS client"              | Add the JS usage section to README, add JS client section to CONTRIBUTING, flip `has-js-client` on |
| "Remove deprecated ENV var"            | Delete the table row in README; surface to user since removal may be breaking                      |

### Update rules

- **Preserve unknown content.** If the file has sections you don't recognize (custom
  architecture notes, team-specific tips), leave them untouched.
- **One logical edit per call.** When adding a new env var, update only that table; do not
  also "touch up" unrelated sections while there.
- **Check for stale cross-references.** Adding a new gRPC service to the CONTRIBUTING
  table means the README's service list (if any) needs the same row.
- **Version bumps touch every occurrence.** A release bump affects every compose YAML in
  the README — use `Edit` with `replace_all` only when you have verified the old string is
  unique to the version (e.g., `:v2.2.6`), otherwise do it one-by-one.

---

## Badge Catalog

Copy these patterns verbatim — the exact URL format matters (shields.io is strict about
path segments). Parameters are clearly marked with `{{…}}`.

| Badge                    | Markdown pattern                                                                                                                                                                                                                     |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Twitter follow           | `[![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/{{twitter-handle}})](https://twitter.com/{{twitter-handle}})`                                                                                                 |
| Discord                  | `[![Discord](https://img.shields.io/discord/{{discord-id}}?logo=discord)](https://discord.gg/{{discord-invite-code}})`                                                                                                               |
| Go version (from go.mod) | `![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/{{repo-path}})`                                                                                                                                         |
| File count               | `![GitHub repo file or directory count](https://img.shields.io/github/directory-file-count/{{repo-path}})`                                                                                                                           |
| Code size                | `![GitHub code size in bytes](https://img.shields.io/github/languages/code-size/{{repo-path}})`                                                                                                                                      |
| CI workflow status       | `![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/{{repo-path}}/{{main-workflow-file}})`                                                                                                      |
| Go Report Card           | `[![Go Report Card](https://goreportcard.com/badge/github.com/{{repo-path}})](https://goreportcard.com/report/github.com/{{repo-path}})`                                                                                             |
| Codecov badge            | Default (no token): `[![codecov](https://codecov.io/gh/{{repo-path}}/graph/badge.svg)](https://codecov.io/gh/{{repo-path}})` — add `?token={{codecov-graph-token}}` to the badge URL only when the repo requires a tokenized variant |
| Codecov sunburst         | Default (no token): `![Coverage graph](https://codecov.io/gh/{{repo-path}}/graphs/sunburst.svg)` — add `?token={{codecov-graph-token}}` to the image URL only when a tokenized variant is required                                   |

**Codecov graph token — not a secret.** It is the public badge token from
`codecov.io/gh/<repo>/settings > Badge`. Committing it is intentional. The private upload
token (used by the CI `report-codecov` job) lives in repo secrets, never here.

---

## Portability to New Projects

This skill lives in this repo's `.claude/skills/write-project-docs/`. To reuse it in a new
Agora service:

1. Copy the directory into the new repo's `.claude/skills/`:

   ```bash
   cp -r /path/to/this-repo/.claude/skills/write-project-docs \
         /path/to/new-repo/.claude/skills/
   ```

2. In the new repo, invoke the skill (Claude picks it up once the file exists). Phase 1
   will collect the new project's inputs.

3. The templates are intentionally Agora-flavoured (mentions of `a-novel` verbs, podman,
   `.github/CONTRIBUTING.md`, etc.). That is a feature, not a bug — it keeps docs consistent
   across services. When a new project legitimately deviates (different tooling, different
   org), update the templates in place rather than forking them.

---

## What NOT to Do

- **Do not edit `CODE_OF_CONDUCT.md`.** It's the Contributor Covenant verbatim. Changes to
  it are org-wide policy, not per-repo.
- **Do not add "Live Demo" / "Screenshots" / "Roadmap" sections** unless the user asks.
  They become stale fast and aren't in the Agora template.
- **Do not write long prose.** README and CONTRIBUTING are reference documents. Tables,
  bullet lists, and runnable code blocks beat paragraphs.
- **Do not put case-specific examples in generic prose.** A particular provider, key, or
  value dates fast and rarely adds understanding; never import an example from the
  conversation that produced the doc. Explain by rationale; reach for an example only as an
  analogy for a genuinely hard concept, and push concrete specifics (commands, names,
  links) to a service's own `CONTRIBUTING.md`, a reference table, or a code block. See
  Editorial Principle 8.
- **Do not embed secrets.** The codecov graph token is fine (public). API keys, passwords,
  real `APP_MASTER_KEY` values, npm auth tokens are not.
- **Do not link to internal-only dashboards.** Anything linked in the README is
  publicly visible once the repo is public.
- **Do not invent version numbers or email addresses** to fill placeholders. Use the TODO
  comment pattern.
- **Do not describe code from memory.** Algorithm names, env var names, config field
  shapes, RPC names, REST paths, lifecycle states — every factual claim about the
  codebase gets verified against the source at the same SHA as the doc commit. A
  README that says "AES-GCM" when the code uses NaCl secretbox is worse than a README
  that does not mention encryption at all. See Editorial Principle 4.
- **Do not duplicate content across README and CONTRIBUTING.** If a fact lives in one,
  the other links to it. Common offenders: client install snippets, env-var tables,
  service role description. Pick one home; link from the other. See Editorial Principle 1.

---

## Quick Reference

| Situation                                    | Skill phase                                                  |
| -------------------------------------------- | ------------------------------------------------------------ |
| New project, all docs missing                | Phase 1 (collect inputs) → Phase 4 (scaffold all three)      |
| "Add env var X to README"                    | Phase 5 (update mode, edit the config table)                 |
| "Update security contact email"              | Phase 5 (edit SECURITY.md only)                              |
| "Docs for a project split off from monorepo" | Phase 1 → Phase 4, then port custom sections from the parent |
| "Port these skills to new-repo"              | [Portability to New Projects](#portability-to-new-projects)  |
| Required value unavailable                   | [Handling Missing Values](#phase-3-handling-missing-values)  |
