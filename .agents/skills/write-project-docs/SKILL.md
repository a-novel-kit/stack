---
name: write-project-docs
description: >
  Write and maintain root-level project docs — README.md, SECURITY.md, CONTRIBUTING.md — for any
  a-novel / a-novel-kit repo (services, golib, nodelib, workflows, stack CLI). Use it when
  scaffolding a new project's docs or updating one: new env var, badge, client or Docker section,
  security contact. Not CODE_OF_CONDUCT.md or in-source comments (document-code).
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

---

## Editorial Principles

These come before the templates. The templates implement them; when a generated file looks
right but violates a principle, the principle wins. They build on `document-code`'s **Prose
economy** section, which owns the sentence-level craft for every prose surface — load it too.

All of them apply to every file except the narrative ones, which shape the **guides** only: principle
12 (tell a story), and principle 10's natural headings, give way in a `README.md` to the fixed section
order above — though principle 10's show-over-tell holds anywhere.

### 1. Audience-first — name the reader before writing the section

Every section in `README.md` and `CONTRIBUTING.md` answers a question a specific reader is holding.
Three readers exist:

| Reader                | What they want                                     | File              |
| --------------------- | -------------------------------------------------- | ----------------- |
| **Operator**          | "How do I run this service?"                       | `README.md`       |
| **Client integrator** | "How do I call this service from another service?" | `README.md`       |
| **Contributor**       | "How do I work on this codebase locally?"          | `CONTRIBUTING.md` |

A section that answers none of those questions does not belong. A question answered twice for the
same reader — across the two files or twice in one — is cut to one home and linked (principle 19): if
the JS client install snippet is in `README.md`, `CONTRIBUTING.md` says "see the README".

Write at the reader's knowledge, not yours; the curse of knowledge is the default failure. A term you
use daily reads as jargon to a first-time contributor — a `Closes` line, an issue number, a Pull
Request description. When a passage only parses because _you_ already know the tool, define what you
assumed, on first use.

### 2. Lead with the role, not the runbook

The first text after the badges in `README.md` is **what the service does and why it exists** — one
to three short paragraphs. Not the stack it is built on, not how to deploy it, not a table of
contents. A reader who cannot tell what the service does from the first paragraph will not find out
by scrolling further.

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
what the document is about, in plain purpose terms: "This document is about how a feature is planned,
built, and shipped." Never open on an implementation fact — "Every piece of work is a GitHub issue"
drops the reader into the mechanism with no frame. State the purpose first and the mechanism arrives
as its answer.

Guide, don't assert: "This document is about…" orients the reader, while "We plan before we build"
commands them, and prescriptive first-person openers read as manifesto. Test the first two sentences
alone — a stranger should know what the doc covers and why, and should not yet have met an
implementation term without a reason for it.

### 4. Verify every factual claim against the source

A README that says "AES-GCM" when the code uses NaCl secretbox is worse than one that says nothing
about encryption. Before describing any of:

- Cryptographic primitives (algorithm names, modes, key sizes)
- Configuration field names and shapes (YAML keys, env var names)
- API surface (RPC names, REST paths, status codes)
- Lifecycle states (active / expired / deleted)
- What a named thing _is_ in the platform (a GitHub Milestone is a grouping feature, not an issue type;
  a label is not a status)
- File paths referenced in prose

…open the source and confirm. The doc commit must reflect the code at the same SHA; when the code
changes one of these things, the doc update belongs in the same change, not a follow-up.

