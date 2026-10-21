---
name: prefer-small-solutions
description: >
  Choose the smallest complete solution for all code planning, implementation, changes, and review.
  Simplify the affected path while preserving architecture, contracts, and meaningful tests.
---

# Prefer small solutions

Choose the shortest clear, idiomatic implementation that fully meets the required behavior and
project constraints. Judge the whole affected solution: callers, adapters, state, configuration,
tests, dependency glue, and operations. Measure size in the distinct instructions a reader holds at
once, not in lines. A repetitive structure, a table read at a glance, or an established pattern costs
little even when long. A short function chaining many different operations costs much. A
simplification wins by removing instructions. Moving complexity to another file, compressing layout,
merging unrelated logic, or omitting a requirement is not a reduction.

For development, load [develop-feature](../develop-feature/SKILL.md) for stage timing. This skill
chooses among solutions allowed by the applicable architecture, language, dependency, documentation,
and testing skills. Required interfaces, contracts, security, error handling, observability,
accessibility, compatibility, and performance remain binding.

## Establish the contract first

Read the task, affected implementation, and relevant tests. Identify required behavior, failure
cases, ownership boundaries, and the project's supported versions before choosing a shortcut.
Trace the affected path through callers, data ownership, transformations, and outputs for every
change, not only bug fixes. Keep this brief for a small edit; use the planning workflow when the decision warrants it.

For a bug, search callers of the changed operation and trace the failing value or state to its
owner. Check sibling paths that rely on the same invariant. Repair the owning boundary when the
invariant is shared; do not move caller-specific policy into a shared helper merely to save lines.
A regression check should expose the original failure and relevant sibling behavior.

## Choose the first complete path

Walk this ladder in order. An option qualifies only if it meets the same contract and project
constraints; stop when further alternatives would not affect the decision.

1. **Remove unnecessary work.** Can a derived value, stronger invariant, or existing operation
   eliminate the proposed state, transformation, round trip, or branch? Omit speculative extension
   points. Preserve explicitly requested behavior; propose a scope change instead of silently
   shipping a smaller interpretation.
2. **Reuse the established capability.** Search the codebase, shared packages, and current
   framework/dependencies. Prefer the project abstraction when it owns a contract or policy that
   a direct platform call would bypass.
3. **Use standard or native behavior.** Check the language, standard library, framework, and
   supported platform before writing custom machinery. Verify unfamiliar APIs against supported
   versions. Compare semantics, not just the happy-path output.
4. **Resolve a real dependency decision.** Follow
   [choose-dependency](../choose-dependency/SKILL.md) when adding/replacing a package or weighing
   build versus buy. A tiny stable helper may belong locally; a mature subsystem may justify a
   vetted library. Include integration, operational, and ongoing maintenance costs.
5. **Write the direct implementation.** Use native data structures, expressive types, and idiomatic
   control flow. Add a helper, wrapper, interface, or abstraction when it owns an invariant, removes
   meaningful repetition, or serves a required boundary. Local repetition can beat an abstraction
   joining unrelated cases. A single implementation is not evidence that an interface is unnecessary.

When two paths qualify, prefer the least total maintenance burden. Examine another option only when
it could change that choice; routine edits need no extra plan, approval, or narrated ladder.

## Shape the code for a scanning reader

The reader holds little context but plans well. They scan blocks and known structures, then dig
into the one place they came for. Shape every change for that path. These rules are soft: behavior,
security, and performance win every conflict.

- **Count instructions, not lines.** A plain error check, a short pipe chain, or one call taking an
  object literal is one instruction, even across several lines. Custom error handling, a flag
  threaded through branches, or three unrelated statements packed together are several.
- **Reuse known structures.** A table, a table-driven test, or a layer in the codebase's established
  shape reads at a glance, even when long. Follow the existing shape before inventing one.
- **Separate small blocks with blank lines.** Give each block one job. The blank lines let the eye
  jump between blocks, so a reader scans dozens of lines and stops only where needed.
- **Keep indentation shallow.** Each level makes the reader track back to the enclosing loop or
  branch. Use guard clauses, an early `return` or `continue`, and a named function for a long loop
  body.
- **Push required complexity down.** An entry point reads as an overview of named steps. The tricky
  algorithm, the edge cases, and the optimization live in a leaf function the reader opens only when
  needed. Moving complexity down does not reduce it; it clears the reader's main path.

