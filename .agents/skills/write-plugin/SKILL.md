---
name: write-plugin
description: >
  Write, change, test, or publish a Narrative Engine plugin, such as the Scenes Composer, in
  a-novel/plugins-narrative-engine: manifests and schemas, migrations, fixtures, messages, the
  Svelte UI and its styles, and the plugin-kit commands. Always load write-frontend and
  write-svelte. Studio's host side uses write-platform.
---

# Plugin Conventions

A plugin gives one kind of document its meaning: the schema of an instance's content and the
interface that edits it. Plugins live in `plugins/<name>/` of `a-novel/plugins-narrative-engine`,
beside the contract (`contract/`, what a host and a plugin share) and the kit (`kit/`: the
`plugin-kit` commands, the test host and `definePlugin`). This skill owns what is specific to a
plugin. Load `write-frontend` and `write-svelte` for every change, `write-design-system` before
choosing a token or a uikit component, `write-frontend-tests` for tests, and
[develop-feature](../develop-feature/SKILL.md) for stage timing. The host's internals belong to
`write-platform`.

The READMEs of `contract/` and `kit/` are the reference for the formats: the `Plugin` and `Host`
types, the manifest, a language file, a migration, and each command's options. Read them in the
repository before writing one. This skill says how we work on plugins and where the traps are.

Use the words of the Initiative's glossary (a-novel/.github#500) in code, messages and comments:
instance, content, version, export, link, identifier, host, registry. One concept, one word.

Read the reference that matches the task before acting:

- [references/changes.md](references/changes.md) before adding a member to a content, adding a
  kind to a tagged union such as a layer kind, or releasing a new version. It walks each case
  through every file it touches.
- [references/scenes-composer.md](references/scenes-composer.md) before changing the Scenes
  Composer: what its schema cannot hold, its links, its presets, and its design rules.

## What a plugin owns

| The host owns                                                      | A plugin owns                                                       |
| ------------------------------------------------------------------ | ------------------------------------------------------------------- |
| Loading a version, its messages, and the translation of its schema | Its schema, its exports and its message files                       |
| Validating the content on load, after an upgrade and before a save | Its UI and every interaction inside it                              |
| Persistence and the save state                                     | Its editing state, undo included                                    |
| The links of the project, the link picker, following a link        | Declaring its exports and choosing the source targets of its links  |
| The upgrade chain, salvage, the restore point, the repair screen   | One migration from the previous version                             |
| The fonts, the canvas and its lamp, on the document                | Its Svelte runtime, its uikit components and the tokens they bundle |
| The shell around the plugin                                        | Its presets and its empty state                                     |

- **A version renders as it was built.** An instance pins a version, and uikit renames tokens
  between releases. So a plugin bundles its own Svelte runtime, uikit components and uikit tokens,
  and reads none of its host's custom properties but the lamp, `--color-lamp`.
