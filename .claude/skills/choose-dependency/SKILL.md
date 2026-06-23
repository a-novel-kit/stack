---
name: choose-dependency
description: >
  Decide whether to solve a need with the standard library, an existing dependency, a new
  third-party library, or an internal implementation — and if importing, which package to pick. Use
  it whenever a plan or change is about to add a library, weighs build-vs-buy, considers replacing an
  internal helper with a dependency (or vice versa), or must choose between competing packages. It
  encodes the a-novel balance: minimize non-standard dependencies, but don't reinvent solved
  problems; when you do import, prefer one comprehensive, well-maintained library from a trusted org
  or community over several narrow ones, so the codebase can consolidate and stay on it. Requires
  internet research from trusted sources to evaluate candidates. Pairs with plan-feature (feeds the
  build-vs-buy section), write-go / write-go-kit (the dependency policy), and manage-versions (when
  the dependency is internal).
---

# Choosing a dependency

Every dependency is a trade. This skill exists because the obvious heuristics pull in opposite
directions and you have to balance them deliberately, not reflexively.

- **Force A — fewer dependencies is better.** Each third-party library is attack surface, a
  supply-chain risk, transitive bloat, a thing that can break or go unmaintained, and a version you
  must track. The standard library is always the first choice; a pile of micro-dependencies is a
  smell.
- **Force B — don't reinvent the wheel.** A library delegates a whole problem's maintenance to its
  owner, so you can focus on the app. Hand-rolled helpers for solved, non-trivial problems are
  _our_ bug surface and _our_ maintenance burden forever. So we generally **prefer a good external
  library to an internal reimplementation** of something already well-solved.

These resolve into one rule:

> **Import rarely; when you do, pick the broad, trusted, well-maintained option and consolidate on
> it.** A batteries-included library from a reputable source that covers a whole domain beats a thin
> wrapper that you'll have to supplement with three more libraries later. Fewer, better, broader
> dependencies — not more, smaller ones.

**Illustration.** Prefer an ORM like `bun` (struct decoding, query building, migrations, hooks all
native, one maintainer) over a lower-level driver like `pgx` that needs extra third-party packages
bolted on for the same ergonomics. One broad, well-maintained dependency replaces several narrow
ones — that is the consolidation you're optimizing for. (This is the _reasoning pattern_; pick per
the actual need, not by analogy.)

---

## The decision procedure

Work top to bottom; stop at the first answer that fits.

1. **Is it already solved in-house?** Standard library, a dependency already in `go.mod` /
   `package.json`, or `golib` / `nodelib`? Use that. Adding a second library to do what an existing
   one already does is the most common avoidable dependency.

2. **Is it trivial and stable?** A few lines, well-understood, unlikely to change (a tiny string
   helper, a constant, a one-off transform)? Implement it internally — pulling a dependency for it
   is not worth the supply-chain and version cost. (For Go libraries, also weigh `write-go-kit`'s
   "should this even live in `golib`?" bar.)

3. **Is it a solved, non-trivial domain?** Parsing, crypto, ORM/SQL, HTTP routing, validation,
   serialization, UUID, time, retries, observability, etc. — these are where you **buy, not build**.
   Reimplementing them is how subtle bugs and security holes get in. Go to candidate evaluation.

4. **Is it genuinely novel to our domain?** No good library exists, or every candidate fails
   evaluation? Implement internally — and design it so it could graduate into a library later
   (`write-go-kit`).

---

## Evaluating candidates (when buying)

Research each serious candidate against these, and write the comparison into the plan's build-vs-buy
section. The first three are gating; the rest are tie-breakers.

- **Trust** — who owns it? A reputable org, a foundation, or a healthy community beats a single
  unknown maintainer. Trust is what makes "delegate the maintenance" safe.
- **Maintenance & health** — recent commits and releases, issues triaged, not archived, a real
  changelog. An abandoned library is an internal implementation you don't control.
- **Security** — known CVEs / advisories, the size and trustworthiness of the **transitive**
  dependency tree (a "small" library that drags in 40 packages isn't small), and supply-chain
  hygiene (signed releases, pinned CI).
- **Coverage** — does it cover enough of the domain that you _won't_ need to add more libraries
  alongside it? This is the consolidation lever; weight it heavily.
- **License** — compatible with the repo's license and distribution. A blocker if not.
- **Footprint & fit** — binary/bundle size, API ergonomics that match our patterns, and how cleanly
  it isolates behind our own abstractions if we ever need to swap it.
- **Adoption** — popularity is corroborating evidence of trust and longevity, not a goal in itself.

---

## Research method — use the internet, from trusted sources

Do not decide from memory. Search the web and read **primary, trustworthy** sources:

- The library's **official docs and repository** (README, changelog, release cadence, open issues).
- The package registry — `pkg.go.dev` (Go), `npmjs.com` (JS) — for versions, dependents, and the
  transitive tree.
- **Security advisories** — GitHub Advisory Database, the Go vulnerability database, npm audit data.
- Reputable comparisons and the broader community's experience — weighted below primary sources, and
  always sanity-checked against the repo itself.

Prefer recent, primary information over old blog posts, and **note your sources** in the plan so the
human can verify the call.

---

## Org policy hooks

- **No `replace` directives; exact version pins.** Go deps are pinned to released tags and bumped by
  Renovate — see `manage-versions`.
- **Internal (a-novel / a-novel-kit) dependencies** are a `manage-versions` concern: SHA-pin while
  developing, dependency releases first, consumer re-pins to the tag before merging.
- **Tooling dependencies** (generators, linters, test tools) stay isolated in their own
  `<tool>.mod` / `.sum` files, never in the main `go.mod`, and are never `go install`-ed globally —
  see `write-go`'s tools policy.
- **Kit repos hold a stricter bar** than services for taking on any dependency (`write-go-kit`).

---

## Output

The deliverable is a short, justified recommendation that drops into the plan's build-vs-buy
section: the need, the options weighed (stdlib / existing / a named library / internal), the
evaluation evidence with sources, and the call with its reasoning. If the answer is "build
internally," say why every buy option fell short; if "buy," say why this library and not the
narrower alternatives.

---

## Examples

**Buy, and consolidate.** Need: talk to Postgres with struct mapping, query building, and
migrations. Options: a low-level driver plus separate mapping/migration libraries, vs one
batteries-included ORM from a trusted maintainer. Choose the broad ORM — one well-maintained
dependency instead of three narrow ones, fewer versions to track, one place to learn.

**Build.** Need: a 15-line helper that formats an internal ID into a display string, specific to our
domain. No external library matches without contortions, the logic is trivial and stable. Implement
it internally — a dependency here would add supply-chain risk for nothing.

**Reuse.** Need: structured logging in a new service. `golib` already exposes the org's logging
setup. Use it — adding a different logging library would fragment the codebase and duplicate a solved
concern.
