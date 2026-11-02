# DESIGN.md

RoNote is a reading tool. Typography comes first, and the UI stays out of the way of the text.

## Type

- Fonts are self-hosted via Fontsource. No font CDNs.
  - Source Serif 4: document body.
  - IBM Plex Sans: UI, and the reader's sans option for the body.
  - JetBrains Mono: code.
- Never Inter.
- UI text is 13–14px. Spacing sits on an 8px grid.

## Color

- Colors come only from design tokens (added in P04). No raw color values or default Tailwind palette classes in components.
- Warm paper and ink palette with one accent.
- Real dark mode with its own tuned tokens, not an inverted light theme.
- No gradients, no glassmorphism. Shadows only as 1px rules.

## Shape and icons

- Radius 3–5px. Pills only for tags.
- Phosphor icons at 16px.
- No emoji, no sparkle icons.

## Layout

- 240px sidebar, 68ch reading column, right margin for notes.
- No centered hero, no feature-card grids.

## Generated content

Machine-made content (later: OCR fixes, converted code) gets a thin left rule and a small "generated" label, so it can never pass as source.

## Motion

- Animate opacity and transform only.
- 120–160ms, ease-out.
- Respect `prefers-reduced-motion`.

## Copy

- Use plain verbs: "Import page", "Run".
- Banned words: unlock, supercharge, seamless, AI-powered, revolutionize.
