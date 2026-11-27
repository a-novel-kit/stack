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
- **A size only stretches.** `scene.width` and `layer.height` count quarters of the base text size.
  A track is never shorter than what it holds, and a narrow box ignores both sizes.
- **A started composer keeps one beat and one layer.** The schema lets both lists be empty: the last
  of each cannot be deleted.
- **A layer's `label` changes nothing of what its cells hold.** It is what a writer calls what the
  layer follows, shown under its name in place of its kind.

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

- **Timeline:** scenes are columns under their beat, layers are tracks. The horizontal reading.
- **A beat, a scene and a layer are each edited whole in a dialog**: a title and a description, or
  a name, a label and a color, saved at once. Two clicks, Enter or F2 open it, and a new item opens
  its own. Only a card is edited where it stands: a click on its text edits it in place.
- **A click on a card's band selects it.** It stays lit, and Delete or Backspace then removes it,
  with a message that takes the deletion back.
- **Every menu opens under the second button of a pointer**, the menu key or Shift with F10. No
  header draws a menu button. A beat keeps one button, which adds a scene.
- **A writer stretches a scene's column and a layer's track** by a grip under the pointer, and by
  Alt with an arrow. The content keeps the size, and a menu only gives the default back.
- **What describes a beat or a scene shows whole at the top, and scrolls away with the tracks**
  while the names stay in view. Out of view, it shows in a tooltip under the pointer or with the
  focus. Nothing collapses the head: a head that changes height on a scroll makes the page jump.
- **Undo and redo are the composer's.** It keeps its last hundred edits, never the choice of a
  structure, and joins the sizes one item takes in a row. The field of a card keeps its own history
  of the typing in place of the browser's, a word, a pause or a paste at a time, then hands over to
  the composer. A dialog's fields keep the browser's own: nothing of a dialog is saved yet.
- **Scene view:** one scene, its layers stacked. The vertical reading, and where long text is
  written. The timeline shows an excerpt and a word count.
- **In a narrow box**, under 48rem of the plugin's own width, the timeline shows one scene at a
  time at its whole width, and nothing scrolls across. A swipe moves between scenes, and two buttons
  do the same. Nothing is edited in place: a tap opens a dialog with the item's fields and every
  other action. The names of the layers are out of view, and a button at the end of the scene's name
  shows them over the cards.
- **A transition stays attached to its two scenes.** The timeline draws it in its own row when they
  are adjacent, and as a reference when they are not.
- **Drag and drop moves scenes and beats**, with a keyboard and a menu equivalent. The keyboard and
  the menu are the floor: drag and drop often fails on a touch screen.

## Look

A sequencer for stories: a dark, dense, neutral work surface, where color belongs to the writer's
material and never to the chrome. The reference is professional production software, such as FL
Studio's playlist.

| Seen in the reference                                  | Rule here                                                                                    |
| ------------------------------------------------------ | -------------------------------------------------------------------------------------------- |
| Clips sit on tracks                                    | A layer is a track, and a filled cell is a card on it, as tall as its track                  |
| Empty lanes stay neutral                               | A place without a card draws nothing, so where the story is written reads at a glance        |
| A clip's name bar is saturated, its body is dimmed     | A layer's exact color shows in the band of each of its cards alone                           |
| A track's name stands on its color                     | The name of a track stands on one plain fill of its color, with no band                      |
| Bar lines are stronger than beat lines                 | Under the names, a beat ends at a stronger line than a scene. The names draw no line         |
| Lanes part without a rule                              | Tracks part with the room around their cards                                                 |
| Small type, tight tracks                               | A card shows the first lines of its text, as many as its layer kind sets                     |
| A selected clip is lit                                 | A card under the pointer lights, and a selected one stays lit. A ring is for the keyboard    |
| Controls appear where the hand is                      | A menu opens where the pointer asked for it. A beat's one button shows on hover and focus    |
| A fixed set of well-tuned colors, picked from a swatch | A writer picks any color for a layer, from swatches or freely. Without one, it stays neutral |

Avoid a spreadsheet's uniform grid, a line between two tracks or two names, the exact color behind
text, text in a band, a menu button on every header, chrome that competes with the tracks, and glow
or gradients as decoration.

A layer may carry a color: any opaque hue, stored as a hex value, with no color as the default. A
beat and a scene carry none. uikit's custom colors draw it, as [the styles](../SKILL.md#styles) of a
plugin say, and each part of a track takes one of their properties:

| Part                   | Fill                                                                  |
| ---------------------- | --------------------------------------------------------------------- |
| The band of a card     | `--color-mark-custom`, the exact color                                |
| The name of a track    | `--color-tint-custom-lit`, the lit tint a label takes                 |
| A card, under its band | An eighth of `--color-ink-custom` over the canvas, a quarter once lit |

uikit holds no token for a card's fill. Derive it as uikit derives its feedback surfaces,
`color-mix(in oklab, var(--color-ink-custom) var(--color-mix-2), transparent)`, with `--color-mix-4`
once lit. A stronger fill under a track's name drops its secondary text under 4.5:1 from yellow to
cyan. Text on a name or a card is primary or secondary, never muted. A browser test holds each text
of a track at 4.5:1 over uikit's swatches, a gray and no color.

Inside a grid whose names stick, draw the focus of a name or a card as an inset `box-shadow`, with
an outline under forced colors: an outline paints above the sticky names a card scrolls under. Give
what takes the focus a `scroll-margin` as wide as the layers and as tall as the names: a beat wider
than the screen otherwise slides under the layers when it takes the focus.

## Fixtures

`fixtures/<version>/` holds `default.json`, one `<preset>.<language>.json` for each preset and
language, and `large.json`. The tests write them all.

- **`large.json` is the size to measure against**: 20 beats of 10 scenes under 10 layers of every
  kind, four cells in five filled. `largeContent()` in `ui/content.fixture.ts` builds it and imports
  nothing from Node, so a browser test can render it.
- **IDs count up** (`countingIds()`), so a fixture is the same on every run. A preset test replaces
  `crypto.randomUUID` with it.
- **The preview starts from any of them**: `?content=large`, `?content=hollywoodBeats.fr&locale=fr`.
