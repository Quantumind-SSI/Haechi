/**
 * HTML and CSS generation for Haechi visual posts.
 *
 * Posts are laid out with Chrome's own engine rather than hand-positioned SVG
 * text, because real line breaking is the difference between copy that fits
 * five canvases and copy that fits one. Every measurement is a scaled design
 * unit from `canvases.mjs`, emitted as a CSS custom property so the rules below
 * stay readable.
 *
 * The frame is identical on every canvas and every post. It always carries the
 * mark, the wordmark, the post tag, the domain, and an "Engineering preview"
 * badge. The badge is not optional: `readiness.md` classifies Haechi as a
 * prototype, so any asset that leaves this repository states that on its face.
 */

import assert from "node:assert/strict";
import { metrics } from "./canvases.mjs";
import { COLOUR, FONT_STACK, MARK_SVG, TYPE } from "./tokens.mjs";

/**
 * Escape text for insertion into HTML character data or a double-quoted
 * attribute.
 *
 * @param {string} text
 * @returns {string}
 */
export function escapeHtml(text) {
  assert.equal(typeof text, "string", "escapeHtml expects a string");
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

/**
 * Apply the accent-emphasis convention: `*phrase*` becomes an accent span.
 * Mirrors the `<h1>Communications that <span>reveal less.</span></h1>`
 * treatment on the shipped site.
 *
 * Must be called on already-escaped text; `*` is not an HTML metacharacter, so
 * escaping first is safe.
 *
 * @param {string} escaped Output of {@link escapeHtml}.
 * @returns {string} HTML fragment.
 */
export function emphasise(escaped) {
  return escaped.replace(/\*([^*]+)\*/g, '<span class="hl">$1</span>');
}

/**
 * Escape then emphasise, the combination used for every headline.
 *
 * @param {string} text
 * @returns {string}
 */
function headlineHtml(text) {
  return emphasise(escapeHtml(text));
}

/**
 * Build the CSS custom property block for one canvas.
 *
 * @param {{w: number, h: number}} canvas
 * @param {ReturnType<typeof metrics>} m
 * @returns {string} Declarations for a `:root` block.
 */
function cssVars(canvas, m) {
  const type = Object.keys(TYPE)
    .map((name) => {
      const s = m.type[name];
      return `--fs-${name}:${s.size}px;--lh-${name}:${s.line};--fw-${name}:${s.weight};--tr-${name}:${s.track}px;`;
    })
    .join("");
  const space = Object.entries(m.space)
    .map(([name, value]) => `--sp-${name}:${value}px;`)
    .join("");
  const colour = Object.entries(COLOUR)
    .map(([name, value]) => `--c-${name}:${value};`)
    .join("");
  return `--w:${canvas.w}px;--h:${canvas.h}px;--k:${m.k};${type}${space}${colour}`;
}

/** Static stylesheet. All sizing comes from the custom properties above. */
const BASE_CSS = `
*{box-sizing:border-box;margin:0;padding:0}
html,body{width:var(--w);height:var(--h);background:var(--c-black)}
body{font-family:${FONT_STACK};-webkit-font-smoothing:antialiased;text-rendering:geometricPrecision}
.canvas{position:relative;width:var(--w);height:var(--h);overflow:hidden;
  background:radial-gradient(120% 90% at 78% 8%, var(--c-gradientStop) 0%, var(--c-black) 62%)}
.grid{position:absolute;inset:0;opacity:.23;
  background-image:linear-gradient(rgba(255,255,255,.08) 1px,transparent 1px),
    linear-gradient(90deg,rgba(255,255,255,.08) 1px,transparent 1px);
  background-size:calc(64px * var(--k)) calc(64px * var(--k));
  -webkit-mask-image:linear-gradient(to bottom,var(--c-black),transparent 88%)}
.glow{position:absolute;width:calc(620px * var(--k));height:calc(620px * var(--k));
  right:calc(-330px * var(--k));top:calc(-90px * var(--k));border:var(--sp-hairline) solid rgba(255,69,0,.14);
  border-radius:50%;box-shadow:0 0 calc(160px * var(--k)) rgba(255,69,0,.1)}
.content{position:relative;display:flex;flex-direction:column;height:100%;padding:var(--sp-pad)}
header,footer{display:flex;align-items:center;justify-content:space-between;flex:0 0 auto}
.brand{display:flex;align-items:center;gap:calc(var(--sp-gap) * .48)}
.brand svg{width:var(--sp-markSize);height:var(--sp-markSize);display:block}
.brand b{font-size:var(--fs-eyebrow);font-weight:700;letter-spacing:calc(var(--tr-eyebrow) * 1.15);
  color:var(--c-white);text-transform:uppercase}
.tag{font-size:var(--fs-micro);font-weight:var(--fw-micro);letter-spacing:var(--tr-micro);
  text-transform:uppercase;color:var(--c-muted);border:var(--sp-hairline) solid var(--c-line);
  border-radius:var(--sp-radius);padding:calc(var(--sp-gap) * .3) calc(var(--sp-gap) * .55)}
main{flex:1 1 auto;display:flex;flex-direction:column;justify-content:center;
  padding:calc(var(--sp-gapLarge) * .85) 0;overflow:hidden;min-height:0}
footer{gap:var(--sp-gap);font-size:var(--fs-micro);letter-spacing:var(--tr-micro);color:var(--c-tertiary)}
footer .url{color:var(--c-muted);font-weight:600}
.status{display:flex;align-items:center;gap:calc(var(--sp-gap) * .34);text-transform:uppercase}
.status i{width:calc(var(--sp-gap) * .3);height:calc(var(--sp-gap) * .3);border-radius:50%;
  background:var(--c-accent);box-shadow:0 0 calc(var(--sp-gap) * .5) var(--c-accent)}
.eyebrow{display:flex;align-items:center;gap:calc(var(--sp-gap) * .45);font-size:var(--fs-eyebrow);
  font-weight:var(--fw-eyebrow);letter-spacing:var(--tr-eyebrow);text-transform:uppercase;color:var(--c-accentText)}
.eyebrow::before{content:"";width:calc(var(--sp-gapLarge) * .8);height:var(--sp-rule);
  background:var(--c-accent);border-radius:var(--sp-radius);flex:0 0 auto}
h1{font-size:var(--fs-h1);line-height:var(--lh-h1);font-weight:var(--fw-h1);letter-spacing:var(--tr-h1);
  color:var(--c-white);text-wrap:balance}
h1 .hl{color:var(--c-accent)}
.lede{font-size:var(--fs-body);line-height:var(--lh-body);letter-spacing:var(--tr-body);color:var(--c-prose);max-width:62ch}
.headblock{display:flex;flex-direction:column;gap:calc(var(--sp-gap) * .72)}
`;

/** Template-specific rules, appended after {@link BASE_CSS}. */
const TEMPLATE_CSS = `
.cards{display:grid;gap:var(--sp-gap)}
.card{display:flex;flex-direction:column;gap:calc(var(--sp-gap) * .5);background:var(--c-panel);
  border:var(--sp-hairline) solid var(--c-line);border-radius:var(--sp-radius);
  padding:calc(var(--sp-gap) * .95);min-width:0}
.card .label{font-size:var(--fs-index);font-weight:var(--fw-index);letter-spacing:var(--tr-index);
  text-transform:uppercase;color:var(--c-accent)}
.card h2{font-size:var(--fs-h3);line-height:var(--lh-h3);font-weight:var(--fw-h3);
  letter-spacing:var(--tr-h3);color:var(--c-white)}
.card p{font-size:var(--fs-small);line-height:var(--lh-small);color:var(--c-prose);flex:1 1 auto}
.card .note{font-size:var(--fs-micro);letter-spacing:var(--tr-micro);text-transform:uppercase;
  color:var(--c-tertiary);border-top:var(--sp-hairline) solid var(--c-line);padding-top:calc(var(--sp-gap) * .5)}

.rows{display:grid;gap:calc(var(--sp-gap) * .8)}
.row{display:grid;grid-template-columns:auto 1fr;gap:calc(var(--sp-gap) * .7);align-items:start;
  border-top:var(--sp-hairline) solid var(--c-line);padding-top:calc(var(--sp-gap) * .6);min-width:0}
.row .label{font-size:var(--fs-index);font-weight:var(--fw-index);letter-spacing:var(--tr-index);
  color:var(--c-accent);padding-top:calc(var(--sp-gap) * .12)}
.row h2{font-size:var(--fs-h3);line-height:var(--lh-h3);font-weight:var(--fw-h3);
  letter-spacing:var(--tr-h3);color:var(--c-white)}
.row p{font-size:var(--fs-small);line-height:var(--lh-small);color:var(--c-muted);
  margin-top:calc(var(--sp-gap) * .22)}

.split{display:grid;gap:var(--sp-gap)}
.panel{display:flex;flex-direction:column;gap:calc(var(--sp-gap) * .55);border-radius:var(--sp-radius);
  padding:calc(var(--sp-gap) * .95);border:var(--sp-hairline) solid var(--c-line);background:var(--c-panel);min-width:0}
.panel.positive{border-color:rgba(255,69,0,.34);background:var(--c-accentSoft)}
.panel .label{font-size:var(--fs-micro);font-weight:600;letter-spacing:var(--tr-micro);
  text-transform:uppercase;color:var(--c-muted)}
.panel.positive .label{color:var(--c-accentText)}
.panel h2{font-size:var(--fs-h3);line-height:var(--lh-h3);font-weight:var(--fw-h3);
  letter-spacing:var(--tr-h3);color:var(--c-white)}
.panel ul{list-style:none;display:flex;flex-direction:column;gap:calc(var(--sp-gap) * .42)}
.panel li{position:relative;padding-left:calc(var(--sp-gap) * .8);font-size:var(--fs-small);
  line-height:var(--lh-small);color:var(--c-prose)}
.panel li::before{content:"";position:absolute;left:0;top:calc(var(--fs-small) * .62);
  width:calc(var(--sp-gap) * .38);height:var(--sp-hairline);background:var(--c-lineStrong)}
.panel.positive li::before{background:var(--c-accent)}
.panel.negative li::before{background:var(--c-tertiary)}

.strip{display:flex;flex-wrap:wrap;gap:calc(var(--sp-gap) * .5);align-items:center}
.strip span{font-size:var(--fs-micro);letter-spacing:var(--tr-micro);text-transform:uppercase;
  color:var(--c-muted);border:var(--sp-hairline) solid var(--c-line);border-radius:var(--sp-radius);
  padding:calc(var(--sp-gap) * .3) calc(var(--sp-gap) * .55)}
.steps{display:grid;gap:var(--sp-gap)}
.step{border-top:var(--sp-rule) solid var(--c-accent);padding-top:calc(var(--sp-gap) * .55)}
.step .label{font-size:var(--fs-index);font-weight:var(--fw-index);letter-spacing:var(--tr-index);color:var(--c-accent)}
.step h2{font-size:var(--fs-h3);line-height:var(--lh-h3);font-weight:var(--fw-h3);
  letter-spacing:var(--tr-h3);color:var(--c-white);margin-top:calc(var(--sp-gap) * .2)}
.step p{font-size:var(--fs-small);line-height:var(--lh-small);color:var(--c-muted);margin-top:calc(var(--sp-gap) * .16)}
.statement{display:flex;flex-direction:column;gap:var(--sp-gapLarge);justify-content:center;height:100%}
.statement h1{font-size:var(--fs-display);line-height:var(--lh-display);letter-spacing:var(--tr-display)}

.slide-index{font-size:var(--fs-micro);letter-spacing:var(--tr-micro);color:var(--c-tertiary);text-transform:uppercase}
.swipe{display:flex;align-items:center;gap:calc(var(--sp-gap) * .4);font-size:var(--fs-micro);
  letter-spacing:var(--tr-micro);text-transform:uppercase;color:var(--c-accentText)}
.layermap{display:flex;align-items:center;gap:calc(var(--sp-gap) * .34)}
.layermap span{font-size:var(--fs-micro);font-weight:600;letter-spacing:var(--tr-micro);color:var(--c-tertiary);
  border:var(--sp-hairline) solid var(--c-line);border-radius:var(--sp-radius);
  padding:calc(var(--sp-gap) * .26) calc(var(--sp-gap) * .46)}
.layermap span.on{color:var(--c-white);border-color:var(--c-accent);background:var(--c-accentSoft)}
.layermap i{flex:1 1 auto;height:var(--sp-hairline);background:var(--c-line)}
.slide{display:flex;flex-direction:column;gap:var(--sp-gapLarge);justify-content:center;height:100%}
.slide h1{font-size:var(--fs-display);line-height:var(--lh-display);letter-spacing:var(--tr-display)}
.slide .spacer{flex:0 1 auto;min-height:0}
`;

/**
 * Arrangement rules keyed by canvas mode.
 *
 * `wide` also reclaims vertical space. A 1200x627 canvas has 40 percent less
 * height than the square one at the same width, so the frame padding, the main
 * gutter and the card padding all tighten. Without this every dense template
 * overflows, which `--audit` proves rather than leaving to the eye.
 */
const MODE_CSS = Object.freeze({
  wide: `
.content{padding:calc(var(--sp-pad) * .6) var(--sp-pad)}
main{padding:calc(var(--sp-gapLarge) * .45) 0;gap:calc(var(--sp-gapLarge) * .62)}
.cards{grid-template-columns:repeat(var(--cols),1fr);align-content:center}
.card{padding:calc(var(--sp-gap) * .78);gap:calc(var(--sp-gap) * .38)}
.card .note{padding-top:calc(var(--sp-gap) * .38)}
.rows{grid-template-columns:1fr 1fr;column-gap:var(--sp-gapLarge);row-gap:calc(var(--sp-gap) * .6);align-content:center}
.split{grid-template-columns:1fr 1fr;align-content:center}
.panel{padding:calc(var(--sp-gap) * .8);gap:calc(var(--sp-gap) * .42)}
.panel ul{gap:calc(var(--sp-gap) * .34)}
.steps{grid-template-columns:repeat(3,1fr)}
.headblock{gap:calc(var(--sp-gap) * .55)}
.hero-split{display:grid;grid-template-columns:1.15fr .85fr;gap:var(--sp-gapLarge);align-items:center;height:100%}
`,
  square: `
main{padding:calc(var(--sp-gapLarge) * .4) 0;gap:calc(var(--sp-gapLarge) * .62)}
.cards{grid-template-columns:1fr;align-content:center;gap:calc(var(--sp-gap) * .66)}
.card{padding:calc(var(--sp-gap) * .82);gap:calc(var(--sp-gap) * .38)}
.card .note{padding-top:calc(var(--sp-gap) * .4)}
.rows{grid-template-columns:1fr;align-content:center}
.split{grid-template-columns:1fr}
.steps{grid-template-columns:1fr}
.hero-split{display:flex;flex-direction:column;gap:var(--sp-gapLarge);justify-content:center;height:100%}
`,
  tall: `
.cards{grid-template-columns:1fr;align-content:center}
.rows{grid-template-columns:1fr;align-content:center}
.split{grid-template-columns:1fr}
.steps{grid-template-columns:1fr}
main{gap:var(--sp-gapLarge)}
.hero-split{display:flex;flex-direction:column;gap:var(--sp-gapLarge);justify-content:center;height:100%}
`,
});

/**
 * Overflow probe. Chrome runs this before `--screenshot` captures, and
 * `render.mjs --audit` reads the resulting attribute out of `--dump-dom`.
 * Purely diagnostic: it never changes layout.
 */
const PROBE_SCRIPT = `
<script>
(function(){
  function resolves(name){
    var c=document.createElement('canvas').getContext('2d');
    var s='HAECHIabcdefghijk0123456789';
    return ['monospace','serif','sans-serif'].some(function(base){
      c.font='700 72px '+base;
      var plain=c.measureText(s).width;
      c.font='700 72px "'+name+'",'+base;
      return c.measureText(s).width!==plain;
    });
  }
  var m=document.querySelector('main');
  var c=document.querySelector('.content');
  var over=m.scrollHeight>m.clientHeight+1||c.scrollHeight>c.clientHeight+1;
  var e=document.documentElement;
  e.setAttribute('data-overflow',over?'true':'false');
  // scrollHeight is clamped at clientHeight, so it can prove overflow but never
  // measures spare room. The union of the children's boxes can do both, and it
  // is template independent: some layouts wrap everything in one element and
  // some place several siblings directly in main.
  var top=Infinity, bottom=-Infinity;
  for(var i=0;i<m.children.length;i++){
    var r=m.children[i].getBoundingClientRect();
    if(r.height===0&&r.width===0) continue;
    top=Math.min(top,r.top); bottom=Math.max(bottom,r.bottom);
  }
  var used=bottom>top?Math.ceil(bottom-top):m.scrollHeight;
  e.setAttribute('data-headroom',String(m.clientHeight-used));
  e.setAttribute('data-font',resolves('Inter')?'Inter':'platform-fallback');
})();
</script>`;

/**
 * Whether the supporting sentence is shown. Dense templates lose it on `wide`
 * canvases, where 627px of height cannot hold a headline, a lede and three
 * cards at legible sizes.
 *
 * @param {{template: string}} post
 * @param {{mode: string}} canvas
 * @returns {boolean}
 */
function showLede(post, canvas) {
  return canvas.mode !== "wide" || post.template === "statement";
}

/**
 * Eyebrow, headline and optional lede, shared by every template.
 *
 * @param {{eyebrow: string, headline: string, lede: string, template: string}} post
 * @param {{mode: string}} canvas
 * @returns {string}
 */
function headBlock(post, canvas) {
  const lede = showLede(post, canvas)
    ? `<p class="lede">${escapeHtml(post.lede)}</p>`
    : "";
  return `<div class="headblock"><p class="eyebrow">${escapeHtml(post.eyebrow)}</p>
<h1>${headlineHtml(post.headline)}</h1>${lede}</div>`;
}

/**
 * @param {import("./posts.mjs").Post} post
 * @param {{mode: string}} canvas
 * @returns {string}
 */
function renderCards(post, canvas) {
  assert.ok(
    Array.isArray(post.cards),
    `${post.id}: cards template needs cards`,
  );
  const cards = post.cards
    .map(
      (
        c,
      ) => `<article class="card"><span class="label">${escapeHtml(c.label)}</span>
<h2>${escapeHtml(c.title)}</h2><p>${escapeHtml(c.body)}</p>
<span class="note">${escapeHtml(c.note)}</span></article>`,
    )
    .join("");
  return `${headBlock(post, canvas)}<div class="cards" style="--cols:${post.cards.length}">${cards}</div>`;
}

/**
 * @param {import("./posts.mjs").Post} post
 * @param {{mode: string}} canvas
 * @returns {string}
 */
function renderRows(post, canvas) {
  assert.ok(Array.isArray(post.rows), `${post.id}: rows template needs rows`);
  const rows = post.rows
    .map(
      (r) => `<div class="row"><span class="label">${escapeHtml(r.label)}</span>
<div><h2>${escapeHtml(r.title)}</h2><p>${escapeHtml(r.body)}</p></div></div>`,
    )
    .join("");
  return `${headBlock(post, canvas)}<div class="rows">${rows}</div>`;
}

/**
 * @param {import("./posts.mjs").Post} post
 * @param {{mode: string}} canvas
 * @returns {string}
 */
function renderSplit(post, canvas) {
  assert.ok(
    Array.isArray(post.panels) && post.panels.length === 2,
    `${post.id}: split template needs exactly two panels`,
  );
  const panels = post.panels
    .map((p) => {
      const items = p.items.map((i) => `<li>${escapeHtml(i)}</li>`).join("");
      return `<section class="panel ${p.tone}"><span class="label">${escapeHtml(p.label)}</span>
<h2>${escapeHtml(p.title)}</h2><ul>${items}</ul></section>`;
    })
    .join("");
  return `${headBlock(post, canvas)}<div class="split">${panels}</div>`;
}

/**
 * @param {import("./posts.mjs").Post} post
 * @param {{mode: string}} canvas
 * @returns {string}
 */
function renderStatement(post, canvas) {
  const strip = Array.isArray(post.strip)
    ? `<div class="strip">${post.strip.map((s) => `<span>${escapeHtml(s)}</span>`).join("")}</div>`
    : "";
  const steps = Array.isArray(post.points)
    ? `<div class="steps">${post.points
        .map(
          (
            p,
          ) => `<div class="step"><span class="label">${escapeHtml(p.label)}</span>
<h2>${escapeHtml(p.title)}</h2><p>${escapeHtml(p.body)}</p></div>`,
        )
        .join("")}</div>`
    : "";
  const head = `<div class="headblock"><p class="eyebrow">${escapeHtml(post.eyebrow)}</p>
<h1>${headlineHtml(post.headline)}</h1><p class="lede">${escapeHtml(post.lede)}</p></div>`;
  if (canvas.mode === "wide" && steps !== "") {
    return `<div class="hero-split">${head}${steps}</div>`;
  }
  return `<div class="statement">${head}${strip}${steps}</div>`;
}

/** Template dispatch table. Bounded indirection, one entry per template. */
const TEMPLATES = Object.freeze({
  cards: renderCards,
  rows: renderRows,
  split: renderSplit,
  statement: renderStatement,
});

/**
 * Render the body of a post for one canvas.
 *
 * @param {import("./posts.mjs").Post} post
 * @param {{mode: string}} canvas
 * @returns {string}
 * @throws {Error} If the post names a template that does not exist.
 */
export function renderBody(post, canvas) {
  const template = TEMPLATES[post.template];
  if (template === undefined) {
    throw new Error(
      `${post.id}: unknown template '${post.template}', expected one of ${Object.keys(TEMPLATES).join(", ")}`,
    );
  }
  return template(post, canvas);
}

/**
 * Progress strip for the carousel: one chip per layer joined by rules, with
 * the current layer lit. It mirrors the `.layer-map` element on the shipped
 * site, tells a reader where they are in the sequence, and gives the tall
 * portrait canvas something to do with its lower third.
 *
 * @param {import("./posts.mjs").Post} post
 * @param {number} active Zero-based index of the current layer, or -1 on the cover.
 * @returns {string}
 */
function layerMap(post, active) {
  const chips = post.rows.map((row, i) => {
    const chip = `<span${i === active ? ' class="on"' : ""}>${escapeHtml(row.label)}</span>`;
    return i < post.rows.length - 1 ? `${chip}<i></i>` : chip;
  });
  return `<div class="layermap">${chips.join("")}</div>`;
}

/**
 * Body for one carousel slide. Slide 0 is the cover; slides 1..n are one row
 * each, expanded with the supporting points that the single-image layouts have
 * no room for.
 *
 * @param {import("./posts.mjs").Post} post
 * @param {number} slide Zero-based slide index.
 * @returns {string}
 * @throws {AssertionError} If the slide index is out of range.
 */
export function renderSlideBody(post, slide) {
  assert.ok(Array.isArray(post.rows), `${post.id}: carousel needs rows`);
  assert.ok(
    Number.isInteger(slide) && slide >= 0 && slide <= post.rows.length,
    `${post.id}: slide ${slide} out of range`,
  );
  if (slide === 0) {
    return `<div class="slide"><div class="headblock">
<p class="eyebrow">${escapeHtml(post.eyebrow)}</p><h1>${headlineHtml(post.headline)}</h1>
<p class="lede">${escapeHtml(post.lede)}</p></div>
<div class="swipe">Swipe through the five layers <span>&#8594;</span></div>
${layerMap(post, -1)}</div>`;
  }
  const row = post.rows[slide - 1];
  const items = row.points.map((p) => `<li>${escapeHtml(p)}</li>`).join("");
  return `<div class="slide"><div class="headblock">
<p class="eyebrow">Layer ${escapeHtml(row.label)}</p><h1>${escapeHtml(row.title)}</h1>
<p class="lede">${escapeHtml(row.body)}</p></div>
<section class="panel positive"><span class="label">In this layer</span><ul>${items}</ul></section>
${layerMap(post, slide - 1)}</div>`;
}

/**
 * Assemble a complete, self-contained HTML document for one post on one
 * canvas. No external requests: the mark is inlined and the stylesheet is
 * embedded, so a render never depends on the network.
 *
 * @param {import("./posts.mjs").Post} post
 * @param {{key: string, w: number, h: number, mode: string, density: number}} canvas
 * @param {{slide?: number, slideCount?: number, fontCss?: string}} [options]
 *   Carousel position when rendering a slide, and any `@font-face` rules to
 *   embed. `fontCss` is produced by `render.mjs` from `brand/fonts/` and is an
 *   empty string when no font files are present, in which case the stack in
 *   `tokens.mjs` falls back to the platform UI face.
 * @returns {string} A full HTML document.
 */
export function renderDocument(post, canvas, options = {}) {
  const m = metrics(canvas);
  const fontCss = typeof options.fontCss === "string" ? options.fontCss : "";
  const isSlide = typeof options.slide === "number";
  const body = isSlide
    ? renderSlideBody(post, options.slide)
    : renderBody(post, canvas);
  const corner = isSlide
    ? `<span class="slide-index">${options.slide + 1} / ${options.slideCount}</span>`
    : `<span class="tag">${escapeHtml(post.tag)}</span>`;
  const title = `${post.id}${isSlide ? ` slide ${options.slide + 1}` : ""} ${canvas.key}`;
  return `<!doctype html>
<html lang="en-GB"><head><meta charset="utf-8">
<title>${escapeHtml(title)}</title>
<style>${fontCss}:root{${cssVars(canvas, m)}}${BASE_CSS}${TEMPLATE_CSS}${MODE_CSS[canvas.mode]}</style>
</head><body><div class="canvas"><div class="grid"></div><div class="glow"></div>
<div class="content">
<header><span class="brand">${MARK_SVG}<b>Haechi</b></span>${corner}</header>
<main>${body}</main>
<footer><span class="url">haechi.co.uk</span>
<span class="status"><i></i>Engineering preview</span></footer>
</div></div>${PROBE_SCRIPT}</body></html>`;
}
