# Haechi Brand Specification

Status: Authoritative
Approved: 2026-08-30
Supersedes: `haechi-brand-designer-guidelines.pdf`,
`haechi-product-branding-and-creative-direction.pdf`,
`haechi-product-branding-and-creative-direction-document.pdf`

This document is the single source of truth for the Haechi visual identity. It
replaces the three brand PDFs, which disagreed with each other and with the
shipped website. Where any PDF conflicts with this file, this file wins.

It is written in Markdown rather than issued as a PDF so that changes are
reviewable and diffable, and so the palette can be checked against the
implementation automatically.

## Decisions recorded in this revision

1. **Teal `#00C4B4` is removed** from the palette. It appeared only in the
   creative direction documents, was never in the guidelines, and was never
   used in the product.
2. **Purple `#6A0572` and navy `#0A1128` are retained** as the extended
   palette, reserved for future use.
3. **The core palette is now defined by the shipped website**, not by the
   earlier prose. The previously specified Dark Charcoal `#1A1A1A` and neutral
   grey `#CCCCCC` are withdrawn, because the product uses a darker, warmer
   scale and has done since launch.

## Core palette

These values are exactly those in `website/styles.css`. They are the palette.

### Surfaces

| Token            | Hex       | Role                                             |
| ---------------- | --------- | ------------------------------------------------ |
| `--black`        | `#050506` | Page background, deepest surface                 |
| `--ink`          | `#09090A` | Primary surface, header background               |
| `--panel`        | `#0F0F11` | Card and panel background                        |
| `--panel-raised` | `#151517` | Raised panel, hover state                        |
| n/a              | `#080809` | Gradient stop above `--black`                    |
| n/a              | `#0D0D0F` | Alternate panel background                       |
| n/a              | `#F0F0EA` | Light panel background, the one inverted surface |

### Text

| Token     | Hex                    | Role                                                                              |
| --------- | ---------------------- | --------------------------------------------------------------------------------- |
| `--white` | `#F7F7F3`              | Primary text, warm white, never pure `#FFFFFF`                                    |
| `--muted` | `#AAA9A3`              | Secondary text                                                                    |
| n/a       | `#D4D3CE`              | Body copy in prose sections                                                       |
| n/a       | `#777671`              | Tertiary text, captions, footnotes                                                |
| n/a       | `#F7F7F3` to `#B7B6B0` | Light text ramp: `#F0F0EA`, `#DEDDD7`, `#D6D5D0`, `#CDCCC6`, `#C2C1BC`, `#B7B6B0` |
| n/a       | `#73726E` to `#5D5C58` | Dim text ramp: `#73726E`, `#6F6E69`, `#66655F`, `#5D5C58`                         |
| n/a       | `#111113`              | Text on the light panel                                                           |

### Accent

| Token             | Hex                      | Role                                                                     |
| ----------------- | ------------------------ | ------------------------------------------------------------------------ |
| `--accent`        | `#FF4500`                | Primary accent, the one colour carried from the original guidelines      |
| `--accent-bright` | `#FF6A32`                | Hover and active state                                                   |
| `--accent-soft`   | `rgba(255, 69, 0, 0.14)` | Accent wash behind emphasis                                              |
| n/a               | `#FF8B5E`                | Border emphasis                                                          |
| n/a               | `#FF9B74`                | Accent text on dark surfaces                                             |
| n/a               | `#FFB08F`                | Light accent fill                                                        |
| n/a               | `#C93900`                | Accent on light surfaces, meets contrast where the brighter tones do not |

### Lines and status

| Token           | Value                       | Role                                |
| --------------- | --------------------------- | ----------------------------------- |
| `--line`        | `rgba(255, 255, 255, 0.12)` | Standard divider                    |
| `--line-strong` | `rgba(255, 255, 255, 0.22)` | Emphasised divider, control borders |
| `--success`     | `#8DFCA4`                   | Success state                       |
| n/a             | `#B6B6AE`                   | Diagram borders on light panels     |

### Extended palette, reserved

| Name   | Hex       | Status                       |
| ------ | --------- | ---------------------------- |
| Purple | `#6A0572` | Approved, not currently used |
| Navy   | `#0A1128` | Approved, not currently used |

Introducing either into the product requires a defined role in this document
first. Do not use them ad hoc.

### Withdrawn

