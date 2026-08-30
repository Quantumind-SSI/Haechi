/**
 * Canvas definitions and the design-unit to pixel scale.
 *
 * A post is authored once and rendered to every canvas. Two independent scale
 * factors keep that honest:
 *
 * - `k` scales geometry (padding, rules, the mark) with canvas width, so the
 *   frame occupies the same proportion of every canvas.
 * - `t` scales type. It is `k` multiplied by an explicit per-canvas density,
 *   because a 1200x627 canvas has far less vertical room than a 1080x1350 one
 *   and identical type would overflow it.
 *
 * Density is stated per canvas rather than derived from the aspect ratio.
 * Explicit beats clever: a designer changing one canvas must not silently
 * change the other four.
 */

import assert from "node:assert/strict";
import { SPACE, TYPE } from "./tokens.mjs";

/** Width of the notional canvas that design units are expressed against. */
export const REFERENCE_WIDTH = 1200;

/**
 * The five approved export canvases.
 *
 * `mode` selects the template arrangement:
 * - `wide`   aspect >= 1.5, horizontal arrangements, tightest vertical budget
 * - `square` aspect ~1, stacked arrangements
 * - `tall`   aspect < 1, stacked with the most vertical room
 *
 * @type {ReadonlyArray<{key: string, w: number, h: number, mode: string, density: number, label: string}>}
 */
export const CANVASES = Object.freeze([
  Object.freeze({
    key: "linkedin",
    w: 1200,
    h: 627,
    mode: "wide",
    density: 0.79,
    label: "LinkedIn single image",
  }),
  Object.freeze({
    key: "og",
    w: 1200,
    h: 630,
    mode: "wide",
    density: 0.79,
    label: "Open Graph share card",
  }),
  Object.freeze({
    key: "x",
    w: 1600,
    h: 900,
    mode: "wide",
    density: 0.88,
    label: "X landscape",
  }),
  Object.freeze({
    key: "square",
    w: 1080,
    h: 1080,
    mode: "square",
    density: 1.0,
    label: "Square",
  }),
  Object.freeze({
    key: "portrait",
    w: 1080,
    h: 1350,
    mode: "tall",
    density: 1.02,
    label: "LinkedIn portrait and carousel",
  }),
]);

/** @type {ReadonlyArray<string>} */
export const CANVAS_KEYS = Object.freeze(CANVASES.map((c) => c.key));

/**
 * Look up a canvas by key.
 *
 * @param {string} key One of {@link CANVAS_KEYS}.
 * @returns {{key: string, w: number, h: number, mode: string, density: number, label: string}}
 * @throws {Error} If the key is not a defined canvas.
 */
export function canvasByKey(key) {
  const found = CANVASES.find((c) => c.key === key);
  if (found === undefined) {
    throw new Error(
      `unknown canvas '${key}', expected one of ${CANVAS_KEYS.join(", ")}`,
    );
  }
  return found;
}

/**
 * Round to two decimal places. Sub-pixel values are kept because the browser
 * lays out in fractional pixels and rounding to integers visibly distorts the
 * type scale on the 1080px canvases.
 *
 * @param {number} value
 * @returns {number}
 */
function px(value) {
  assert.ok(Number.isFinite(value), `expected finite px value, got ${value}`);
  return Math.round(value * 100) / 100;
}

/**
 * Compute every pixel measurement for one canvas.
 *
 * Complexity: O(|TYPE| + |SPACE|), both fixed at module load, so effectively
 * constant.
 *
 * @param {{w: number, h: number, density: number}} canvas A member of {@link CANVASES}.
 * @returns {{k: number, t: number, type: Record<string, {size: number, line: number, weight: number, track: number}>, space: Record<string, number>}}
 * @throws {AssertionError} If the canvas has non-positive dimensions.
 */
export function metrics(canvas) {
  assert.ok(canvas.w > 0 && canvas.h > 0, "canvas dimensions must be positive");
  assert.ok(canvas.density > 0, "canvas density must be positive");

  const k = canvas.w / REFERENCE_WIDTH;
  const t = k * canvas.density;

  /** @type {Record<string, {size: number, line: number, weight: number, track: number}>} */
  const type = {};
  for (const [name, step] of Object.entries(TYPE)) {
    type[name] = {
      size: px(step.size * t),
      line: step.line,
      weight: step.weight,
      track: px(step.size * t * step.track),
    };
  }

  /** @type {Record<string, number>} */
  const space = {};
  for (const [name, value] of Object.entries(SPACE)) {
    space[name] = px(value * k);
  }
  space.hairline = Math.max(1, Math.round(SPACE.hairline * k));

  return { k: px(k), t: px(t), type, space };
}