- **uikit takes only what completely unrelated interfaces can share.** A field, a grid behavior or a
  header that only makes sense for a plugin's material stays in the plugin's `ui/`. A generic need
  goes to uikit first, as [write-design-system](../write-design-system/SKILL.md#package-boundaries)
  rules, and the plugin bumps uikit once it is released.
- **Everything that crosses the host is plain JSON, copied, and asynchronous**, so the same
  interface can later cross a `postMessage` boundary. The host refuses a Svelte state proxy: keep
  the content in `$state.raw`, or send `$state.snapshot(...)`.
- **`host.content.set` replaces the whole content**, and the host validates it against the schema
  before saving. It can refuse: keep what the writer sees, say the save failed, and offer a retry.
  Run one save at a time: each replaces the whole content, so the last one carries every edit, and
  two in flight could land out of order.
- **The host is fixed for the life of a mount.** A new locale or another instance is a new mount,
  so read the `host` prop once (`untrack`) and take the translations from `host.messages`.
- **A link's destination is `<export>/<id>`**, which the host resolves in any plugin. Its source
  target is free text that only the plugin that wrote it reads. `host.links.replace` takes every
  link the instance is the source of, so deleting an item sends the list without that item's links.
- **Nothing about AI is designed before its own stage.** The manifest reserves `aiSchema`: leave it
  out.

## The folder

The kit's README draws the layout. In this repository:

- **The folder's name is the plugin's registry name**: lowercase letters, digits and hyphens. Its
  `package.json` is private and holds no version, since the manifests name them.
- **Declare `svelte`, `@a-novel-kit/uikit` and the kit (`workspace:*`) in the plugin's own
  `package.json`.** `build` bundles the tokens of the uikit the plugin's folder resolves, and
  `check` reads uikit's component properties from the plugin's own `node_modules`. A uikit that only
  the root declares bundles the root's tokens and fails the check on uikit's own properties.
- **A pull request that changes a dependency waits for the `allow-dependency-change` label.** It is
  the developer's to add, and every push removes it.
- **`ui/index.ts` exists from the first version.** `check` and `build` both need it. A Svelte
  plugin exports `definePlugin(App)`, where `App` receives the host as its `host` prop.
- **`ui/content.ts` holds the content's types as `type` aliases.** Only an alias is assignable to
  the `Json` the host takes; an `interface` is not.
- **No hex color in `ui/`, comments included**, outside `*.test.*` and `*.fixture.*` files. uikit's
  token contract reads every line of every source file there and refuses a legacy color. Sample
  colors live in tests and fixtures.

## The manifest

A plugin writes one manifest per version, `versions/<version>.yaml`, and publishes it as JSON. It
holds `manifest: 1`, `schema` and `exports`. The contract's README lists each member's rules, and
`plugin-kit check` applies them. Write the schema this way:

- **The dialect is JSON Schema 2020-12 without `format`, `$dynamicRef` or `$dynamicAnchor`, and each
  `$ref` starts with `#`.** Validators disagree on formats, and the browser's validator lacks the
  dynamic keywords. Use a `pattern` where a format would go.
- **Every `title` and `description` is a translation key**, in every subschema, and so is an
  export's `name`. Name a key `schema.` followed by the path of its subschema without `$defs`, and
  give it a translation in every language in the same change.
- **`default` is the content of a new instance**, valid against the schema. One default serves every
  language, so a name in it is English.
- **Close every object (`additionalProperties: false`) and bound every string and array**
  (`maxLength`, `maxItems`). A misspelled member is then refused instead of stored, and no content
  grows without limit. The UI keeps the same limits in its fields and actions, or the host refuses
  the save: a unit test reads them from the schema.
- **Tag each union by `kind`**: every branch is an object whose `kind` is a `const`, listed under
  `oneOf`. The tag lets a validator report the problems of one branch, and the host's salvage
  descends into the branch whose tag matches.
- **An ID is a lowercase UUID, as `crypto.randomUUID()` writes it**, made once and never reused. A
  link stores it, so it survives a reorder, which an index would not.
- **Say in a YAML comment what the schema cannot enforce**, such as a value that must repeat
  another. The UI holds that rule, and a test proves it.

## Messages

- **`i18n/en.json` is the reference.** Every other language holds exactly its keys, and `check`
  refuses a difference. Type the messages from English
  (`(typeof import("../i18n/en.json"))["messages"]`), so TypeScript refuses a key English lacks.
- **Write French as [write-frontend](../write-frontend/SKILL.md#localization-language-register)
  rules.**
- **Only the current version is held to the English keys.** A key that only a past version names may
  leave.
- **uikit's built-in text falls back to English.** Components that call uikit's `useMessages()`,
  such as `Select`, `Combobox` and `ActionMenu`'s default trigger, read nodelib-i18n's context, and a
  plugin mounts outside Studio's. Pass such a component every text it shows, or set nodelib-i18n's
  context on the root from `host.locale` and declare `@a-novel-kit/nodelib-i18n` and `i18next`.

## Styles

A plugin renders on its host's canvas, in a shadow root, inside a box its host sizes. The kit's
README describes the mechanics under "The styles".

- **Fill the box.** The root takes `block-size: 100%` and what outgrows it scrolls inside. Never
  size anything from the screen, with viewport units or otherwise: the page does not scroll for a
  plugin.
- **The canvas holds the plugin.** No card around the whole plugin: title groups with uikit's
  `Section` and build `Card`s as a page does.
- **Keep uikit's token contract.** Take each value from
  [the ladders](../write-design-system/references/ladders.md), set text with
  `font: var(--text-<role>)`, and put `/* off-scale: <reason> */` above a value no token holds.
  `plugin-kit check` runs the contract on `ui/`. A misspelled token shows as a missing color in the
  browser, never as an error.
- **Set the text color of what the plugin draws** (`color: var(--color-text-primary)` on its root).
  The canvas sets none, so text outside a `Card` or a `Section` takes the browser's default.
- **The lamp is the host's.** A plugin sets none.
- **A color the writer gives an item is optional**: a six-digit hex value (`^#[0-9A-Fa-f]{6}$`),
  absent for no color. Draw it with uikit's `customColorStyle` and pick it with `ColorField`, as
  [write-design-system](../write-design-system/SKILL.md#color-type-space-and-shape) rules for a
  custom color. The chrome stays neutral.
- **The element that holds the shadow root is the plugin's**: its tokens are set on it through
  `:host`. A host lays a plugin out from a parent of that element.
- **A modal `Dialog` blurs and holds the page from a shadow root from uikit 0.29.1 on**
  (a-novel-kit/uikit#417): it adds a stylesheet of its own to the document while it shows. Under
  an earlier uikit it only dims the page, so a plugin that opens one depends on 0.29.1 or later.
  Lists, menus and tooltips open against their source as on a page. Prefer an undo to a confirmation
  dialog.
- **A build is one file that imports nothing at run time.** A dynamic `import()`, a second
  stylesheet or an asset file fails `build`.

## Testing

Load `write-frontend-tests`. What a plugin adds:

- **Test the UI as it is built.** A browser test mounts the module `plugin-kit build` writes: a
  Vitest global setup runs the command once and provides the source, and the test imports it from a
  blob URL. A UI imported from its sources carries no tokens and reads the test host's document.
  `plugins/scenes-composer/ui/built.fixture.ts` and `kit/test/setup.ts` show the pattern. A build
  made inside a test sets `NODE_ENV` to `production` (`vi.stubEnv`): under Vitest it is `test`, and
  a development build embeds file paths.
- **Mount with `mountPlugin` and the manifest**, so the test host refuses an invalid save as the host
  does. Read the result back with `expect.poll(() => mounted.content())`, locate with
  `page.elementLocator(mounted.target)`, and destroy every mount in `onTestFinished`: mounts share
  the screen's height.
- **Unit-test content transitions and presets in Node**, each result validated against the schema
  with nodelib-json's `validateJson` and the manifest read from its YAML.
- **Fixtures are file snapshots.** The tests write `fixtures/<current version>/` with
  `toMatchFileSnapshot`, from IDs that count up so a fixture is the same on every run. After a change
  to the schema, the presets, the messages or a generator, run `pnpm build`, then `pnpm test -u`,
  and commit what changed. CI writes nothing, so a stale fixture fails `test-js`.
- **The built-in browser pane does not see inside a shadow root.** Its `find` and `read_page` miss
  the plugin; screenshots show it, `javascript_tool` reaches it through `.shadowRoot`, and Playwright
  pierces an open shadow root.
- **uikit behaves this way under a test**: a click on a `Combobox` opens its list and closes it, so
  `fill` the field; a list, a menu and a tooltip slide into place, so poll a position.

Run the gate from the repository root: `pnpm format`, `pnpm lint`, then `pnpm test:ci`, which
compiles the packages, runs the tests in Node and Chromium, then `plugin-kit check` in every plugin.
`check` needs Node 24.2 or later: uikit's token contract script runs only under `import.meta.main`,
and on an older Node the kit reports that the check did not run. CI runs the browser tests in the
Playwright image `main.yaml` names. After a change to `pnpm-lock.yaml`, run the gate on a fresh copy
(`git archive HEAD | tar -x -C <folder>`, then `pnpm install --frozen-lockfile`) before pushing.

## Looking at a plugin

Compile the packages (`pnpm build` at the root), then serve the plugin from its folder:

```bash
pnpm exec plugin-kit preview --host 127.0.0.1
```

The page mounts the built module in the test host and rebuilds when a file changes. Its address
holds every choice: `?locale=fr`, `?content=<fixture of the current version>`, and `?reset` to
forget what the plugin saved for that start. Pass `--host 127.0.0.1`: by default Vite listens on
IPv6 `localhost` alone.

The preview is a plugin's review surface. The rendered-UI gate of
[write-frontend](../write-frontend/SKILL.md#live-ui-review--mandatory-handoff) applies to it in
place of Storybook: keep it live, hand the exact address of each state to review, one fixture per
state, and send screenshots at the reviewed widths.

## Versions and migrations

- **Nothing of a plugin needs a migration until a version of it is published.** Until then, change
  the current manifest in place and write its fixtures again.
- **A published version never changes.** Instances pin it. Any change to what it ships (its
  manifest, its UI, its migration or its messages), a UI fix included, is a new version with its own
  manifest. The registry answers 409 for a version it
  holds, which `publish` reads as done: a change sent under a published number exits 0 and never
  reaches a writer.
- **Number a version by what its upgrade does.** A patch keeps the schema; a minor changes it and
  carries every earlier content over whole; a major sets content aside. `check` refuses a loss
  inside a major version. A major costs writers their content: plan it with the developer.
- **Write a migration only when the upgrade cannot carry the content alone.** The upgrade validates
  against each version's schema, fills a missing required member from its subschema's `default`, and
  sets aside what still fails. An optional member, a member with a default, a new enum value or a
  new union branch need no migration. A value derived from the old content, or a moved shape, does.
- **A migration is `migrations/<version>.ts`**, from the previous version alone. It receives a copy
  and returns `{ migrated, unmigrated }`, where `unmigrated` is what the host keeps for the writer.
  Type its input and output inside the file: `ui/content.ts` follows the current version and drifts
  from the shape the migration was written for.
- **Fixtures of every version stay.** `check` carries each one through every later version: they
  prove the upgrade. A new version gets its own fixtures once the tests name its folder.

## Publishing

`plugin-kit publish --url <registry route>` checks the plugin, builds it under the version it sends,
and posts one JSON document: the name, the version, the schema, the exports, the UI module, the
migration to this version, and every language. A 409 answer counts as done, so a release can run
twice. `--prerelease dev.1` sends `<version>-dev.1`, for the registry of a local stack.

How the registry authenticates a publisher, and which route a release calls, is not settled.
Publishing to a shared registry is a release: hand it to the developer.

## Commands

| What                                              | Command                                                             |
| ------------------------------------------------- | ------------------------------------------------------------------- |
| Compile the contract and the kit, which run built | `pnpm build`, at the root, after a change to their sources          |
| Check one plugin, or every plugin                 | `pnpm exec plugin-kit check` in its folder, or `pnpm check:plugins` |
| Build the module a registry would serve           | `pnpm exec plugin-kit build`, in its folder, to `dist/`             |
| Look at it                                        | `pnpm exec plugin-kit preview --host 127.0.0.1`, in its folder      |
| Write the fixtures again                          | `pnpm test -u`, at the root                                         |
| The CI gate                                       | `pnpm lint` and `pnpm test:ci`, at the root, after `pnpm format`    |
