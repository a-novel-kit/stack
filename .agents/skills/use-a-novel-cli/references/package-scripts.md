# Package script ownership and naming

Read this reference when routed here by [use-a-novel-cli](../SKILL.md). Its rules apply to the selected work.

## pnpm scripts vs. the CLI — the boundary

When you touch a repo's `package.json` scripts (or review a PR that does), apply one rule:

> A pnpm script earns its place only when it carries something **specific to
> the repo** — a local package, a config file, a fixed argument set, or a hook
> the CLI itself invokes. A script that merely **mirrors a CLI capability** is
> indirection and must be deleted; run the CLI directly instead.

- **Delete** (pure mirrors): `publish:major|minor|patch` — releases are cut in
  CI by the release workflow (the `release-core` action), never a pnpm script or
  a local command. These wrappers added nothing and drifted; delete them.
- **Keep** (repo-specific constructs the CLI discovers or invokes):
  - `test` (`vitest run …`), `build:rest` (`vite build …`) — the concrete
    invocations `a-novel test` / `a-novel build` discover and run.
  - `lint:go` / `lint:proto` / `format:go` / `format:proto` / `generate:go` —
    lint/format/generate have no CLI verb by design (see below); these are
    their canonical home.
  - `prepublish:doc` and its `prepublish:doc:readme` / `:openapi` children —
    the release flow (`release-core`) runs `prepublish:doc` as a hook, and the
    children carry this repo's stamp prefix + file (`a-novel publish stamp
'<prefix>' <file>`). Those repo-specific args justify the script.

The smell test for a new/edited script: _strip the repo-specific part — if
what's left is just an `a-novel <verb>` call, the script shouldn't exist._

### Naming: generic does everything, language lanes are suffixed

A second rule governs how the surviving scripts are **named**:

> A **generic** verb (`format`, `lint`, `build`, `generate`, `test`) must do
> **everything** that verb covers in the repo. A script scoped to one
> language/lane is **suffixed** (`format:go`, `lint:proto`, `format:js`). A
> bare verb that silently runs only one lane is the bug this rule forbids.

- **Multi-lane verb → umbrella + suffixes.** A service has Go, Protobuf and a
  JS package, so `format` = `pnpm format:go && pnpm format:proto && pnpm
format:js`, and `lint` likewise. Each lane is a `:`-suffixed script; the bare
  verb chains them. The classic violation: `format` aliased to Prettier only,
  so `pnpm format` leaves Go unformatted and the contributor trips `lint-go` in
  CI.
- **Single-lane verb → stay generic, do NOT suffix.** A pure-JS repo
  (`nodelib`), a Prettier-only repo (`workflows`), or `build`/`test` in a
  service (only a JS pnpm lane — Go is built/tested via `a-novel`) already do
  everything under the bare verb. A redundant `:js`/`:go` alias there is
  overdoing it: the suffix disambiguates **multiple** lanes.
- **Name the lane by what it actually contains.** The Node/Prettier lane is
  `:js` when the repo ships a real JS/TS package (the lane runs eslint + tsc +
  prettier on actual JS). When the lane only runs Prettier over docs/config and
  there is **no JS** (`golib`), name it `:prettier` — `format:js` in a Go-only
  repo is the confusion this rule exists to prevent.
- **CI calls the lane, not the umbrella.** The `lint-node` composite action
  runs on a node-only runner with no Go/buf toolchain, so it must target the
  node lane (`lint:ci` → `lint:js`, or `lint_action: "lint:prettier"`), never
  the bare `lint` umbrella. The per-language CI jobs (`lint-go`, `lint-proto`)
  invoke their tools directly, not through pnpm. When you turn a bare verb into
  a Go-inclusive umbrella, re-point that repo's `lint-node` at the node lane in
  the same change or you red-build CI.