## Keep simple code fast

Short code can hide a cost that grows with its input. Check these patterns whenever input size is
not fixed by our own code, in new code and in code you touch. Fix a trap that costs something on
realistic input. Leave an unmeasured micro-gain alone when it costs readability. The language skills
add their own traps: [write-go](../write-go/SKILL.md#performance-traps),
[write-frontend](../write-frontend/SKILL.md#performance-and-compatibility),
[write-svelte](../write-svelte/SKILL.md#svelte-5-reactivity),
[write-sql](../write-sql/SKILL.md#bounded-reads), and the pitfall lists of
[write-bash-scripts](../write-bash-scripts/SKILL.md#common-pitfalls) and
[write-github-actions](../write-github-actions/SKILL.md#common-pitfalls).

- **Recursion.** Each call adds a stack frame, and neither Go nor JavaScript engines remove tail
  calls. Depth that comes from input can exhaust the stack: nested JSON, a file tree, a user-built
  outline, a dependency graph. Recursive closures also keep every enclosing scope alive until the
  deepest call returns. Write a loop over an explicit stack (depth-first) or queue (breadth-first);
  it costs one slice and reads as plainly. Keep recursion only when our own code bounds the depth to
  a small number.
- **Loop-invariant work inside a loop.** Compiling a regexp or template, building a formatter,
  client, or schema, and parsing a constant belong before the loop, or at package or module level.
- **A lookup inside a loop.** `slices.Contains`, `includes`, `find`, or `indexOf` over a second
  collection makes the loop O(n·m). Build a map or `Set` once, before the loop.
- **A copy inside a loop.** Spreading an accumulator (`{ ...acc, [key]: value }`) or appending to a
  Go string with `+=` rebuilds the whole value on every turn, which is O(n²). Mutate a local
  accumulator or use a builder.
- **A round trip per item.** One query, API call, or command per element is the N+1 pattern. Batch
  the call, join the query, or fetch the list once.
- **Serial waits on independent work.** Awaiting independent calls one by one adds up their
  latencies. Run them concurrently with a bound when ordering and rate limits allow.
- **Unbounded growth.** A cache, map, buffer, slice, or queue fed by requests or input needs a size
  or age bound. A multi-row query needs a `LIMIT`. A read of an untrusted body needs a size limit.
- **Work that outlives its owner.** A goroutine, timer, ticker, listener, observer, interval, or
  subscription needs an exit tied to its owner's lifetime. Server module state shared by every
  request must not grow per request.

## Prove the simplification

Before deleting or replacing logic, establish which invariant or existing capability makes it
redundant. Preserve evaluation order, mutation, absence semantics, errors, resource use, and public
behavior. Retain validation at trust boundaries. Keep one authoritative representation of each
fact where the contract permits it.

A native control still has to satisfy the agreed interaction, browser, accessibility, localization,
and design-system contract. A database constraint can enforce an invariant without replacing
authorization or the application's public error contract. A shorter cache primitive still needs
the required expiry, invalidation, and concurrency behavior.

Retain tests that demonstrate behavior and catch regressions. Use focused cases and existing
fixtures. Reduce scaffolding only while preserving meaningful assertions and required coverage;
never weaken a test to make a shorter implementation pass. Test depth follows risk and contracts,
not the number of production lines.

When a deliberate simplification has a material ceiling, state the assumption, consequence, and
observable trigger for revisiting it in the existing plan or the nearest useful code comment.
Use the project's documentation conventions; do not create a separate shortcut ledger or speculative
upgrade framework. Record consequential tradeoffs, not a justification for every ordinary choice.

## Finish with a subtraction pass

Review the complete diff once. Can an added step, stored value, conversion, helper, dependency, or
fixture disappear without losing behavior or clarity? Remove obsolete code made unnecessary by
this change. Keep cleanup in scope, run the affected checks, and stop when further reduction would
obscure intent, violate a constraint, or expand the task.

Explain retained complexity only when the tradeoff matters to review. Do not score success by lines
or infer savings against an implementation that was never built.

## Maintaining this guidance

When changing this skill or its loading routes, use
[the evaluation scenarios](references/evaluation.md) to check behavior and policy preservation.
The ladder and operational checks draw on
[Ponytail](references/ponytail.md); the workspace's existing decisions govern their application.
