# Project document templates

Read this reference when routed here by [write-project-docs](../SKILL.md). Its rules apply to the selected work.

Contents:

- [Phase 4: Scaffold Templates](#phase-4-scaffold-templates)
- [4.1 README.md template](#41-readmemd-template)
- [4.2 SECURITY.md template](#42-securitymd-template)
- [4.3 CONTRIBUTING.md template](#43-contributingmd-template)

## Phase 4: Scaffold Templates

All three templates below use `{{variable}}` placeholders. Substitute every placeholder with the
inputs from Phase 1 before writing the file; an unresolved `{{…}}` in the output is a bug.

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
[![codecov](https://codecov.io/gh/{{repo-path}}/graph/badge.svg)](https://codecov.io/gh/{{repo-path}}) <!-- TODO(project-docs): if this repo requires a tokenized Codecov badge, append `?token=<codecov-graph-token>` to the badge URL above using the value from codecov.io/gh/{{repo-path}}/settings > Badge -->

![Coverage graph](https://codecov.io/gh/{{repo-path}}/graphs/sunburst.svg) <!-- TODO(project-docs): if this repo requires a tokenized Codecov sunburst, append `?token=<codecov-graph-token>` to the image URL above -->

## What it does

<!-- Mandatory role section per Editorial Principle 2. One to three short paragraphs: what this
     service owns (the entity, the noun), who it serves and how (the verb), and the surface
     (REST? gRPC? both? public? internal?). Then, if relevant, a one-line note on related
     concepts (e.g. "Authentication and identity live in service-authentication; this service
     only manages signing keys"). -->

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

**README structure:** the [Section order](structure.md#section-order--one-order-every-repo) table is authoritative
— it spells out the five slots and the exact headings that fill them per repo type. This scaffold only
fills slot content; it never reorders the slots.

**Mechanical rules:**

- Badges follow the per-repo-type set in the **Fleet standard** header table, always in the order
  shown there (socials, then `<hr />`, then the type's tech badges, then the Codecov sunburst only
  when the repo reports coverage). Deviating breaks the visual rhythm across the fleet.
- The `<hr />` literal (not `---`) separates the social badges from the repo metrics, matching
  the existing Agora convention.
- Docker compose examples pin images by an explicit release tag, never `:latest`. In prose, link
  the latest release (`…/releases/latest`) instead of a `(current: vX.Y.Z)` string that goes stale
  — see Fleet-standard hard rule 7.
- The config-vars tables use `<br/>` to stack multiple image names in a single cell, keeping the
  table narrow.
- If you cannot fill in the role section's three answers — the entity, the consumers, the surfaces —
  stop and collect them (Principle 2).

### 4.2 SECURITY.md template

This file is near-boilerplate: only `{{org-label}}` and `{{security-email}}` are substituted. Do
not rewrite it to "improve" it — consistency across Agora services matters more than prose polish.

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

The template intentionally has gaps: a real CONTRIBUTING is mostly the bespoke "service-specific
concepts" section, which cannot be templated, so update mode (Phase 5) is the common case and the
skill's job there is to enforce the structure — audience, no duplicated content, no platform-wide
setup — not to generate that content.

**What CONTRIBUTING should contain**, in the template's order: one short intro paragraph linking the
org-wide contribution guide; **Quick local interactions**, the curl / grpcurl on-ramp for a contributor
who already has the service running (`a-novel run start <service>/<target>`); **Service-specific
concepts**, the bespoke section the template comment details, whose config field names are subject to
Editorial Principle 4; and **Questions**, identical across services.

**Structure and audience:** CONTRIBUTING serves contributors only — people who will edit this
codebase (Principle 1) — so a section an operator or integrator would want is in the wrong file.
Concretely:

- The "What it does" / role description belongs in the README; contributors are expected to have read
  it first.
- Contributors already know the stack, so do NOT re-document the framework or platform itself
  (GitHub Actions mechanics, the Go clean-architecture layering, the pnpm workspace model, etc.).
  Link its official docs and spend the words only on what is specific to THIS repo — its
  conventions, directory layout, and build/release model. Generic Go layering diagrams
  (DAO → service → handler) live in the `write-go-service` skill, not in per-project docs.
- Client install snippets and published-client code examples, deployment compose blocks, and the
  env-var reference tables belong in the README. CONTRIBUTING refers to them with a link.
- Platform-wide setup (prerequisites, generic `a-novel`/`pnpm` commands, lint/test/format) belongs
  in the org-level contribution guide that the intro paragraph already links to. When that link
  exists, **omit Prerequisites and Common Commands from CONTRIBUTING entirely** — they create a
  second source of truth that drifts from the org guide. Earlier versions of this skill generated a
  Prerequisites list, an install block, and a Common Commands table here; a repo that still carries
  them loses them in the next CONTRIBUTING edit.
