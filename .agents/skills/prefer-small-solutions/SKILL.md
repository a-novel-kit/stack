---
name: prefer-small-solutions
description: >
  Choose the smallest complete solution for all code planning, implementation, changes, and review.
  Simplify the affected path while preserving architecture, contracts, and meaningful tests.
---

# Prefer small solutions

Choose the shortest clear, idiomatic implementation that fully meets the required behavior and
project constraints. Judge the whole affected solution: callers, adapters, state, configuration,
tests, dependency glue, and operations. Count after normal formatting. Moving complexity to another
file, compressing layout, or omitting a requirement is not a reduction.

For development, load [develop-feature](../develop-feature/SKILL.md) for stage timing. This skill
chooses among solutions allowed by the applicable architecture, language, dependency, documentation,
and testing skills. Required interfaces, contracts, security, error handling, observability,
accessibility, compatibility, and performance remain binding.

## Establish the contract first

Read the task, affected implementation, and relevant tests. Identify required behavior, failure
cases, ownership boundaries, and the project's supported versions before choosing a shortcut.
Keep this brief for a small edit; use the planning workflow when the decision warrants it.

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

## Prove the simplification

Before deleting or replacing logic, establish which invariant or existing capability makes it
redundant. Preserve evaluation order, mutation, absence semantics, errors, resource use, and public
behavior. Keep one authoritative representation of each fact where the contract permits it.

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
alone or infer savings against an implementation that was never built.

## Maintaining this guidance

When changing this skill or its loading routes, use
[the evaluation scenarios](references/evaluation.md) to check behavior and policy preservation.
The ladder and operational checks draw on
[Ponytail](references/ponytail.md); the workspace's existing decisions govern their application.
