---
name: write-project-docs
description: >
  Create or update README.md, SECURITY.md, and CONTRIBUTING.md in either organization. Route by
  document and edit scope; source comments use document-code; excludes CODE_OF_CONDUCT.md.
---

# Project Docs

This skill governs the three root-level Markdown files that describe a project to external
readers: `README.md`, `SECURITY.md`, `CONTRIBUTING.md`. They are the first thing visitors see,
setting expectations for usage, security contact, and how to contribute, so they must be accurate,
scannable, and consistent across all Agora services.

They carry the **global picture** — the architecture, the layer split, the principles behind the
shape of the system — which code comments defer to instead of re-explaining the system from inside
one source file (see `document-code`). Keep that wide-angle view current, so the comments in the code
can stay local and specific.

`README.md` is a reference **entrypoint**: how to install, configure, and call the project, plus that
global picture. Closer to an extension of the code comments than to a guide, it follows the section
order below rather than a narrative. `CONTRIBUTING.md` and `SECURITY.md` are **guides**: they walk a
reader through a process, as do the pages they link (onboarding, a board-lifecycle walkthrough).

The skill has two modes, detected in Phase 2: **scaffold** generates a missing file from the
templates here, **update** edits the relevant section of an existing one in place.

Separate concerns:

- `CODE_OF_CONDUCT.md` — **not managed by this skill.** Copy the Contributor Covenant
  verbatim from <https://www.contributor-covenant.org/version/2/1/code_of_conduct.md>
  when setting up a new project and do not edit further.
- `document-code` — governs doc comments inside source files (Go, SQL, TS, etc.), not these
  project-level Markdown files. Its **Prose economy** section does reach here: it owns
  sentence-level prose craft on every surface we write, README sections included. Load it alongside
  this skill and treat it as the base layer the Editorial Principles below build on.

---

## Choose the edit path

1. Identify the document, reader, and requested change. Read the existing file and verify facts
   against the implementation before choosing wording.
2. If the file exists, use update mode below and preserve unrelated content. If it is missing,
   scaffold from [templates](references/templates.md).
3. For every edit, read the applicable rules in [fleet structure](references/structure.md) and
   [editorial principles](references/editorial.md). Fleet structure wins where older templates
   disagree. Read sections for the document and concern being changed.
4. Before collecting or substituting metadata, read [inputs and missing values](references/inputs.md).
   Reuse verified inputs and session decisions; ask only for required information still missing.
5. For badges or coverage graphs, read [badge patterns](references/badges.md). Load templates only
   for scaffolding or adding a missing standard section.

The existing phases and numbered principles are in these references. Their requirements remain
binding for the selected path; moving the material does not change the fleet standard.

## Phase 2: Detect Scaffold vs. Update

```bash
ls README.md SECURITY.md CONTRIBUTING.md 2>/dev/null
```

- File missing → scaffold mode: generate from template in Phase 4
- File present → update mode: read it first, edit the relevant section only (Phase 5)

Never overwrite an existing file with the full template — see Principle 7.

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

- **Preserve unknown content.** Sections you don't recognize (custom architecture notes,
  team-specific tips) stay untouched — Principle 7.
- **One logical edit per call.** When adding a new env var, update only that table; do not
  also "touch up" unrelated sections while there.
- **Check for stale cross-references.** Adding a new gRPC service to the CONTRIBUTING
  table means the README's service list (if any) needs the same row.
- **Version bumps touch every occurrence.** A release bump affects every compose YAML in
  the README — use `Edit` with `replace_all` only when you have verified the old string is
  unique to the version (e.g., `:v2.2.6`), otherwise do it one-by-one.

---

## Portability to New Projects

Maintain the shared skill source in stack's `.agents/skills/` and use the workspace's existing
discovery links. Make cross-project corrections in stack through a feature branch and PR.
Do not fork the rules into personal installations or a separate per-service copy. Adapt generated
project documentation to verified project capabilities while retaining the fleet standard.

---

## What NOT to Do

- **Do not edit `CODE_OF_CONDUCT.md`.** It's the Contributor Covenant verbatim. Changes to
  it are org-wide policy, not per-repo.
- **Do not add "Live Demo" / "Screenshots" / "Roadmap" sections** unless the user asks.
  They become stale fast and aren't in the Agora template.
- **Do not write long prose.** README and CONTRIBUTING are reference documents; tables, bullet
  lists, and runnable code blocks beat paragraphs. See Fleet-standard hard rule 4.
- **Do not put case-specific examples in generic prose.** A particular provider, key, or
  value dates fast and rarely adds understanding. See Editorial Principle 8.
- **Do not embed secrets.** The codecov graph token is fine (public). API keys, passwords,
  real `APP_MASTER_KEY` values, npm auth tokens are not.
- **Do not link to internal-only dashboards.** Anything linked in the README is
  publicly visible once the repo is public.
- **Do not invent version numbers or email addresses** to fill placeholders. Use the TODO
  comment pattern (Phase 3).
- **Do not describe code from memory.** Every factual claim about the codebase gets verified
  against the source at the same SHA as the doc commit. See Editorial Principle 4.
- **Do not duplicate content across README and CONTRIBUTING.** Common offenders: client install
  snippets, env-var tables, service role description. Pick one home; link from the other. See
  Editorial Principle 1.

---

## Quick Reference

| Situation                                    | Skill phase                                                  |
| -------------------------------------------- | ------------------------------------------------------------ |
| New project, all docs missing                | Phase 1 (collect inputs) → Phase 4 (scaffold all three)      |
| "Add env var X to README"                    | Phase 5 (update mode, edit the config table)                 |
| "Update security contact email"              | Phase 5 (edit SECURITY.md only)                              |
| "Docs for a project split off from monorepo" | Phase 1 → Phase 4, then port custom sections from the parent |
| "Port these skills to new-repo"              | [Portability to New Projects](#portability-to-new-projects)  |
| Required value unavailable                   | [Handling Missing Values](references/inputs.md#phase-3-handling-missing-values)  |
