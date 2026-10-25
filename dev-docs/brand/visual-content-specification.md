# Haechi Visual Content Specification

Status: Authoritative for visual posts
Approved: 2026-08-30
Extends: [`brand-specification.md`](./brand-specification.md)
Implemented by: [`brand/`](../../brand/), rendered to `brand-assets/posts/`

This document governs educational and marketing visual posts: the images
published to LinkedIn, X, and any other feed, plus the Open Graph card the
website needs before launch.

It extends the brand specification rather than replacing it. Where the two
disagree, the brand specification wins on identity, and this document wins on
post composition.

Two things make it different from a normal brand deck. First, it is executable:
the palette, the canvases, the templates and the claim rules all live in
`brand/` as code, and the posts are generated rather than hand-drawn. Second,
the claim rules are enforced by that code, so a prohibited marketing claim
cannot reach a rendered asset even by accident.

## Why this exists

Before this revision, the repository had no guidance on producing visual
content at all. It had a brand specification covering colour, type and the
logo, and it had a website carrying the only marketing copy that exists. It
had nothing that said how those combine into a post, and nothing that stopped a
post from repeating the claims the superseded PDFs make and the implementation
does not support.

## The claim rules, which are not negotiable

Four documents independently restrict what Haechi may say in public. They are
listed here in full because every post is bound by all four.

| Source                                                                                                           | Restriction                                                                                                                           |
| ---------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| [`../README.md`](../README.md), Claim Risk Notice                                                                | The legacy product material must not be issued as a description of current capability until the release gates pass and ADR-010 lands. |
| [`brand-specification.md`](./brand-specification.md), Voice                                                      | Do not reuse "provably anonymous", "quantum-resistant", or "cryptographic no-logs guarantee".                                         |
| [`../adr/adr-004-capability-trust-model.md`](../adr/adr-004-capability-trust-model.md)                           | No production "anonymous" or "zero-knowledge" claim before ADR-010 evidence and external cryptographic review.                        |
| [`../integration/haechi-integration-document.html`](../integration/haechi-integration-document.html), section 16 | Marketing claims must be traceable to test reports, audits, or clearly labelled research objectives.                                  |

[`../engineering/readiness.md`](../engineering/readiness.md) is the controlling
statement of what is actually true. Today Haechi is an engineering prototype:
post-quantum reinforcement is simulated, the capability protocol is a research
implementation, and the gateway data plane is simulated. Nine production
blockers are open.

### Enforcement

`brand/claims.mjs` encodes these restrictions as executable rules. It runs
before Chrome starts, over every string in every post and over the generated
markup, and a `fail` finding aborts the whole run. A prose rule that lives only
in a document gets broken; this one cannot be.

Each rule carries the document that imposes it, so a failure tells the author
what to read:

```
FAIL claim/quantum-resistance at e3-no-silent-downgrade.headline
    match:    quantum-resistant
    context:  ...*Hybrid,* [[quantum-resistant]] never replacement...
    reason:   Post-quantum reinforcement is simulated. Say 'post-quantum
              transition' or 'PQ reinforced' instead.
    citation: dev-docs/brand/brand-specification.md
```

Rules are narrow by design. "Post-quantum transition" and "PQ reinforced" are
approved language and must never trip the quantum rules; only the claim forms
("quantum-resistant", "quantum-safe") are prohibited.

Two terms are `warn` rather than `fail`. "Unlinkable" appears on the public
roadmap as a prototype and is allowed with that framing attached. "VPN" is
flagged because the shipped site deliberately never uses it.

The linter also enforces C-08 of the engineering constitution: zero em dashes,
in post copy and in this document.

### Safe framings

These are supported by authoritative documents and may be used freely:
architectural intent ("designed so that", "the gateway never receives"), the
three named problems, the three engineering pillars, the five-layer
architecture, the ports-and-adapters stance, the telemetry prohibition list,
deny-by-default and fail-closed, no silent downgrade, the three deployment
models, and the engineering-preview status.

### The status badge is mandatory

Every canvas carries "Engineering preview" in its footer. It is part of the
frame, not a per-post choice, and it cannot be switched off. The hero on
haechi.co.uk already states this; an image that travels without its page must
carry it too.

## Canvases

Five sizes. A post is authored once and rendered to all of them.

