# Project documentation editorial principles

Read this reference when routed here by [write-project-docs](../SKILL.md). Its rules apply to the selected work.

Contents:

- [Editorial Principles](#editorial-principles)
- [1. Audience-first — name the reader before writing the section](#1-audience-first--name-the-reader-before-writing-the-section)
- [2. Lead with the role, not the runbook](#2-lead-with-the-role-not-the-runbook)
- [3. Open with what the document is about, not with a mechanism](#3-open-with-what-the-document-is-about-not-with-a-mechanism)
- [4. Verify every factual claim against the source](#4-verify-every-factual-claim-against-the-source)
- [5. Show the canonical, link or table the variants](#5-show-the-canonical-link-or-table-the-variants)
- [6. Reference, don't enumerate](#6-reference-dont-enumerate)
- [7. Edit in place; preserve unknown content](#7-edit-in-place-preserve-unknown-content)
- [8. Lead with rationale; dose examples to disambiguate](#8-lead-with-rationale-dose-examples-to-disambiguate)
- [9. Plain language, plain sentences](#9-plain-language-plain-sentences)
- [10. Show the flow, don't announce it](#10-show-the-flow-dont-announce-it)
- [11. Keep it self-contained, no links to ephemeral work items](#11-keep-it-self-contained-no-links-to-ephemeral-work-items)
- [12. Tell a story, not a spec](#12-tell-a-story-not-a-spec)
- [13. Name a concept once, concretely, and keep the name](#13-name-a-concept-once-concretely-and-keep-the-name)
- [14. Start simple: the global picture first, then the details](#14-start-simple-the-global-picture-first-then-the-details)
- [15. A link rides on the prose, never the other way around](#15-a-link-rides-on-the-prose-never-the-other-way-around)
- [16. Lead with the do; keep the don't a footnote](#16-lead-with-the-do-keep-the-dont-a-footnote)
- [17. Make every reference land on something the reader already holds](#17-make-every-reference-land-on-something-the-reader-already-holds)
- [18. State what is, not what is not](#18-state-what-is-not-what-is-not)
- [19. State each thing once; duplicate only to go deeper](#19-state-each-thing-once-duplicate-only-to-go-deeper)

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
inversion, or a cleft makes the reader unpack a sentence before reading it. Load and apply that section
here, including its anti-tautology check for headings, prose, and examples.

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