A process still taking shape is not a fact yet: state what the system does today, not the policy you
expect it to grow into ("an Epic ships one per release" is fiction until releases work that way). An
option the platform merely offers is not taxonomy either — an enabled type carrying zero issues is
configured, not real, so check usage, not just config. Name the condition on a conditional rule ("only
for Epics under one Initiative or Milestone"), or it reads as universal. Mark a workaround a current
limitation forces as temporary ("for now, a Milestone is tied to one repository"): unmarked, it hardens
into apparent design and no one revisits it when the limitation lifts.

### 5. Show the canonical, link or table the variants

When a service has several deployment shapes (REST × gRPC × standalone × split = four combinations),
do not paste four near-identical compose blocks in sequence: the reader who wants the simplest path
scans past three they will not use, and any future update becomes a four-place edit. Show one
canonical block inline — the **production / expected shape**, per the Fleet standard above (lead with
production, relegate the dev one-liner to "Running locally") — then list the other shapes in a table or
collapse them under a `<details>` block. Any time two blocks differ by one line, the second belongs in
a diff, table, or collapsible block, not in line.

The same holds for any set of parallel items, a catalog of types or a matrix of options: in a reference
or in-depth section, a table reads faster than a run of paragraphs, while the narrative up front stays
prose. Go harder on presentation the deeper into the document you are.

### 6. Reference, don't enumerate

Comprehensive lists of fields, methods, or env vars are reference material. They go in a dedicated
section (or in generated reference docs like the OpenAPI viewer or godoc), **after** the canonical
example, never interleaved with prose: readers who need the reference jump to it, readers who don't
are not made to scroll past it.

For client packages, the README example shows the **minimum viable call** — install, construct, one
real operation. That is enough to unblock someone; the full surface is what intellisense,
`pkg.go.dev`, or the published API reference is for.

### 7. Edit in place; preserve unknown content

In update mode, treat existing custom sections (architecture diagrams, team notes, org-specific
footers, release call-outs that are not in the template) as data, not noise: read the whole file, then
edit only the section the user is changing. A "rewrite" instruction from the user is the only override, and even then, surface
anything that looks like deliberate custom content before discarding it.

### 8. Lead with rationale; dose examples to disambiguate

A doc earns its keep by explaining what a thing is, why it exists, and how to approach it. That
rationale leads and carries the weight; an example never stands in for it, because one left to do the
explaining goes stale and breaks the rhythm. **Never import an example from the conversation that
produced the doc** — it aided the author, not the reader, and lands as arbitrary and dated later.

Once the rationale is on the page, a short example pins down what prose leaves fuzzy: an inline pairing
("named for its goal, not its version"), a compact do / don't table, or a code block. Dose them —
short, few, and only where the explanation needs it, judging per point. Across a do / don't series,
thread one running example through every row so the reader tracks a single thing and the rows can
cross-reference, and pair every don't with why it is wrong, or it is a second example rather than a
lesson.

An example must instantiate the rule it illustrates, not a cousin of it: explaining an Epic's atomic
landing (its Tasks merging as a unit) with a cross-repo dependency — really the stages rule, one piece
released before another can use it — teaches a false model and braids two rules under one heading. An
example that holds only under another rule belongs under that rule, and the muddle usually signals the
section should split in two.

Concrete specifics that run long (exact commands, names, links) are reference material, not
explanation: keep the prose general and push them where reference belongs — a table, a code block, or
the relevant service's own `CONTRIBUTING.md`. An analogy is its own kind of example: reach for it when
the concept is genuinely hard, and state the concept directly otherwise.

### 9. Plain language, plain sentences

Write so a tired reader gets it on the first pass. `document-code`'s **Prose economy** section states
this rule in full and covers these files: prefer the common, short word and reach for a technical one
only when it earns its place; state each point as a plain subject-verb-object sentence rather than a
rhetorical label ("Why this matters:", "Note:"); keep the plain word order, since a fronted object, an
inversion, or a cleft makes the reader unpack a sentence before reading it. Load it and apply it here.

What project docs add: two short sentences beat one long one spliced together. Avoid the em-dash that
cuts a sentence in half, and the enumeration that only restates what you just wrote. Get the
conjunction comma right — put one before and, but, or so only when a full clause with its own subject
follows ("the board runs itself, and you barely notice it"), never before a compound predicate sharing
the subject ("easier to write and far easier to review"); "so" meaning "therefore" takes the comma, a
restrictive "because" does not. When a draft feels dense, revise for plainness before shipping: the
plain version is the finished one, not a step toward it.

### 10. Show the flow, don't announce it

Let the reader absorb the model by reading, not by being told its shape. A section named "The big
picture" or "The design" announces your structure instead of teaching the subject; drop the label, and
name in the heading what the reader is doing or learning there ("When the board needs you").

A heading must match the scope of what it covers. When a rule recurs at every level of a hierarchy,
state it once as the general rule and title its home for the whole hierarchy: "Planning an issue" fits,
"Planning a task" strands the rule under the smallest case it governs. A heading level is likewise a
claim of sameness, so let the levels mirror the kinds — peers are the same sort of thing, the board's
objects grouped apart from the practices for working with them.

Subheadings help a section the reader scans, not one they read through: a reference block answering
many separate questions reads better broken under headings a reader can jump between, while an argument
or a story only fragments under them. Match the anchor's weight to the need — a heading is heaviest,
for a section the reader jumps around; bold lead-ins or a distinctive topic sentence per paragraph
carry a lighter scan; a pure read-through needs nothing. Over-anchoring a rarely-visited explanation
costs more than it gives.

Prefer showing to telling: a worked path teaches the model better than a list of definitions (an idea
becomes a Task, the Task becomes a Pull Request, the Pull Request merges and ships). When a sequence of
states is the point, a small text diagram beats a paragraph — visual and sparse, a few words per node,
marking what matters, like who acts or where a gate falls. It also beats a screenshot whenever the
subject changes or sits behind a login (a board, a pipeline, a console): it diffs in review, renders in
the reader's own theme, tracks the concepts rather than the pixels, and keeps private data out of a
public repo, where a snapshot of a live UI rots the moment the UI moves. Keep one diagram per idea,
deleting an earlier one a richer diagram subsumes. A diagram is a claim, so its shorthand must stay
true: a label that sweeps a human gate into "the board does it" is a bug, not a simplification.

### 11. Keep it self-contained, no links to ephemeral work items

Never point a doc at a specific issue, epic, or PR, by number or by link. They close, archive, and
get renumbered, so "see #46" rots and leads nowhere. If a rationale is worth keeping, write it into
the doc itself. Commit messages and PR descriptions may reference issues; docs may not.

### 12. Tell a story, not a spec

A guide is a narrative with an arc, not a catalog of facts. The arc that carries a reader: what this
document is, then what they will do, then how it works, then what to pay attention to, then the edge
cases, then the machinery underneath. Each part earns the next, so the reader is led through, never
dropped into a list.

Pacing does the leading. Set up what is coming, then move through it with real transitions ("It starts
with the intention," "Then comes the code," "Now it is a maintainer's turn"). Let a motif thread
through and pay off later: the point where the work "waits for a human" becomes the very thing to pay
attention to. Vary the sentence length, and land the important beat on a short one.

A story is the order of ideas, not extra words — keep each beat as tight as principle 9 demands ("Task
being the smallest unit" beats "the smallest, the one that turns straight into code"). The chain holds
inside a paragraph too: each sentence follows from the one before. A paragraph that packs several ideas
and leaves the links implicit reads as dense, not concise; make each link explicit, or split it.

The trap is the appliance manual: "There is a wash cycle and a rinse cycle. Press start. If it beeps,
open the door." Flat, listed, no momentum. This principle governs the shape of a guide, and the others
refine the prose inside it; it does not apply to a `README.md`, a reference entrypoint (see the scope
note above).

### 13. Name a concept once, concretely, and keep the name

`document-code`'s **Prose economy** owns this rule and its extensions; apply it as written. One thing
is specific to a guide: a motif (principle 12) may echo a term, never replace it.

### 14. Start simple: the global picture first, then the details

Give the reader the whole shape in its simplest form before any detail: someone new grasps how the
thing works from the opening, then meets the specifics, then the edge cases, each layer resting on the
one before. Do not open a section with an exception or a corner case; open with the common path, and
let the rare and the deep follow. This orders the document as a whole and every section inside it, and
it binds a concept to the rules about it: introduce a type before any rule that leans on it. A crisp
rule ("an Epic lands whole") invites use as an early capstone, but before the reader has met the
concept it rests on nothing, so it belongs no earlier than the section defining the term.

The same instinct decides what a document carries at all. A process or lifecycle doc holds the generic
shape; the language, the stack, and the specific tooling belong in the repository's or language's own
doc, linked to rather than stamped in. Detail only some readers need — a step an agent handles on its
own, an advanced path — gets its own section with a line up front saying who it is for, so the main
narrative stays simple and universal and whoever needs more follows the link.

A concept with behavior of its own — its own lifecycle, flow, or failure modes — earns a section too,
never an aside on a neighbor: "A Bug is like a Task, for a defect," tucked into a table cell,
under-serves a thing that carries its own hotfix path. The aside signals the concept has outgrown its
host.

### 15. A link rides on the prose, never the other way around

Anchor a link to a phrase that already earns its place in the sentence; never write a clause, a
sentence, or a stacked list just to hold one. "Taking time to refine it makes the [whole planning
process](…) smooth" reads as prose; "refine it, and [planning](…) walks through it" reads as a footnote
bolted on. When a phrase needs several URLs at once, such as the same view in two orgs, a short
parenthetical on it carries them ("the Roadmap view ([a-novel](…), [a-novel-kit](…))"). If a link has
nowhere natural to sit, rework the prose or drop the link.

### 16. Lead with the do; keep the don't a footnote

`document-code` sets the default: write the choice, not the rejected alternative, and keep a
counter-example only where the wrong path is the one a reader would otherwise take. When one earns its
place, this principle sets the order and the weight. Lead with the instruction, plain and given the
most room ("make the spec sharp; it is the foundation"), then set the failure it guards against apart
after it and smaller — it is a transient warning, there only to make the instruction land: "a vague one
builds the wrong thing." Do not lead with the failure, weld a caveat onto it, or bury the do at the
tail. "Build from a vague one and you build the wrong thing, however clean the code, so make it sharp"
does all three, hiding the instruction behind the warning meant to drive it home.

### 17. Make every reference land on something the reader already holds

`document-code`'s **Prose economy** owns this: a pronoun sits beside its one possible antecedent, and
earlier content is named by a plain description rather than an abstract handle the reader must decode.

### 18. State what is, not what is not

This is `document-code`'s "write the choice, not the rejected alternative", applied to absent features:
describe what the system has and does, and leave out what it lacks, because "there is no tier between
Task and Epic" or "we do not use a Feature type" summons a thing into the reader's mind for the sole
purpose of denying it. The tell, specific to docs: the absence you feel the urge to explain is usually
one only you can see, because you just removed it or argued it away. Cut it; the plain list of what
exists says all the reader needs.

### 19. State each thing once; duplicate only to go deeper

Give each fact, rule, or mechanism a single home, and refer to it from anywhere else that needs it. The
same explanation in two places is not reinforcement but two copies to keep in sync, and a reader who
meets it twice at the same depth wonders what changed between them. A repeat earns its place only when
the second pass goes materially deeper: the overview names a mechanism, the deep section takes it
apart. When two passages say nearly the same thing at the same altitude, keep the one in its natural
home — the mechanism with the machinery, the practice with the workflow — and cut the other. When a
passage carries a fresh point wrapped around restated material, keep the point and link the rest: a
philosophy recap states the philosophy and points at the mechanism it rests on.

---

## Phase 1: Collect Required Inputs

Before scaffolding a new file, ask the user for the inputs below, in a single message rather than
one question at a time. In **update** mode, ask only for the inputs relevant to the section being
edited.

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

The codecov graph token is **public** — it controls badge/graph rendering only, not repo access,
so committing it is safe (see [Badge Catalog](#badge-catalog)).

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

Never overwrite an existing file with the full template — see Principle 7.

---

## Phase 3: Handling Missing Values

When an input is required but not available, write an HTML TODO comment at the exact
location where the value belongs. Format:

```markdown
<!-- TODO(project-docs): <what is missing> — <where to get it> -->
```

HTML comments do not render in GitHub's Markdown preview, so the file still looks clean to
visitors, while grep finds them instantly:

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

**Do not** invent values. A fake email or a `YOUR_TOKEN_HERE` placeholder that a reader might
mistake for real content is worse than a visible TODO.

---

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

**README structure:** the [Section order](#section-order--one-order-every-repo) table is authoritative
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

## Badge Catalog

Copy these patterns verbatim — the exact URL format matters (shields.io is strict about
path segments). Parameters are marked with `{{…}}`.

| Badge                    | Markdown pattern                                                                                                                                                                                                                     |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Twitter follow           | `[![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/{{twitter-handle}})](https://twitter.com/{{twitter-handle}})`                                                                                                 |
| Discord                  | `[![Discord](https://img.shields.io/discord/{{discord-id}}?logo=discord)](https://discord.gg/{{discord-invite-code}})`                                                                                                               |
| Go version (from go.mod) | `![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/{{repo-path}})`                                                                                                                                         |
| File count               | `![GitHub repo file or directory count](https://img.shields.io/github/directory-file-count/{{repo-path}})`                                                                                                                           |
| Code size                | `![GitHub code size in bytes](https://img.shields.io/github/languages/code-size/{{repo-path}})`                                                                                                                                      |
| CI workflow status       | `![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/{{repo-path}}/{{main-workflow-file}})`                                                                                                      |
| Codecov badge            | Default (no token): `[![codecov](https://codecov.io/gh/{{repo-path}}/graph/badge.svg)](https://codecov.io/gh/{{repo-path}})` — add `?token={{codecov-graph-token}}` to the badge URL only when the repo requires a tokenized variant |
| Codecov sunburst         | Default (no token): `![Coverage graph](https://codecov.io/gh/{{repo-path}}/graphs/sunburst.svg)` — add `?token={{codecov-graph-token}}` to the image URL only when a tokenized variant is required                                   |

**Codecov graph token — not a secret.** It is the public badge token from
`codecov.io/gh/<repo>/settings > Badge`, and committing it is intentional. The private upload
token (used by the CI `report-codecov` job) lives in repo secrets, never here.

---

## Portability to New Projects

This skill lives in this repo's `.agents/skills/write-project-docs/`. To reuse it in a new
Agora service:

1. Copy the directory into the new repo's `.agents/skills/`:

   ```bash
   cp -r /path/to/this-repo/.agents/skills/write-project-docs \
         /path/to/new-repo/.agents/skills/
   ```

   Codex and Copilot scan `.agents/skills` directly, but Claude Code only scans
   `.claude/skills` — so if the new repo has no link there yet, add one:

   ```bash
   ln -s ../.agents/skills /path/to/new-repo/.claude/skills
   ```

2. In the new repo, invoke the skill (Claude picks it up once the file exists). Phase 1
   will collect the new project's inputs.

3. The templates are intentionally Agora-flavoured (`a-novel` verbs, podman,
   `.github/CONTRIBUTING.md`, etc.), which keeps docs consistent across services. When a new
   project legitimately deviates (different tooling, different org), update the templates in
   place rather than forking them.

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
| Required value unavailable                   | [Handling Missing Values](#phase-3-handling-missing-values)  |