| Key        | Size      | Mode     | Density | Use                                    |
| ---------- | --------- | -------- | ------- | -------------------------------------- |
| `linkedin` | 1200x627  | `wide`   | 0.79    | LinkedIn single image                  |
| `og`       | 1200x630  | `wide`   | 0.79    | Open Graph and X summary card          |
| `x`        | 1600x900  | `wide`   | 0.88    | X landscape                            |
| `square`   | 1080x1080 | `square` | 1.00    | Square, cross-posting                  |
| `portrait` | 1080x1350 | `tall`   | 1.12    | LinkedIn portrait, and carousel slides |

`og` at 1200x630 is the asset the brand specification lists as missing and
required before public launch. It is now generated from `m1-reveal-less`.

### Scale

Sizes are authored as design units: pixels on a notional 1200px-wide canvas.
Two factors convert them.

- `k = width / 1200` scales geometry. Padding, rules and the mark occupy the
  same proportion of every canvas.
- `t = k * density` scales type. Density is stated per canvas because a
  1200x627 canvas has 40 percent less height than the square one at the same
  width, and identical type overflows it.

Density is explicit per canvas rather than derived from the aspect ratio.
Changing one canvas must not silently change the other four.

## Frame

Identical on every canvas and every post.

```
+--------------------------------------------------+
|  [mark] HAECHI                        [post tag] |
|                                                  |
|  --- EYEBROW                                     |
|  Headline with an *accent phrase*                |
|  Supporting sentence.                            |
|                                                  |
|  <template body>                                 |
|                                                  |
|  haechi.co.uk                * ENGINEERING PREVIEW|
+--------------------------------------------------+
```

- Background is `--black #050506` with the same 64px grid and top-right accent
  arc used by the website hero, scaled by `k`.
- The mark is inlined as SVG. No render fetches anything over the network.
- The eyebrow is preceded by a 4px accent rule and set in accent text.
- A headline may mark one phrase with `*asterisks*` to render it in
  `--accent #FF4500`. This mirrors `<h1>Communications that <span>reveal
less.</span></h1>` on the site. Use it once per headline, on the phrase that
  carries the point.
- The supporting sentence is dropped on `wide` canvases for dense templates,
  where 627px of height cannot hold a headline, a lede and three cards at
  legible sizes. It is always kept for the `statement` template.

## Templates

Four, chosen by what the post has to say rather than by how it should look.

| Template    | Use it when                                  | Wide arrangement           | Square and tall    |
| ----------- | -------------------------------------------- | -------------------------- | ------------------ |
| `statement` | One idea, stated plainly                     | Headline left, steps right | Stacked, centred   |
| `cards`     | Two to four parallel items                   | Columns                    | Stacked            |
| `rows`      | An ordered sequence, typically five          | Two columns                | One column         |
| `split`     | A contrast, especially what is and is not so | Two panels side by side    | Two panels stacked |

`split` panels are tonal. The `positive` panel carries an accent wash and
accent bullet rules; the `negative` panel is a plain panel with muted rules.
Use `positive` for what the system does and `negative` for what it never does.
Exactly two panels, one of each tone.

### Carousels

A post marked `carousel` also renders a portrait slide sequence: a cover
followed by one slide per row. Slides carry a progress strip showing every
layer with the current one lit, mirroring the site's layer map.

Only `e2-five-layers` is a carousel today. A sequence is worth the format when
the content is genuinely ordered and each step needs more room than a single
image can give it.

## The first eight posts

Five educational, three marketing. Every post records the repository paths its
copy came from, and those citations are written into `manifest.json`, which is
how section 16 traceability is satisfied.

| Id                            | Kind        | Template    | Says                                                                |
| ----------------------------- | ----------- | ----------- | ------------------------------------------------------------------- |
| `e1-security-gap`             | educational | `cards`     | The three pressure points: quantum, metadata, centralised trust     |
| `e2-five-layers`              | educational | `rows`      | The five layers and their trust boundaries. Also a 6-slide carousel |
| `e3-no-silent-downgrade`      | educational | `split`     | Hybrid post-quantum reinforcement never downgrades quietly          |
| `e4-privacy-separated-access` | educational | `split`     | What a gateway receives, and what it never receives                 |
| `e5-telemetry-boundary`       | educational | `split`     | The GIM data contract, prohibited against permitted                 |
| `m1-reveal-less`              | marketing   | `statement` | The positioning line and the four audiences                         |
| `m2-operating-models`         | marketing   | `cards`     | Managed, dedicated, restricted                                      |
| `m3-enterprise-pilot`         | marketing   | `statement` | Discovery, deployment, evidence                                     |

