# Visual post generator

Generates the Haechi educational and marketing posts as PNGs, from post copy
held as data. The rules it follows are in
[`dev-docs/brand/visual-content-specification.md`](../dev-docs/brand/visual-content-specification.md);
this file covers running it.

## Commands

```bash
npm run brand:test     # unit tests, no browser needed, under a second of work
npm run brand:audit    # measure real layout in Chrome, fail on overflow
npm run brand:posts    # render 46 assets to brand-assets/posts/
```

Useful flags on `node brand/render.mjs`:

| Flag           | Effect                                                   |
| -------------- | -------------------------------------------------------- |
| `--list`       | Print the render plan and exit. No browser is launched.  |
| `--only=ID`    | One post, for example `--only=e2-five-layers`.           |
| `--canvas=KEY` | One canvas: `linkedin`, `og`, `x`, `square`, `portrait`. |
| `--scale=N`    | Device scale factor, 1 to 4. Use `2` for retina exports. |
| `--keep-html`  | Keep the generated HTML in `brand-assets/posts/html/`.   |
| `--audit`      | Measure layout, write nothing.                           |
| `--out=DIR`    | Write somewhere other than `brand-assets/posts/`.        |

## Requirements

Node 20 or later, and a Chrome-family browser. The renderer looks in the usual
macOS and Linux locations; set `CHROME` to override:

```bash
CHROME=/usr/bin/chromium npm run brand:posts
```

No npm dependency is added. Chrome is driven over the DevTools Protocol using
the `WebSocket` global that Node 22 and later provide.

## Layout

| File              | Holds                                                      |
| ----------------- | ---------------------------------------------------------- |
| `tokens.mjs`      | Palette, type scale, spacing, font stack, the inlined mark |
| `canvases.mjs`    | The five canvases and the design-unit to pixel scale       |
| `claims.mjs`      | The claim-safety linter and the withdrawn-colour check     |
| `posts.mjs`       | The eight posts, as data, with source citations            |
| `templates.mjs`   | HTML and CSS for the frame and the four templates          |
| `cdp.mjs`         | Minimal Chrome DevTools Protocol client                    |
| `render.mjs`      | Orchestration, claim gate, capture, manifest               |
| `render.test.mjs` | Unit tests                                                 |
| `fonts/`          | Optional. Drop Inter here to embed it. Absent by default.  |

Output goes to `brand-assets/posts/`, which is git-ignored: the assets are
derived from committed source, so they are regenerated rather than stored.

## How it works

1. `posts.mjs` is read and every string is linted by `claims.mjs`. A prohibited
   claim, a withdrawn palette colour or an em dash aborts the run before Chrome
   starts.
2. `templates.mjs` builds one self-contained HTML document per post per canvas.
   Nothing is fetched over the network: the mark is inlined and any fonts are
   embedded as data URIs.
3. `cdp.mjs` launches one Chrome instance for the whole run and loads each
   document in turn.
4. Each page is measured and captured on the same load, so "it fits" describes
   the delivered pixels. Overflow fails the run.
5. PNG dimensions are checked against the canvas, then `manifest.json` is
   written with sizes, hashes, resolved font, headroom and source citations.

## Fonts

The repository ships no font files, so renders fall back to the platform UI
face and every run reports which family it resolved:

```
resolved font family: platform-fallback
```

Put Inter `.woff2` files in `brand/fonts/` and they are embedded automatically,
with weights inferred from the file names. The report then reads `Inter`.

## Two things that will waste your time if you change them

`--disable-background-networking` must not be added to the Chrome flags. With a
fresh `--user-data-dir` it makes headless Chrome hang on startup with no error.

`--virtual-time-budget` must not be combined with a screenshot capture. Virtual
time never reaches the budget while a capture is pending, and Chrome hangs.

Both are recorded in comments at the point of use in `cdp.mjs`.
