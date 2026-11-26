# The Scenes Composer

The first plugin, in `plugins/scenes-composer/`. Its current manifest and `ui/content.ts` define the
content. This page holds what they cannot say, and the design rules of its Epic
(a-novel/.github#620). The glossary of a-novel/.github#500 names every item: beat, scene, layer,
cell, transition, preset, block, timeline, scene view.

- [What the schema cannot hold](#what-the-schema-cannot-hold)
- [Links](#links)
- [Presets](#presets)
- [Views](#views)
- [Look](#look)
- [Fixtures](#fixtures)

## What the schema cannot hold

The interface holds these rules, and a test proves each one:

- **An ID is made at creation and never reused.** A link to a deleted item then stays broken
  instead of pointing at a stranger.
- **A scene holds one cell per layer at most, and a cell's `kind` repeats its layer's.** The schema
  cannot read the layer from the cell.
- **A cell may be absent or empty.** Whether a cell is filled depends on its content, not on its
  presence.
- **A link layer has no cell.** Its links live in the project's links, which the host holds.
  `accepts` is read on a link layer alone, and a link layer without it offers nothing to pick.
- **Every limit of the schema is a limit of a field.** A title, a logline or a text the field lets
  past the schema's `maxLength` makes the host refuse the save.

## Links

| What                          | Source target, in this instance | Destination                        | Relationship         |
| ----------------------------- | ------------------------------- | ---------------------------------- | -------------------- |
| A link of a link layer's cell | `cells/<scene ID>/<layer ID>`   | `<export>/<item ID>`, any instance | The link's own       |
| A transition                  | `scenes/<scene ID>`             | `scenes/<scene ID>`, this instance | `therefore` or `but` |

- An ID holds no slash, so a source target splits on `/`.
- Moving a scene changes no link. Deleting a scene or a link layer sends one `host.links.replace`
  without the links it was the source of.
- The composer exports its scenes as `scenes`: a link addresses a scene as `scenes/<scene ID>`.

## Presets

- **A preset is offered once, when an instance is new.** A composer with no beat has not started:
  that is when the presets show. After that, a writer edits by hand.
- **`PRESETS` in `ui/presets.ts` lists them in the order a composer offers them**: blank, Hollywood
  beats, 15 beats. Never name the last one after the trademarked structure it follows.
- **A preset returns a whole content**: its beats, each with one empty scene, and the two starting
  layers (Scenes, a logline layer, and Enriched scenes, a script layer) under translated names, all
  with new IDs.
- **The order of the beats is code, their words are messages.** To add a beat, add its key to the
  preset's list and its texts to every language. TypeScript refuses a key English lacks.

## Views

- **Timeline:** scenes are columns under their beat, layers are rows. The horizontal reading.
- **Scene view:** one scene, its layers stacked. The vertical reading, and where long text is
  written. The timeline shows an excerpt and a word count.
- **On a phone:** one scene at a time, and a swipe moves between scenes. Layer names collapse to an
  icon and open over the grid. Each layer carries an icon from a curated list, which the schema does
  not hold yet.
- **A transition stays attached to its two scenes.** The timeline draws it in its own row when they
  are adjacent, and as a reference when they are not.
- **Drag and drop moves scenes and beats**, with a keyboard and a menu equivalent. The keyboard and
  the menu are the floor: drag and drop often fails on a touch screen.

## Look

A sequencer for stories: a dark, dense, neutral work surface, where color belongs to the writer's
material and never to the chrome. The reference is professional production software, such as FL
Studio's playlist.

| Seen in the reference                                  | Rule here                                                                                |
| ------------------------------------------------------ | ---------------------------------------------------------------------------------------- |
| Track headers carry a color, clips take a tint of it   | A layer can carry a color: a solid mark in its header and a tint on its filled cells     |
| Empty lanes stay neutral                               | An empty cell has no tint, so where the story is written reads at a glance               |
| Bar lines are stronger than beat lines                 | A beat boundary is a stronger separator than a scene boundary. Rows part with a hairline |
| Small type, tight rows, no padding around every clip   | Compact rows whose height follows the layer kind. No card around each cell               |
| Controls appear where the hand is                      | Row and column actions show on hover and focus, and always in a menu and by keyboard     |
| A fixed set of well-tuned colors, picked from a swatch | A writer picks any color, from swatches or freely. Without one, an item stays neutral    |

Avoid a spreadsheet's uniform grid, a card per cell, chrome that competes with the lanes, and glow
or gradients as decoration.

A beat and a layer may carry a color: any opaque hue, stored as a hex value, with no color as the
default. uikit's custom colors draw it, as [the styles](../SKILL.md#styles) of a plugin say.

Inside a grid whose headers stick, draw the focus of a cell as an inset `box-shadow`, with an
outline under forced colors: an outline paints above the sticky headers the cell scrolls under.

## Fixtures

`fixtures/<version>/` holds `default.json`, one `<preset>.<language>.json` for each preset and
language, and `large.json`. The tests write them all.

- **`large.json` is the size to measure against**: 20 beats of 10 scenes under 10 layers of every
  kind, four cells in five filled. `largeContent()` in `ui/content.fixture.ts` builds it and imports
  nothing from Node, so a browser test can render it.
- **IDs count up** (`countingIds()`), so a fixture is the same on every run. A preset test replaces
  `crypto.randomUUID` with it.
- **The preview starts from any of them**: `?content=large`, `?content=hollywoodBeats.fr&locale=fr`.
