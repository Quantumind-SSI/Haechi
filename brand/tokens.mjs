/**
 * Design tokens for Haechi visual posts.
 *
 * Every colour here is copied from `dev-docs/brand/brand-specification.md`,
 * which is itself a copy of `website/styles.css`. `render.test.mjs` asserts
 * that the tokens marked `stylesheet: true` still match the stylesheet, so
 * this file cannot silently drift from the shipped product.
 *
 * Sizes are expressed in *design units*: pixels on a notional 1200px-wide
 * canvas. `canvases.mjs` multiplies them by a per-canvas scale factor.
 */

/**
 * Canonical palette. Keys mirror the CSS custom properties where one exists.
 *
 * @type {Readonly<Record<string, string>>}
 */
export const COLOUR = Object.freeze({
  black: "#050506",
  ink: "#09090a",
  panel: "#0f0f11",
  panelRaised: "#151517",
  gradientStop: "#080809",
  panelAlt: "#0d0d0f",
  white: "#f7f7f3",
  muted: "#aaa9a3",
  prose: "#d4d3ce",
  tertiary: "#777671",
  accent: "#ff4500",
  accentBright: "#ff6a32",
  accentSoft: "rgba(255, 69, 0, 0.14)",
  accentBorder: "#ff8b5e",
  accentText: "#ff9b74",
  line: "rgba(255, 255, 255, 0.12)",
  lineStrong: "rgba(255, 255, 255, 0.22)",
  success: "#8dfca4",
});

/**
 * Tokens that must be byte-identical to a `:root` custom property in
 * `website/styles.css`. Maps token name to CSS custom property name.
 *
 * @type {Readonly<Record<string, string>>}
 */
export const STYLESHEET_PARITY = Object.freeze({
  black: "--black",
  ink: "--ink",
  panel: "--panel",
  panelRaised: "--panel-raised",
  white: "--white",
  muted: "--muted",
  accent: "--accent",
  accentBright: "--accent-bright",
  accentSoft: "--accent-soft",
  line: "--line",
  lineStrong: "--line-strong",
  success: "--success",
});

/**
 * Colours withdrawn by the brand specification, 2026-08-30. A post that uses
 * any of these fails the render. `#ffffff` is permitted only inside the logo
 * mark, which `claims.mjs` exempts by name.
 *
 * @type {readonly string[]}
 */
export const WITHDRAWN_COLOURS = Object.freeze([
  "#00c4b4",
  "#1a1a1a",
  "#cccccc",
  "#ffffff",
  "#000000",
]);

/**
 * Type scale in design units.
 *
 * `size` is px at scale 1. `line` is a unitless line-height multiplier.
 * `track` is letter-spacing as a fraction of the font size.
 *
 * @type {Readonly<Record<string, {size: number, line: number, weight: number, track: number}>>}
 */
export const TYPE = Object.freeze({
  display: { size: 78, line: 1.02, weight: 700, track: -0.028 },
  h1: { size: 60, line: 1.06, weight: 700, track: -0.024 },
  h2: { size: 46, line: 1.1, weight: 700, track: -0.022 },
  h3: { size: 29, line: 1.22, weight: 600, track: -0.012 },
  body: { size: 23, line: 1.5, weight: 400, track: -0.003 },
  small: { size: 19, line: 1.48, weight: 400, track: 0 },
  eyebrow: { size: 16, line: 1.2, weight: 600, track: 0.18 },
  micro: { size: 14, line: 1.3, weight: 500, track: 0.06 },
  index: { size: 15, line: 1, weight: 700, track: 0.14 },
});

/**
 * Spacing and geometry in design units. `radius` is 4px per the brand
 * specification: sharp, near-square corners.
 *
 * @type {Readonly<Record<string, number>>}
 */
export const SPACE = Object.freeze({
  pad: 76,
  gap: 26,
  gapLarge: 42,
  rule: 4,
  radius: 4,
  markSize: 40,
  gridCell: 64,
  hairline: 1,
});

/**
 * Font stack, identical to `body` in `website/styles.css`.
 *
 * Inter is requested first but the repository ships no font files, so a
 * renderer without Inter installed falls back to the platform UI face. The
 * renderer reports which family it actually resolved; see `brand/README.md`.
 *
 * @type {string}
 */
export const FONT_STACK =
  'Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';

/**
 * The approved mark, inlined so a render never depends on a network fetch or
 * a relative path. Byte-identical to `website/assets/haechi-mark.svg` with the
 * accessibility attributes dropped, because a decorative mark inside a raster
 * export carries no accessible name. `render.test.mjs` asserts the three path
 * `d` attributes still match the shipped file.
 *
 * @type {string}
 */
export const MARK_SVG = [
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" aria-hidden="true">',
  '<path d="M32 4 55 14v16c0 14.4-8.9 25.6-23 30C17.9 55.6 9 44.4 9 30V14L32 4Z" fill="#ff4500"/>',
  '<path d="M32 11.5 48 18v11.6c0 9.9-5.7 17.9-16 22.1-10.3-4.2-16-12.2-16-22.1V18l16-6.5Z" fill="#0b0b0c"/>',
  '<path d="m22 35 10-16 10 16-5.7-2.1L32 41l-4.3-8.1L22 35Z" fill="#fff"/>',
  "</svg>",
].join("");