| Name          | Hex       | Reason                                                                      |
| ------------- | --------- | --------------------------------------------------------------------------- |
| Teal          | `#00C4B4` | Removed by decision, 2026-08-30                                             |
| Dark Charcoal | `#1A1A1A` | Superseded by the `#050506` to `#151517` surface scale                      |
| Neutral Grey  | `#CCCCCC` | Superseded by the warm text scale                                           |
| Pure White    | `#FFFFFF` | Superseded by `#F7F7F3`. Pure white is not used as a text or surface colour |
| Deep Black    | `#000000` | Superseded by `#050506`                                                     |

## Typography

The site sets one family and ships no font files, so most visitors see the
system fallback. This is the current state, recorded honestly.

| Role     | Stack                                                                             |
| -------- | --------------------------------------------------------------------------------- |
| All text | `Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif` |

The earlier guidelines proposed separate heading, body and monospace families,
including Space Grotesk, Syncopate, Roboto, Open Sans, JetBrains Mono and Fira
Code. None were adopted. A monospace face is still wanted for keys, code and
terminal output; until it is chosen and self-hosted, that requirement is open
rather than specified.

Self-hosting Inter would make the rendered result match this specification for
every visitor. It is not done today.

## Logo

`website/assets/haechi-mark.svg` is the approved mark and the only brand
artwork that exists.

It is an abstract shield in three vector paths on a 64x64 viewBox, carrying an
accessible title and description. Its construction follows the intended
concept: an abstract protective form rather than a literal creature, using the
accent sparingly against a near-black base.

| Element      | Colour    |
| ------------ | --------- |
| Outer shield | `#FF4500` |
| Inner field  | `#0B0B0C` |
| Inner glyph  | `#FFFFFF` |

The inner field `#0B0B0C` sits between `--ink` and `--panel` and is specific to
the mark. Pure white is used inside the mark for maximum contrast at small
sizes and is the one sanctioned exception to the rule above.

### Rules

- Use the SVG wherever vector is supported.
- Do not recolour, rotate, stretch, or add effects.
- Clear space on all sides is at least the height of the inner glyph.
- Minimum size is 24 pixels; below that the inner glyph stops resolving.
- On light surfaces, keep the mark as drawn. There is no approved inverted
  variant yet.

### Assets that do not exist

Required before a public launch, all generable from the approved mark:

- Favicon set: `.ico` plus PNG at 16, 32, 48, 180, 192 and 512 pixels
- Apple touch icon and web app manifest
- Open Graph and social preview image at 1200x630
- Horizontal and stacked wordmark lockups
- Monochrome and inverted variants

## Voice

The product is an engineering preview. Brand and marketing language must not
claim capabilities the implementation does not have.

The superseded PDFs describe Haechi as "provably anonymous" and
"quantum-resistant" with a "cryptographic no-logs guarantee". None of that is
implemented. See [`../engineering/readiness.md`](../engineering/readiness.md)
for what is true today, and do not reuse those phrases in any public material
until the corresponding release gates pass.

## Reading the superseded PDFs

The three PDFs contain no embedded images. `pdfimages -list` returns an empty
table for all three. They are written specifications, not asset containers, so
there is no logo artwork inside them to extract.

To read them as images, or to search their text:

```bash
brew install poppler
pdftoppm -png -r 150 dev-docs/brand/<document>.pdf brand-assets/reference/<document>
pdftotext -layout dev-docs/brand/<document>.pdf brand-assets/spec/<document>.txt
```

`brand-assets/` is git-ignored. It is entirely derived from the committed PDFs
and the committed SVG, so it is regenerated rather than stored.

## Motion

Motion reinforces Haechi's engineering restraint: fast, restrained, and fail-closed in UX. All animations must respect `prefers-reduced-motion`.

| Token | Value | Use |
|-------|-------|-----|
| duration.instant | 0ms | Disabled states, immediate feedback |
| duration.fast | 150ms | Micro-interactions, hover |
| duration.base | 250ms | Standard transitions |
| duration.medium | 350ms | Reveals, tab switches |
| duration.slow | 500ms | Hero entrance |
| duration.slower | 750ms | Page-level emphasis |
| easing.standard | cubic-bezier(0.22,1,0.36,1) | Default enter/exit |
| easing.entrance | cubic-bezier(0.16,1,0.3,1) | Reveal stagger |
| easing.exit | cubic-bezier(0.4,0,1,1) | Hide |
| easing.emphasis | cubic-bezier(0.2,0.8,0.2,1) | Spring emphasis |
| spring.stiffness | 260 | Motion spring stiffness |
| spring.damping | 22 | Motion spring damping |
| spring.mass | 1 | Motion spring mass |
| stagger | 80ms | Reveal stagger interval |

All motion is driven by the brand tokens above; JavaScript animations use Motion tokens, CSS transitions use the duration and easing values.
