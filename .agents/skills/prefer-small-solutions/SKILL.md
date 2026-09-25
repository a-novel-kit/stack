---
name: prefer-small-solutions
description: >
  Choose the smallest complete implementation when planning, writing, changing, or reviewing code
  in any language. Reduce maintained code through whole-path design, native capabilities, and reuse
  while preserving project architecture, dependency policy, and meaningful tests. Apply to all coding work.
---

# Prefer small solutions

Choose the shortest clear, idiomatic implementation that fully meets the required behavior and
project constraints. Judge size across the affected solution, including callers, adapters, state,
configuration, tests, and dependency glue. A short function that moves complexity elsewhere is no
improvement. Count lines after normal formatting; readability and correctness constrain the choice.

## Find the shorter path before writing

- Identify the required behavior, failure cases, and architectural boundaries. For a small edit,
  keep this reasoning brief; follow the project's planning workflow when the change warrants it.
- Trace the affected path through callers, data ownership, transformations, and outputs. Read the
  relevant implementation and tests; search for existing solutions before inventing another.
- Ask what can disappear: an intermediate representation, round trip, duplicated source of truth,
  stored value that can be derived, or branch made unnecessary by a stronger invariant. Prefer the
  design that removes work across the path. Keep changes within the task's scope.
- Check the language, standard library, framework, and installed dependencies for capabilities that
  replace custom logic. Verify unfamiliar APIs against the project's supported versions.
- When alternatives matter, compare the direct implementation with the best reuse or design
  shortcut. Choose the one with the least total code and maintenance burden that satisfies the same
  contract. Record a consequential tradeoff in the existing plan or review; routine edits need no
  extra document or approval step.

## Make each piece earn its place

- Use native data structures, expressive types, standard operations, and framework conventions to
  make invalid states or repeated bookkeeping unnecessary. Keep evaluation order, mutation,
  absence semantics, error behavior, and resource use correct.
- Reuse a suitable existing capability directly. Add a helper, wrapper, interface, or abstraction
  when it owns a real invariant, removes meaningful repetition, or serves a required project
  boundary. Avoid speculative extension points, pass-through layers, and one-use scaffolding.
  A little local repetition can be simpler than a configurable abstraction joining unrelated cases.
- Keep one authoritative representation of each fact where the contract permits it. Remove dead
  branches, redundant conversions, and obsolete helpers made unnecessary by this change. Establish
  why a check is redundant before deleting it; retain validation at trust boundaries.
- Follow the project's dependency-selection policy. Consider native and existing capabilities
  first; use a vetted library for a solved, substantial problem when justified. Include dependency,
  integration, and operational costs in the comparison. Saving a few lines alone does not justify
  a new package, and avoiding a dependency does not justify rebuilding a mature subsystem.

## Preserve the principles

The project's structure, required interfaces, public contracts, security, error handling,
observability, compatibility, and performance constraints remain binding. Retain purposeful
ceremony even when it costs lines. Follow the applicable language, architecture, dependency,
documentation, and testing skills; this skill chooses among solutions those rules permit.

Keep tests that demonstrate behavior and catch regressions. Prefer focused cases and existing
fixtures over duplicated setup or a new test framework. Reduce test scaffolding only while
preserving meaningful assertions and required coverage. Never weaken a test to make a shorter
implementation pass.

## Finish with a subtraction pass

Review the complete diff once: can an added step, state value, conversion, helper, dependency, or
test fixture be removed without losing required behavior or clarity? Simplify justified cases,
then run the applicable checks. Stop when further reduction would obscure intent, violate a
constraint, or expand scope. Explain retained complexity only when the tradeoff matters to review.
