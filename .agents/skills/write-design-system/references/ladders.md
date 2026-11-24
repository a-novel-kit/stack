# The ladders

Every visual value in uikit and in a platform is decided by what the thing is, never by eye. Find the
thing in its ladder and take its token. A value no ladder holds is a rare exception: it carries an
`/* off-scale: <reason> */` comment directly above its declaration, and a role that recurs gets a row
here first. The token contract lint (`pnpm lint`, in uikit and in every platform) fails on a value
outside its family and prints the exceptions it let through.

The same tables render in uikit's Storybook foundations, and each token carries its role beside its
definition in `tokens.css`. Change the three together.

- [Text styles](#text-styles)
- [Text color](#text-color)
- [Spacing](#spacing)
- [Sizes](#sizes)
- [Surfaces](#surfaces)
- [Shape](#shape)
- [Elevation, motion and layers](#elevation-motion-and-layers)

## Text styles

An element takes its style with one declaration, `font: var(--text-<role>)`, which sets its weight,
size, line height and family together. Never set `font-size`, `line-height` or `font-family` in a
component or an application; `font: inherit` on a native control stays. Set `font-weight` alone, with a
weight token, only for a state such as a chosen item.

| The text is                                                                             | Token                             | Draws as                            |
| --------------------------------------------------------------------------------------- | --------------------------------- | ----------------------------------- |
| A page's title                                                                          | `--text-display`                  | display bold, 23 to 29px            |
| The title of a section, a dialog, or a state that replaces content                      | `--text-title`                    | display bold, 20px                  |
| A title one level down: a card that names itself                                        | `--text-title-sm`                 | display bold, 18px                  |
| A title two levels down                                                                 | `--text-title-xs`                 | display bold, 16px                  |
| Content: a value, a name, a dialog's or a state's copy, text that sets no style         | `--text-body`                     | regular 16px, open lines            |
| What a field holds on one line, and in a large field                                    | `--text-field`, `--text-field-lg` | regular 16px or 18px, compact lines |
| An explanation that may run several lines, such as a description under a title          | `--text-body-sm`                  | regular 14px, open lines            |
| Interface text: a label, a tab, a link of the rail, an option, a cell, a message's body | `--text-ui`                       | regular 14px, compact lines         |
| A group's name, a message's title, a column's header                                    | `--text-ui-strong`                | bold 14px, compact lines            |
| A button's label                                                                        | `--text-action`                   | bold 14px, one line                 |
| A large button's label                                                                  | `--text-action-lg`                | bold 16px, one line                 |
| A compact fact: a field's hint or error, a timestamp, a count, a tooltip                | `--text-meta`                     | regular 13px, compact lines         |
| Small text on a fill: a badge, a chip, a tag                                            | `--text-tag`                      | medium 13px, compact lines          |
| An identifier, a shortcut, code                                                         | `--text-code`                     | mono 14px                           |
| Narrative prose                                                                         | `--text-story`                    | story face 18px, loose lines        |

A heading's level sets its style, never the reverse: level 2 takes `--text-title`, level 3
`--text-title-sm`, level 4 `--text-title-xs`. A `Section` finds its level from the sections that hold
it, so a caller counts none. The page's root takes `--text-body`, so text with no style of its own reads
as content, on the same even line box.

## Text color

The color follows the role, so a style and its color come as a pair.

| The text is                                                                          | Token                         |
| ------------------------------------------------------------------------------------ | ----------------------------- |
| What the reader came for: a title, content, a value, the label of a choice they make | `--color-text-primary`        |
| What names or explains it: a field's label, a description, a message's body          | `--color-text-secondary`      |
| What a reader can skip: a hint, a timestamp, a count, a link of the rail at rest     | `--color-text-muted`          |
| A placeholder, inside a well only: it fails 4.5:1 on an island                       | `--color-control-placeholder` |
| Disabled                                                                             | `--color-text-disabled`       |
| Colored: a tag, a colored action, a message's mark, the current item                 | its hue's ink, below          |

Primary, secondary and muted keep 4.5:1 on the canvas and on an island (18.0, 12.4 and 8.4:1 on the
canvas; 15.1, 10.4 and 7.0:1 on a card). Links, actions, feedback and the current item take their own
semantic tokens, each an alias of its hue's ink.

A hue shows two ways, and they pair: its ink, the neon itself, for what is drawn thin, and its tint, a
deep step of its ramp, for what is filled under its ink. The neon never fills behind text.

| The hue is       | Ink: text, an icon, a line                          | Tint: a tag, a chosen chip, a colored action | Ink on tint |
| ---------------- | --------------------------------------------------- | -------------------------------------------- | ----------- |
| Brand blue       | `--color-ink-brand`, the neon                       | `--color-tint-brand`                         | 5.7:1       |
| Pressure magenta | `--color-ink-pressure`, the neon, a quarter lighter | `--color-tint-pressure`                      | 4.9:1       |
| Signal amber     | `--color-ink-signal`, the neon                      | `--color-tint-signal`                        | 6.5:1       |
| None             | `--color-text-secondary`                            | `--color-action-neutral`, one veil           | 7:1 or more |

Under the pointer, a colored action lifts its fill one step up its ramp and its ink a quarter of the way
to white; a chosen one stays lifted and glows.

## Spacing

One step per relationship. Pick the relationship, never the pixels. A component that owns a
relationship already sets it: `Field`, `Section`, `Stack`, `ActionGroup`, `FormActions`, `Card`.

| The space separates                                                                                                              | Token             | Size       |
| -------------------------------------------------------------------------------------------------------------------------------- | ----------------- | ---------- |
| Links of one navigation group                                                                                                    | none              | 0          |
| Lines of one item, such as a name and its details; a list's edge and its options                                                 | `--space-1`       | 3px        |
| Chips or tags side by side, nothing else                                                                                         | `--space-1-5`     | 5px        |
| Parts of one thing: an icon and its label, a label and its field, a field and its note, a title and its description              | `--space-2`       | 6px        |
| Things that work as one unit: buttons in a group, a mark and its text block, islands side by side; a control's edge and its text | `--space-3`       | 9px        |
| Siblings: fields, list rows, a heading and its content; a compact island's edge and its content                                  | `--space-4`       | 12px       |
| A card's or a dialog's edge and its content, a dialog's parts, a page's top                                                      | `--space-6`       | 18px       |
| Groups: sections of a page, a form's actions under its last field, a rail's top and bottom groups                                | `--space-8`       | 24px       |
| A page's foot                                                                                                                    | `--space-12`      | 36px       |
| A page's sides                                                                                                                   | `--layout-gutter` | 12 to 24px |

A negative margin only lets a hover box overhang its row or a nested card reach its inset.

## Sizes

A size never takes a `--space-*` token, and a space never takes a size.

| The size is                                                                   | Token                                                                          |
| ----------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| A control's height: the default, a field, a form's or a toolbar's action      | `--control-height-md` (32px), no prop                                          |
| A compact control: an action in a row or beside a title, a tab, an option     | `--control-height-sm` (28px), `size="sm"`                                      |
| A page's only action, on a page built around it                               | `--control-height-lg` (40px), `size="lg"`                                      |
| An icon inside a control, beside interface text, or in a message's mark       | `--icon-size-sm` (16px)                                                        |
| An icon beside content, a checkbox, a radio button                            | `--icon-size-md` (20px)                                                        |
| A mark that stands alone, as the icon of a state that replaces content        | `--icon-size-lg` (24px)                                                        |
| Any other mark, picture or track                                              | `--size-N`, on the 4px grid                                                    |
| A page or a form's column                                                     | `--layout-container-{xs,sm,md,lg}`                                             |
| Running text                                                                  | `--layout-readable-measure` (68ch), `-long` (80ch) for a short block in a card |
| A floating surface: a menu's least width, a tooltip's most, a toast, a banner | `--layout-floating-{sm,md,lg,xl}` (12, 16, 20, 32rem)                          |
| A rail or a split pane's side                                                 | `--layout-sidebar`                                                             |

A coarse pointer adds 4px to every control height. Percentages, viewport units and intrinsic keywords
need no token.

## Surfaces

| The surface is                                                                       | Token                                                                                             |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------- |
| The page                                                                             | `--color-surface-canvas`                                                                          |
| A well: a field, or a passive well cut into its surface                              | `--color-surface-sunken`                                                                          |
| A card                                                                               | `--color-surface-island-glass`                                                                    |
| A passive group inside an island: a fieldset, a description's cell                   | `--color-surface-island-subtle`                                                                   |
| An opaque island: the navigation, a dialog, a menu                                   | `--color-surface-island-strong`                                                                   |
| A frosted bar or popover                                                             | `--color-surface-glass`                                                                           |
| What the pointer hovers, on any surface                                              | one veil more: `--color-surface-glass-hover`, or `--color-action-subtle-hover` under colored text |
| A neutral action at rest, a neutral badge, a switch's empty track, a chip not chosen | one veil over what holds it: `--color-action-neutral`                                             |
| A tag, a chosen chip, a colored action                                               | its hue's tint, under its ink: `--color-tint-{brand,pressure,signal}`                             |
| A mark that is on: a checked box, a switch, a slider's filled part                   | the brand's ink: `--color-control-on`                                                             |
| An app's lamp, one of the three hues, never a blend                                  | `--color-lamp-{amber,magenta,blue}`                                                               |

A line is `--color-border-divider` between siblings, a hairline wide, or `--color-border-default` around
a shape with no fill, at `--border-width-outline`.

## Shape

| The shape is                                                                                                    | Token                                        |
| --------------------------------------------------------------------------------------------------------------- | -------------------------------------------- |
| A mark or an inline piece: a badge, a chip, a tag, a checkbox, a flag, a link's focus box                       | `--radius-sm`                                |
| A small square or a row in a list: an icon-only button, a compact navigation item, an option, a tooltip, a cell | `--radius-md`                                |
| A labeled control or a compact island: a button, a field, a navigation link, a message box, a table, a fieldset | `--radius-lg`                                |
| An island or a track: a card, a dialog, a switch, a meter                                                       | `--radius-xl`                                |
| A circle: a radio button, a round avatar, a spinner, a thumb                                                    | `--radius-round`                             |
| A shape nested in another                                                                                       | the outer radius less the space between them |

## Elevation, motion and layers

| The thing is                                                          | Token                             |
| --------------------------------------------------------------------- | --------------------------------- |
| A raised card: flat, held by a hairline ring                          | `--shadow-ring`                   |
| An island on an island: a nested card, a tooltip                      | `--shadow-md`                     |
| A surface that opens over the page: a menu, a list, a dialog, a toast | `--shadow-lg`                     |
| A surface resting at the screen's edge                                | `--shadow-floating`               |
| A control changing state: hover, press, check, a list opening         | `--motion-state`                  |
| A surface entering, leaving or resizing: a dialog, the rail           | `--motion-surface`                |
| A mark that lands in place: a check, a radio button's dot             | `--motion-mark`                   |
| The beat of a loop: a spinner, a skeleton                             | `--duration-slow`, eased `linear` |
| A menu, a list, a tooltip                                             | `--layer-dropdown`                |
| A sticky bar, the shell's header                                      | `--layer-sticky`                  |
| A dialog                                                              | `--layer-modal`                   |
| A toast, the consent banner, the skip link                            | `--layer-toast`                   |

A motion token carries its duration and its easing together: `transition: color var(--motion-state)`.
Inside one component, a `z-index` of -1, 0 or 1 orders its own layers and needs no token.

A viewport breakpoint is one of two widths, which a media query cannot read from a token: 35rem, where
an action group stops stacking, and 48rem, where a page stops being a phone's. A container query
follows its own content.
