# Haechi Brand Assets

Collected 2026-08-30 from the repository and from the brand documents in
`dev-docs/brand/`.

## Finding: there is no logo artwork to extract

The three brand PDFs contain **zero embedded images**. `pdfimages -list`
returns an empty table for all three. They are written specifications that
describe the intended identity in words: the visual metaphor, the colour
palette with hex, RGB and CMYK values, typography, and rules for what a logo
should convey.

No designer has produced logo artwork from that specification, or if they have,
it is not in this repository.

The only actual brand image that exists is the shield mark drawn by hand in
SVG for the website.

## Contents

| Path | What it is |
| --- | --- |
| `logo/haechi-mark.svg` | The only real logo asset. Three vector paths, 64x64 viewBox, accessible title and description |
| `reference/*.png` | The 13 brand document pages rendered at 150 dpi, for reading the specification visually |
| `spec/*.txt` | Text of the same documents, searchable |

## Specified palette

From `haechi-brand-designer-guidelines.pdf`.

| Name | Hex | Role |
| --- | --- | --- |
| Dark Charcoal | `#1A1A1A` | Dominant background |
| Deep Black | `#000000` | Stealth |
| Orange-Red | `#FF4500` | Alert and emphasis |
| White | `#FFFFFF` | Contrast |

Three further values appear in the creative direction documents and are not
reconciled with the guidelines above: `#00C4B4` teal, `#6A0572` purple, and
`#0A1128` deep navy, plus greys `#333333` and `#CCCCCC`.

## The implementation does not match the specification

Comparing the palette against `website/styles.css`:

| Specified | Used by the site | Status |
| --- | --- | --- |
| `#FF4500` | `#FF4500` | Matches |
| `#1A1A1A` | `#09090A`, `#0B0B0C`, `#050506` | Darker near-blacks, not the specified charcoal |
| `#CCCCCC` | `#B6B6AE`, `#D4D3CE`, `#777671` | Warm greys, not the specified neutral |
| `#00C4B4` teal | not used | Absent |
| `#6A0572` purple | not used | Absent |

Only the orange-red is a literal match. The site also introduces an orange
ramp that no document specifies: `#FF9B74`, `#FFB08F`, `#FF8B5E`, `#FF6A32`,
`#C93900`.

Either the site or the guidelines is wrong. Someone who owns the brand needs to
decide which, and the losing document should be superseded rather than left to
contradict the other.

## Typography

Specified but not verified against the implementation:

- Headings: geometric sans-serif, suggested Inter, Space Grotesk or Syncopate
- Body: legible sans-serif, suggested Roboto or Open Sans
- Monospace: JetBrains Mono or Fira Code, for keys, code and terminal output

The site currently requests Inter with a system fallback stack and ships no
font files, so it renders in the system font for most visitors.

## Missing assets a launch would need

None of these exist anywhere in the repository:

- Favicon set: `.ico`, and PNG at 16, 32, 48, 180, 192 and 512 pixels
- Apple touch icon and a web app manifest
- Open Graph and social preview image, typically 1200x630
- Logo lockups: horizontal and stacked wordmark with the mark
- Monochrome and inverted variants for single-colour reproduction
- Raster exports of the mark for contexts that cannot use SVG

The mark is clean vector, so all of these can be generated from
`logo/haechi-mark.svg` once someone confirms the mark itself is approved.

## Regenerating

```bash
brew install poppler
pdftoppm -png -r 150 dev-docs/brand/<document>.pdf brand-assets/reference/<document>
pdftotext -layout dev-docs/brand/<document>.pdf brand-assets/spec/<document>.txt
```
