# Visual reference study

Use this method when screenshots, recordings, or admired interfaces inform a design direction. It
turns visual evidence into testable product rules without copying an identity or trusting captured
pixels.

## Extract relationships, not pixels

1. Group frames by sequence and identify whether each frame explains a rule, compares alternatives,
   or shows a finished result.
2. Record observations under stable dimensions: role, hierarchy, geometry, contrast, boundary,
   proportion, state, motion, and content density.
3. Separate the demonstrated principle from the example treatment. A blue button may demonstrate
   priority; blue itself may be irrelevant.
4. Mark each observation **retain**, **adapt**, or **reject** against the product thesis, existing
   foundations, accessibility, and implementation constraints.
5. Recreate the retained relationship with system tokens, render it in the target browser, and
   measure the actual semantic pair.

Treat HDR, wide-gamut, compressed, photographed, and color-managed captures as uncalibrated. Do not
sample them for production values. Use them to compare ordering and relationships, then tune the
result on the target displays and gamut.

## Color and contrast

- Assign a small role set before hues: primary action or identity, supporting distinction, essential
  foreground and background, and feedback roles as required.
- Keep the essential field very dark and low-chroma for a neon direction. Make upper neutral steps
  separate enough for text and disabled-state hierarchy.
- Use a deliberate harmony with a chroma budget. One accent should dominate a region; supporting
  accents appear only where their stable meaning matters.
- Build ramps with perceptual, gamut-aware curves. A smooth S-curve can keep early steps dark and
  accelerate into vivid upper steps; inspect every hue because equal OKLCH parameters do not create
  equal perceived energy.
- Validate the exact rendered foreground/background and control/text pairs. Tutorial safe zones,
  palette indexes, and harmony geometry are starting hypotheses, not contrast evidence.
- For distant-hue gradients, specify OKLCH or Oklab interpolation and the hue route. Add an intentional
  transition stop when the direct midpoint becomes muddy.

## Boundaries and islands

Choose the least explicit boundary that preserves hierarchy:

1. spacing, alignment, and invisible grouping;
2. tonal or opacity shift;
3. elevation and localized shadow;
4. visible border for interactive affordance, structural separation, or forced-colors fallback.

Use borderless semi-opaque islands for floating navigation, tool clusters, overlays, and independently
scannable regions. Use transparent groups where content hierarchy already provides the boundary.
Backdrop blur is progressive enhancement: the opaque fallback must remain legible.

## Light, glow, and geometry

- Treat glow as emitted light: a tight bright halo plus a softer, weaker outer halo derived from the
  emitting object's size. One eighth of its characteristic size is a useful first blur estimate, not
  a universal constant.
- Reserve glow for selected, focused, or intentionally luminous graphic states. Keep it off ordinary
  text and avoid permanent bloom on resting controls.
- Prefer a brightening plus localized glow for selected states when that matches the product contract;
  ensure hover never erases a persistent selected, invalid, checked, or expanded state.
- Center by perceived visual weight. Correct asymmetric glyphs, play marks, chevrons, and nested icons
  after viewing them in their actual control, while keeping corrections rare and documented.

## Proportion and density

Use a ratio ladder instead of one universal proportion:

- a small density ratio around 1.125 for type and compact workstation details;
- a medium ratio around 1.25–1.5 for component and local hierarchy;
- the golden ratio around 1.618 for occasional major page, split, or bento hierarchy.

Ratios organize priority; they do not override content fit, responsive constraints, minimum targets,
or optical balance.

## Rendered acceptance

Review the result in the default dark theme at narrow and wide sizes. Compare primitive ramps,
semantic uses, controls, navigation, overlays, and large surfaces together. Check selected plus hover,
focus, disabled, invalid, reduced motion, forced colors, sRGB fallback, and wide-gamut enhancement.
Record whether each adopted reference principle improved comprehension or hierarchy; remove effects
that only make the screenshot more decorative.
