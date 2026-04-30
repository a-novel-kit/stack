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

## Usage

<!-- Include the sub-sections below only when the corresponding capability flag is set. -->

### Docker

<!-- If has-grpc: include the gRPC compose block(s). -->
<!-- If has-rest: include the REST compose block(s). -->
<!-- If has-standalone: include both the standalone and split-image variants for each protocol. -->

Above are the minimal required configuration to run the service locally. Configuration is
done through environment variables. Below is a list of available configurations:

**Required variables**

| Name | Description | Images |
| ---- | ----------- | ------ |

<!-- One row per required env var. -->

**Optional variables**

<!-- Group by concern: REST API, Logs & Tracing, etc. One table per group. -->

<!-- If has-js-client: include the JS/npm usage section. -->
<!-- If has-go-client: include the Go module usage section. -->
```

**README section guidance:**

- The nine entries in the catalog (two socials + three repo metrics + four CI/coverage,
  counting the Codecov sunburst) always appear in the order shown. Deviating breaks the
  visual rhythm across services.
- The `<hr />` literal (not `---`) separates the social badges from the repo metrics — this
  matches the existing Agora convention.
- Docker compose examples must pin images by tag (e.g., `:v2.2.6`), never `:latest`. When
  scaffolding, ask the user for the current release version or write a
  `<!-- TODO(project-docs): current image tag (see GitHub releases) -->` placeholder.
- The config-vars tables use `<br/>` to stack multiple image names in a single cell —
  keeps the table narrow.
- Client usage snippets should demonstrate the **minimum viable call** (ping + one real
  operation), not every available method. Readers will find the rest in the API reference.

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

````markdown
# Contributing to {{project-slug}}

Welcome to the {{project-display-name}} for the A-Novel platform. This guide will help you understand the codebase, set
up your development environment, and contribute effectively.

Before reading this guide, if you haven't already, please check the
[generic contribution guidelines](https://github.com/{{org-contributing-url}}) that are relevant
to your scope.

---

## Quick Start

### Prerequisites

The following must be installed on your system.

- [Go](https://go.dev/doc/install)
- [Node.js](https://nodejs.org/en/download)
  - [pnpm](https://pnpm.io/installation)
- [Podman](https://podman.io/docs/installation)
- (optional) [Direnv](https://direnv.net/)
- Make
  - `sudo apt-get install build-essential` (apt)
  - `sudo pacman -S make` (arch)
  - `brew install make` (macOS)
  - [Make for Windows](https://gnuwin32.sourceforge.net/packages/make.htm)

### Bootstrap

Install the dependencies:

```bash
make install
```

### Common Commands

| Command         | Description                      |
| --------------- | -------------------------------- |
| `make run`      | Start all services locally       |
| `make test`     | Run all tests                    |
| `make lint`     | Run all linters                  |
| `make format`   | Format all code                  |
| `make build`    | Build Docker images locally      |
| `make generate` | Generate mocks and protobuf code |

### Interacting with the Service

<!-- If has-rest: include REST interaction snippets (curl). -->
<!-- If has-grpc: include gRPC interaction snippets (grpcurl). -->

---

## Project-Specific Guidelines

> This section contains patterns specific to this {{project-display-name}}.

<!-- TODO(project-docs): document the project-specific patterns — typical sections:
     - domain concepts (what the service owns / produces)
     - lifecycle of the primary entity (states, transitions)
     - key configuration files and what they control
     - scheduled jobs / cron
     - gRPC services table (if has-grpc)
     - REST API overview (if has-rest)
     - client packages (if has-go-client / has-js-client)
-->

---

## Questions?

If you have questions or run into issues:

- Open an issue at https://github.com/{{repo-path}}/issues
- Check existing issues for similar problems
- Include relevant logs and environment details
````

**CONTRIBUTING section guidance:**

- The **Quick Start** and **Questions** sections are identical across all Agora services.
  Do not customize them per project — that creates drift.
- The **Interacting with the Service** section varies only by protocol (REST vs. gRPC).
  Use the capability flags to include/exclude snippets.
- The **Project-Specific Guidelines** section is bespoke. The template leaves a TODO
  comment listing common sub-sections (domain concepts, lifecycle, config, etc.) — the
  user fills these in based on the project. Do not invent content.

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
