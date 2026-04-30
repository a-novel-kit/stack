---
name: write-project-docs
description: >
  Write, review, and maintain the root-level project documentation — README.md, SECURITY.md,
  CONTRIBUTING.md — for Agora backend services. Use this skill whenever scaffolding a new
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

### 3. Verify every factual claim against the source

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

### 4. Show the canonical, link or table the variants

When a service has several deployment shapes (REST × gRPC × standalone × split = four
combinations), do not paste four near-identical compose blocks in sequence. The reader
who wants the simplest path is forced to scan past three blocks they will not use, and
the duplication turns any future update into a four-place edit.

Pick one canonical block — usually the simplest dev path — show it inline, then list the
other shapes in a table or collapse them under a `<details>` block, with one example of
the production-shape variant inside. The principle generalizes: any time two blocks
differ by one line, the second belongs in a diff, table, or collapsible block, not in line.

### 5. Reference, don't enumerate

Comprehensive lists of fields, methods, env vars, etc. are reference material. They go in
a dedicated section (or in generated reference docs like the OpenAPI viewer or godoc),
**after** the canonical example, never interleaved with prose. Readers who need the
reference jump to it; readers who don't are not forced to scroll past it to find the next
thing they came for.

For client packages, the README example shows the **minimum viable call** — install,
construct, one real operation. That is enough to unblock someone. The full surface is what
intellisense, `pkg.go.dev`, or the published API reference is for.

### 6. Edit in place; preserve unknown content

In update mode, treat existing custom sections (architecture diagrams, team notes,
release call-outs that are not in the template) as data, not noise. Read the whole file,
identify the section the user is changing, edit only that section. A "rewrite" instruction
from the user is the only override — and even then, surface anything that looks like
deliberate custom content before discarding it.

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

| Input                | Example                                       | Default                                       |
| -------------------- | --------------------------------------------- | --------------------------------------------- |
| Project slug         | `service-json-keys`                           | (ask — used in page title)                    |
| Org contributing URL | `a-novel/.github/blob/master/CONTRIBUTING.md` | `a-novel/.github/blob/master/CONTRIBUTING.md` |

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

## Running it

The minimal local setup is one Postgres image plus one service image. Pin both to the
same release tag (current: `vX.Y.Z`). <!-- TODO(project-docs): replace vX.Y.Z with the current image tag -->

<!-- ONE canonical compose block. The simplest dev-mode shape (typically standalone-rest
     or standalone-grpc). Do NOT paste a second compose block here for a different
     deployment shape. -->

For the other deployment shapes:

| Shape | Use when | Image |
| ----- | -------- | ----- |

<!-- One row per shape this service ships (standalone-rest, standalone-grpc, split-rest,
     split-grpc, etc.). Each cell is a single line — no compose YAML inline. -->

<details>
<summary>Production (split images) example</summary>

<!-- ONE compose block showing the split-image, migrations-as-separate-service shape.
     Inside <details> so dev readers don't pay the scroll cost. -->

</details>

> Standalone images run migrations on startup. Convenient for dev, not recommended for
> production — use the split images plus the dedicated `migrations` image instead.

### Configuration

Configuration is driven by environment variables.

**Required**

| Name | Description | Images |
| ---- | ----------- | ------ |

<!-- One row per required env var. -->

**Optional — REST**

<!-- Only when has-rest. One table per concern. -->

**Optional — Logs and tracing**

<!-- OTel + GCP project ID + app name. -->

<!-- If has-js-client: include the JS/npm usage section. Minimum-viable call only. -->
<!-- If has-go-client: include the Go module usage section. Minimum-viable call only. -->
```

**README structure (top to bottom):**

1. **Title + badge block.** Mechanical; the catalog below specifies exact URLs.
2. **What it does.** Mandatory. One-to-three paragraph role section per Editorial
   Principle 2. Comes before _anything_ about deployment.
3. **Running it.** Operator section. One canonical compose block, table or `<details>`
   for variants per Principle 4. Production-shape variant under details if it exists.
4. **Configuration.** Reference tables for env vars. Required first, optional grouped by
   concern. Lives in its own subsection after the canonical compose, never interleaved
   (Principle 5).
5. **Using the client packages.** Integrator section. One minimum-viable example per
   client (Go, JS, etc.). Link to API reference; do not enumerate the full surface.

**Mechanical rules:**

- The nine entries in the badge catalog (two socials + three repo metrics + four
  CI/coverage, counting the Codecov sunburst) always appear in the order shown. Deviating
  breaks the visual rhythm across services.
- The `<hr />` literal (not `---`) separates the social badges from the repo metrics —
  this matches the existing Agora convention.
- Docker compose examples must pin images by tag (e.g., `:v2.2.6`), never `:latest`.
  When scaffolding, ask the user for the current release version or write a
  `<!-- TODO(project-docs): current image tag (see GitHub releases) -->` placeholder.
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

For platform-wide setup, prerequisites, and the standard `make` targets, see the
[generic contribution guidelines](https://github.com/{{org-contributing-url}}). This file
documents what is specific to {{project-display-name}}.

---

## Quick local interactions

<!-- A handful of curl / grpcurl examples that hit the live service after `make run`.
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

If you have questions or run into issues:

- Open an issue at https://github.com/{{repo-path}}/issues
- Check existing issues for similar problems
- Include relevant logs and environment details
```

**Note on the template body that used to live here:** earlier versions of this skill
generated a Prerequisites list, a `make install` block, and a Common Commands table
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
- Client install snippets, deployment compose blocks, and the env-var reference tables
  belong in the README. CONTRIBUTING refers to them with a link.
- Platform-wide setup (prerequisites, generic `make` commands, lint/test/format) belongs
  in the org-level contribution guide that the intro paragraph already links to. When
  that link exists, **omit Prerequisites and Common Commands from CONTRIBUTING entirely**
  — they create a second source of truth that drifts from the org guide.

**What CONTRIBUTING should contain:**

1. **Intro paragraph + link to the org-wide contribution guide.** One short paragraph.
2. **Quick local interactions.** A handful of curl / grpcurl examples that hit the live
   service after `make run`. This is the pragmatic on-ramp for a contributor who has the
   service running and wants to poke at it.
3. **Service-specific concepts.** The bespoke section. Examples of what belongs here:
   - Domain invariants that aren't obvious from the code (e.g., main-vs-legacy key
     semantics, transaction scoping rules, materialized-view refresh requirements).
   - Cryptographic flows (which algorithm, where the key comes from, what gets sealed).
   - Config schemas the contributor will actually edit, with field names taken from the
     code. Subject to Editorial Principle 3 — verify field names against the source.
   - Scheduled-job semantics if the service has them.
   - Surface table (gRPC services / REST endpoints) as a quick orientation, not a full
     API spec — link to the proto file or OpenAPI doc for that.
4. **Questions.** Identical across services. Where to file issues, how to ask.

What does NOT belong:

- Architecture diagrams of generic Go layering (DAO → service → handler) — that's in the
  `write-go-code` skill, not in per-project docs.
- Code examples for the published client packages — that's README integrator content.
- Prerequisites and `make` command tables — those live in the org-wide contributing
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

3. The templates are intentionally Agora-flavoured (mentions of `make` targets, podman,
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
  that does not mention encryption at all. See Editorial Principle 3.
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
