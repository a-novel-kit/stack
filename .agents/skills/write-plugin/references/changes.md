# Changing a plugin

Three changes, each through every file it touches. The paths are the Scenes Composer's; another
plugin has the same folders. Run every command from the repository root unless a step names the
plugin's folder.

- [Before any change: is the version published?](#before-any-change-is-the-version-published)
- [Add a member, with a migration](#add-a-member-with-a-migration)
- [Add a kind to a tagged union](#add-a-kind-to-a-tagged-union)
- [Release a version](#release-a-version)

## Before any change: is the version published?

A version is published once the registry holds it. Ask the developer when you cannot tell.

- **Not published:** edit the highest `versions/<version>.yaml` in place. No migration, no new
  version. Skip the version steps below.
- **Published:** the change ships as a new version. Copy the highest manifest to
  `versions/<new version>.yaml` and leave every earlier manifest, migration and fixture as it is.

## Add a member, with a migration

The example gives each layer a required `icon` that the old content does not hold. It is a member
made up for the walk, not a design.

1. **Choose the number.** The upgrade loses nothing, so the version is a minor: `1.0.0` becomes
   `1.1.0`. Copy `versions/1.0.0.yaml` to `versions/1.1.0.yaml`.
2. **Write the schema.** Add the member under `properties`, with a `title` key, and list it in
   `required`. Give `default` a value for it: `check` refuses a default its own schema refuses.
3. **Decide whether a migration is needed.** Run `pnpm exec plugin-kit check` in the plugin's
   folder. A required member without a default makes the upgrade set every earlier item aside, and
   `check` reports `version 1.1.0 loses /layers/0 inside a major version` for each fixture. Two ways
   out:
   - The member has one sensible value for old content: give its subschema a `default`. The upgrade
     fills it, and no migration is needed.
   - The value derives from the old content: write `migrations/1.1.0.ts`.
4. **Write the migration.** Type the old shape inside the file, as far as the migration reads it:

   ```ts
   import type { Json, Migration } from "@a-novel/plugins-narrative-engine-kit";

   // The content of 1.0.0, as far as this migration reads it.
   type Before = { beats: Json; layers: { kind: string }[] };

   // 1.1.0 gives each layer an icon, which 1.0.0 derives from its kind.
   const ICONS: Record<string, string> = { notes: "text", logline: "text", script: "script", link: "link" };

   const migrate: Migration = (content) => {
     const { beats, layers } = content as Before;

     return { migrated: { beats, layers: layers.map((layer) => ({ ...layer, icon: ICONS[layer.kind] ?? "text" })) } };
   };

   export default migrate;
   ```

   `check` now carries every fixture of `1.0.0` through it. A migration that returns `unmigrated`
   fails the check inside a major version: renamed `2.0.0`, the same migration passes.

5. **Translate the new keys** in `i18n/en.json` and every other language.
6. **Follow the type.** Add the member to `ui/content.ts`. TypeScript then lists every place that
   builds the type: the presets, `ui/content.fixture.ts`, the samples of the tests, the edits.
7. **Point the tests at the new version.** `ui/manifest.fixture.ts` reads the current manifest, and
   each `toMatchFileSnapshot` names `fixtures/<version>/`. Move both to `1.1.0`.
8. **Write the fixtures.** `pnpm build`, then `pnpm test -u` writes `fixtures/1.1.0/`. Keep
   `fixtures/1.0.0/`: it proves the upgrade.
9. **Run the gate**: `pnpm format`, `pnpm lint`, `pnpm test:ci`.

## Add a kind to a tagged union

The example adds a `summary` layer kind to the composer, whose cell holds one text.

1. **Choose the number.** A new enum value and a new union branch keep every earlier content valid:
   a minor, or the current version in place while it is unpublished.
2. **Write the schema.**
   - Add the kind to the `enum` of `layer.properties.kind`.
   - Add a `summaryCell` definition: `kind: { const: summary }`, `layer`, `text` with its
     `maxLength`, `additionalProperties: false`, and its `title` and `description` keys.
   - List it under `cell.oneOf`.
3. **Translate** the schema keys and the kind's display name in every language. A messages object
   indexed by the kind fails `svelte-check` until English holds the new name.
4. **Follow the type.** Add the kind to `Layer["kind"]` and a `SummaryCell` to the `Cell` union in
   `ui/content.ts`.
5. **Teach the UI.** TypeScript does not find every place: a view that ends on a fallback branch
   compiles and draws the new kind as another one. Search `ui/` for an existing kind's name, such as
   `"notes"`, and handle the new kind at each hit: the edits, the limit its field keeps, how its cell
   draws and edits, and the actions that add it. Then update the tests that enumerate the kinds or
   read the limits from the schema.
6. **Give the fixtures the kind.** `ui/content.fixture.ts` builds the large fixture: add a layer of
   the kind and a branch of `cell()` that fills it. A generator that falls through to another kind
   builds a cell whose kind differs from its layer's, which the schema accepts: only a test catches
   it.
7. **Write the fixtures and run the gate**, as in the steps above.

## Release a version

1. **Make sure the version is new.** The registry answers 409 for a version it holds, and `publish`
   reports it as "already published" with exit 0: a change under a held number never ships. A
   change without a schema change is a patch with a copy of the manifest.
2. **Check and build** in the plugin's folder: `pnpm exec plugin-kit check`, then
   `pnpm exec plugin-kit build`. `dist/ui.js` is the module, and `dist/migrations/<version>.js` each
   migration.
3. **Look at it** with `pnpm exec plugin-kit preview --host 127.0.0.1`, on the fixtures of the new
   version, in every language.
4. **Publish.** The command sends the current version to the registry route it is given:

   ```bash
   pnpm exec plugin-kit publish --url <registry route>
   ```

   Against a local stack, add `--prerelease dev.1`. How a publisher is authenticated is not settled,
   and publishing to a shared registry is the developer's: hand them the command, never run it.