Educational posts explain a mechanism and should survive being read by an
engineer who already knows the field. Marketing posts state a position and
should survive being read by a procurement lead who does not.

## Typography

One family, the same stack as the site:
`Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI",
sans-serif`.

The repository ships no font files, so renders currently fall back to the
platform UI face. This is recorded honestly rather than hidden: every render
reports the family it actually resolved, and `manifest.json` records it.

To render in Inter, place the font files in `brand/fonts/`. The renderer embeds
whatever it finds there as data URIs and the probe will report `Inter`. No code
change is needed. Doing this, and self-hosting Inter on the website in the same
change, would close the gap the brand specification already notes.

Type scale, in design units:

| Role      | Size | Line | Weight | Tracking |
| --------- | ---- | ---- | ------ | -------- |
| `display` | 78   | 1.02 | 700    | -0.028em |
| `h1`      | 60   | 1.06 | 700    | -0.024em |
| `h2`      | 46   | 1.10 | 700    | -0.022em |
| `h3`      | 29   | 1.22 | 600    | -0.012em |
| `body`    | 23   | 1.50 | 400    | -0.003em |
| `small`   | 19   | 1.48 | 400    | 0        |
| `eyebrow` | 16   | 1.20 | 600    | +0.18em  |
| `micro`   | 14   | 1.30 | 500    | +0.06em  |
| `index`   | 15   | 1.00 | 700    | +0.14em  |

## Colour

The palette is the one in `brand-specification.md`, unchanged. Three rules
specific to posts:

1. Accent is for emphasis, never for surface. One accent phrase per headline,
   accent rules and indices, one accent-washed panel in a `split`.
2. The withdrawn colours are checked mechanically. `brand/claims.mjs` scans
   generated markup for `#00C4B4`, `#1A1A1A`, `#CCCCCC`, `#FFFFFF` and
   `#000000` and fails the render. The logo mark is exempt, because its inner
   glyph is the one sanctioned use of pure white.
3. The reserved purple and navy have no role here. Do not introduce them.

This check found a real defect during implementation: a `#000` in a mask
gradient. It is now a palette token.

## Imagery

The superseded designer guidelines remain the only imagery guidance, and
nothing in the current specification contradicts them:

- Abstract and conceptual, not literal.
- High contrast, light and shadow.
- No stock photography of people.

The posts follow this by using no photography at all. Every visual element is
drawn from the design system: the grid, the accent arc, the mark, panels and
rules. This is a deliberate constraint, not a limitation. It keeps every asset
reproducible from source and free of licensing questions.

## Verification

Three gates, all runnable.

```bash
npm run brand:test     # 43 unit tests: token drift, claim rules, templates
npm run brand:audit    # measures real layout in Chrome, fails on overflow
npm run brand:posts    # renders all 46 assets and writes manifest.json
```

`brand:test` includes drift guards asserting that the palette still matches
`website/styles.css` and the inlined mark still matches
`website/assets/haechi-mark.svg`. A copy without a test is a copy that will
diverge.

`brand:audit` is the layout gate. Fitting is measured on the same page load
that produces the screenshot, so it is a property of the delivered pixels
rather than of a separate render. It reports headroom per canvas, so it is also
the tool for judging whether copy has room to grow.

`brand:posts` writes `manifest.json` alongside the assets: dimensions, byte
size, SHA-256, resolved font, headroom, and the source citations for each post.

## Adding a post

1. Add an entry to `brand/posts.mjs` with `sources` pointing at the documents
   that support the copy. If you cannot cite one, the claim is not ready.
2. Run `npm run brand:test`. Structural and claim problems surface here.
3. Run `npm run brand:audit`. Fix any overflow by shortening copy or by
   adjusting the template, not by shrinking type below the scale.
4. Run `npm run brand:posts` and look at the result on all five canvases.

## Where the output goes

`brand-assets/posts/`, which is git-ignored. The assets are fully derived from
committed source, so they are regenerated rather than stored, consistent with
how the rest of `brand-assets/` already works. `manifest.json` records what a
given run produced.

## Keeping this honest

The claim rules in `brand/claims.mjs` are a snapshot of four documents that
will change. When ADR-010 lands and defines permitted claims, the rules that
cite ADR-004 must be revisited in the same change, and the corresponding
prohibitions relaxed only as far as the evidence supports. Until then they stay
as they are.
