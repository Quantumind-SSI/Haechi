#!/usr/bin/env node
/**
 * Render every Haechi visual post to PNG.
 *
 * Pipeline: post data -> claim gate -> HTML document -> one Chrome session ->
 * layout measurement -> PNG capture -> dimension check -> manifest.
 *
 * Two properties matter more than speed here.
 *
 * The claim gate runs before Chrome starts. A post containing a prohibited
 * marketing claim, a withdrawn palette colour, or an em dash aborts the whole
 * run rather than producing an asset somebody might publish.
 *
 * Layout is measured on the same page load that produces the screenshot, so
 * "it fits" is a measurement of the delivered pixels rather than of a separate
 * and possibly divergent render.
 *
 * Usage:
 *   node brand/render.mjs [--out=DIR] [--only=ID] [--canvas=KEY]
 *                         [--scale=N] [--keep-html] [--audit] [--list]
 *
 * Exit codes: 0 success, 1 gate, layout or render failure, 2 bad arguments.
 */

import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { CANVASES, CANVAS_KEYS, canvasByKey } from "./canvases.mjs";
import { launch } from "./cdp.mjs";
import {
  assertClean,
  formatFindings,
  lintColours,
  lintPost,
} from "./claims.mjs";
import { POSTS, POST_IDS } from "./posts.mjs";
import { renderDocument } from "./templates.mjs";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const REPO = path.resolve(HERE, "..");
const DEFAULT_OUT = path.join(REPO, "brand-assets", "posts");
const FONT_DIR = path.join(HERE, "fonts");

/** Upper bound on renders in one run, so a bad plan cannot spin forever. */
const MAX_RENDERS = 500;

/** Candidate Chrome locations, checked in order. */
const CHROME_CANDIDATES = Object.freeze([
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  "/Applications/Chromium.app/Contents/MacOS/Chromium",
  "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
  "/usr/bin/google-chrome",
  "/usr/bin/google-chrome-stable",
  "/usr/bin/chromium",
  "/usr/bin/chromium-browser",
  "/snap/bin/chromium",
]);

/** Expression evaluated in each page to read the probe attributes. */
const PROBE_EXPRESSION = `(function(){
  var e = document.documentElement;
  return {
    overflow: e.getAttribute('data-overflow'),
    headroom: Number(e.getAttribute('data-headroom')),
    font: e.getAttribute('data-font')
  };
})()`;

/**
 * Locate a Chrome-family browser.
 *
 * @returns {string} Absolute path to the executable.
 * @throws {Error} If none is found. `CHROME` overrides the search.
 */
function findChrome() {
  const override = process.env.CHROME;
  if (typeof override === "string" && override.length > 0) {
    if (!fs.existsSync(override)) {
      throw new Error(
        `CHROME is set to '${override}' but that path does not exist`,
      );
    }
    return override;
  }
  const found = CHROME_CANDIDATES.find((c) => fs.existsSync(c));
  if (found === undefined) {
    throw new Error(
      `no Chrome-family browser found. Install Google Chrome or set CHROME to an executable.\nSearched:\n  ${CHROME_CANDIDATES.join("\n  ")}`,
    );
  }
  return found;
}

/** MIME type and CSS format keyed by font file extension. */
const FONT_FORMATS = Object.freeze({
  ".woff2": ["font/woff2", "woff2"],
  ".woff": ["font/woff", "woff"],
  ".ttf": ["font/ttf", "truetype"],
  ".otf": ["font/otf", "opentype"],
});

/**
 * Guess a weight from an Inter filename such as `Inter-SemiBold.woff2`.
 *
 * Ordered so `SemiBold` and `ExtraBold` are matched before plain `Bold`.
 *
 * @param {string} name File name.
 * @returns {number} A CSS font weight.
 */
export function weightFromName(name) {
  const table = [
    [/thin/i, 100],
    [/extra-?light/i, 200],
    [/light/i, 300],
    [/medium/i, 500],
    [/semi-?bold/i, 600],
    [/extra-?bold/i, 800],
    [/black|heavy/i, 900],
    [/bold/i, 700],
  ];
  const hit = table.find(([pattern]) => pattern.test(name));
  return hit === undefined ? 400 : hit[1];
}

/**
 * Build `@font-face` rules from any font files in `brand/fonts/`, embedded as
 * data URIs so a render never touches the network at load time.
 *
 * Returns an empty string when the directory is absent or holds no supported
 * files, which is the current state of the repository: no font files ship, so
 * renders fall back to the platform UI face. Dropping Inter into that
 * directory makes every subsequent render match the brand specification with
 * no code change, and the probe reports which face was actually used.
 *
 * Complexity: O(total bytes of font files).
 *
 * @returns {{css: string, files: string[]}}
 */
function loadFontFaces() {
  if (!fs.existsSync(FONT_DIR)) return { css: "", files: [] };
  const names = fs
    .readdirSync(FONT_DIR)
    .filter((n) => FONT_FORMATS[path.extname(n).toLowerCase()] !== undefined)
    .sort();
  const rules = names.map((name) => {
    const [mime, format] = FONT_FORMATS[path.extname(name).toLowerCase()];
    const data = fs.readFileSync(path.join(FONT_DIR, name)).toString("base64");
    return `@font-face{font-family:Inter;font-style:normal;font-display:block;font-weight:${weightFromName(name)};src:url(data:${mime};base64,${data}) format("${format}")}`;
  });
  return { css: rules.join(""), files: names };
}

/**
 * Read width and height out of a PNG IHDR chunk.
 *
 * @param {Buffer} buffer File contents.
 * @returns {{w: number, h: number}}
 * @throws {Error} If the buffer is not a PNG.
 */
export function pngSize(buffer) {
  const SIGNATURE = "89504e470d0a1a0a";
  if (
    buffer.length < 24 ||
    buffer.subarray(0, 8).toString("hex") !== SIGNATURE
  ) {
    throw new Error("not a PNG file");
  }
  return { w: buffer.readUInt32BE(16), h: buffer.readUInt32BE(20) };
}

/**
 * Build the full list of renders: every post on every canvas, plus one
 * portrait slide sequence for each post marked `carousel`.
 *
 * @param {{only: string | null, canvas: string | null}} filters
 * @returns {Array<{name: string, post: object, canvas: object, slide?: number, slideCount?: number}>}
 * @throws {Error} If the plan exceeds {@link MAX_RENDERS}.
 */
export function buildPlan(filters) {
  const posts =
    filters.only === null ? POSTS : [POSTS.find((p) => p.id === filters.only)];
  const canvases =
    filters.canvas === null ? CANVASES : [canvasByKey(filters.canvas)];
  /** @type {Array<{name: string, post: object, canvas: object, slide?: number, slideCount?: number}>} */
  const plan = [];
  for (const post of posts) {
    for (const canvas of canvases) {
      plan.push({ name: `${post.id}.${canvas.key}`, post, canvas });
    }
    if (post.carousel !== true) continue;
    const portrait = canvasByKey("portrait");
    if (filters.canvas !== null && filters.canvas !== portrait.key) continue;
    const count = post.rows.length + 1;
    for (let slide = 0; slide < count; slide += 1) {
      const index = String(slide + 1).padStart(2, "0");
      plan.push({
        name: `${post.id}.carousel-${index}.portrait`,
        post,
        canvas: portrait,
        slide,
        slideCount: count,
      });
    }
  }
  if (plan.length > MAX_RENDERS) {
    throw new Error(
      `plan of ${plan.length} renders exceeds the ${MAX_RENDERS} limit`,
    );
  }
  return plan;
}

/**
 * Run the claim-safety gate over every post and every generated document.
 *
 * @param {Array<{name: string, post: object, html: string}>} documents
 * @returns {import("./claims.mjs").Finding[]} Warnings. Failures throw.
 * @throws {Error} On any `fail` finding.
 */
export function gate(documents) {
  const seen = new Set();
  /** @type {import("./claims.mjs").Finding[]} */
  const findings = [];
  for (const doc of documents) {
    if (!seen.has(doc.post.id)) {
      seen.add(doc.post.id);
      findings.push(...lintPost(doc.post));
    }
    findings.push(...lintColours(doc.html, doc.name));
  }
  return assertClean(findings);
}

/**
 * Generate HTML for every entry in the plan.
 *
 * @param {ReturnType<typeof buildPlan>} plan
 * @param {string} fontCss
 * @returns {Array<{name: string, post: object, canvas: object, html: string}>}
 */
export function buildDocuments(plan, fontCss) {
  return plan.map((entry) => ({
    name: entry.name,
    post: entry.post,
    canvas: entry.canvas,
    html: renderDocument(entry.post, entry.canvas, {
      slide: entry.slide,
      slideCount: entry.slideCount,
      fontCss,
    }),
  }));
}

/**
 * Load one document, measure it, and optionally capture it.
 *
 * @param {import("./cdp.mjs").Browser} browser
 * @param {{name: string, post: object, canvas: object, html: string}} doc
 * @param {{out: string, htmlDir: string, scale: number, audit: boolean}} options
 * @returns {Promise<object>} A manifest row.
 * @throws {Error} If the captured PNG is not the expected size.
 */
async function renderOne(browser, doc, options) {
  const htmlPath = path.join(options.htmlDir, `${doc.name}.html`);
  fs.writeFileSync(htmlPath, doc.html, "utf8");
  await browser.load(pathToFileURL(htmlPath).href, {
    w: doc.canvas.w,
    h: doc.canvas.h,
    scale: options.scale,
  });
  const probe = await browser.evaluate(PROBE_EXPRESSION);
  const row = {
    name: doc.name,
    post: doc.post.id,
    kind: doc.post.kind,
    canvas: doc.canvas.key,
    width: doc.canvas.w * options.scale,
    height: doc.canvas.h * options.scale,
    overflow: probe.overflow !== "false",
    headroomPx: probe.headroom,
    font: probe.font,
    sources: [...doc.post.sources],
  };
  if (options.audit) return row;

  const bytes = await browser.screenshot();
  const size = pngSize(bytes);
  if (size.w !== row.width || size.h !== row.height) {
    throw new Error(
      `${doc.name}.png is ${size.w}x${size.h}, expected ${row.width}x${row.height}`,
    );
  }
  const pngPath = path.join(options.out, `${doc.name}.png`);
  fs.writeFileSync(pngPath, bytes);
  row.file = path.basename(pngPath);
  row.bytes = bytes.length;
  row.sha256 = createHash("sha256").update(bytes).digest("hex");
  return row;
}

/**
 * Drive one browser session across every document.
 *
 * @param {string} chrome Executable path.
 * @param {ReturnType<typeof buildDocuments>} documents
 * @param {{out: string, scale: number, keepHtml: boolean, audit: boolean}} options
 * @returns {Promise<object[]>} Manifest rows in plan order.
 */
async function renderAll(chrome, documents, options) {
  const profileDir = fs.mkdtempSync(
    path.join(os.tmpdir(), "haechi-brand-chrome-"),
  );
  const htmlDir = options.audit
    ? fs.mkdtempSync(path.join(os.tmpdir(), "haechi-brand-html-"))
    : path.join(options.out, "html");
  fs.mkdirSync(htmlDir, { recursive: true });
  const browser = await launch(chrome, profileDir);
  const rows = [];
  try {
    for (const [i, doc] of documents.entries()) {
      const row = await renderOne(browser, doc, { ...options, htmlDir });
      rows.push(row);
      const detail = options.audit
        ? `${row.overflow ? "OVERFLOW" : "fits"}  headroom ${String(row.headroomPx).padStart(5)}px`
        : `${row.width}x${row.height}  ${(row.bytes / 1024).toFixed(0)}kB`;
      process.stdout.write(
        `  [${String(i + 1).padStart(2, " ")}/${documents.length}] ${doc.name.padEnd(42)} ${detail}\n`,
      );
    }
  } finally {
    // Teardown reports rather than throws, so an error from the render loop is
    // not replaced by a cleanup error. Reporting is not optional: a browser
    // that failed to exit is a leaked process the operator needs to know about.
    for (const problem of await browser.close()) {
      process.stderr.write(`brand: browser teardown: ${problem}\n`);
    }
    fs.rmSync(profileDir, { recursive: true, force: true });
    if (options.audit || !options.keepHtml) {
      fs.rmSync(htmlDir, { recursive: true, force: true });
    }
  }
  return rows;
}

/**
 * Parse argv into options.
 *
 * @param {string[]} argv Arguments after the script name.
 * @returns {{out: string, only: string | null, canvas: string | null, scale: number, keepHtml: boolean, audit: boolean, list: boolean}}
 * @throws {Error} On an unknown flag or an invalid value.
 */
export function parseArgs(argv) {
  const options = {
    out: DEFAULT_OUT,
    only: null,
    canvas: null,
    scale: 1,
    keepHtml: false,
    audit: false,
    list: false,
  };
  for (const arg of argv) {
    const [flag, value] = arg.includes("=")
      ? arg.split(/=(.*)/s, 2)
      : [arg, ""];
    if (flag === "--out") options.out = path.resolve(value);
    else if (flag === "--only") options.only = value;
    else if (flag === "--canvas") options.canvas = value;
    else if (flag === "--scale") options.scale = Number(value);
    else if (flag === "--keep-html") options.keepHtml = true;
    else if (flag === "--audit") options.audit = true;
    else if (flag === "--list") options.list = true;
    else throw new Error(`unknown argument '${arg}'`);
  }
  if (options.only !== null && !POST_IDS.includes(options.only)) {
    throw new Error(`--only must be one of: ${POST_IDS.join(", ")}`);
  }
  if (options.canvas !== null && !CANVAS_KEYS.includes(options.canvas)) {
    throw new Error(`--canvas must be one of: ${CANVAS_KEYS.join(", ")}`);
  }
  if (
    !Number.isInteger(options.scale) ||
    options.scale < 1 ||
    options.scale > 4
  ) {
    throw new Error("--scale must be an integer between 1 and 4");
  }
  return options;
}

/**
 * Print the render plan without producing anything.
 *
 * @param {ReturnType<typeof buildPlan>} plan
 * @returns {void}
 */
function printPlan(plan) {
  for (const entry of plan) {
    process.stdout.write(
      `${entry.name.padEnd(44)} ${entry.canvas.w}x${entry.canvas.h}  ${entry.post.kind}\n`,
    );
  }
  process.stdout.write(`\n${plan.length} render(s) planned.\n`);
}

/**
 * Fail the run if any document did not fit its canvas.
 *
 * @param {object[]} rows Manifest rows.
 * @returns {void}
 * @throws {Error} If any row overflowed.
 */
function assertNoOverflow(rows) {
  const bad = rows.filter((r) => r.overflow);
  if (bad.length === 0) return;
  throw new Error(
    `${bad.length} document(s) overflow their canvas:\n  ${bad
      .map((b) => `${b.name} by ${Math.abs(b.headroomPx)}px`)
      .join("\n  ")}`,
  );
}

/**
 * Write the manifest beside the assets.
 *
 * @param {string} out Output directory.
 * @param {object[]} rows Manifest rows.
 * @param {{scale: number}} options
 * @returns {void}
 */
function writeManifest(out, rows, options) {
  const manifest = {
    generated: new Date().toISOString(),
    generator: "brand/render.mjs",
    scale: options.scale,
    font: [...new Set(rows.map((r) => r.font))].join(", "),
    note: "Derived output. Regenerate with 'npm run brand:posts'.",
    assets: rows,
  };
  fs.writeFileSync(
    path.join(out, "manifest.json"),
    `${JSON.stringify(manifest, null, 2)}\n`,
    "utf8",
  );
}

/**
 * Entry point.
 *
 * @returns {Promise<number>} Process exit code.
 */
async function main() {
  let options;
  try {
    options = parseArgs(process.argv.slice(2));
  } catch (error) {
    process.stderr.write(`${error.message}\n`);
    return 2;
  }

  try {
    const plan = buildPlan({ only: options.only, canvas: options.canvas });
    if (options.list) {
      printPlan(plan);
      return 0;
    }

    const fonts = loadFontFaces();
    const documents = buildDocuments(plan, fonts.css);
    const warnings = gate(documents);
    const postCount = new Set(documents.map((d) => d.post.id)).size;
    process.stdout.write(
      `claim gate passed: ${postCount} post(s), ${documents.length} document(s)\n`,
    );
    if (warnings.length > 0)
      process.stdout.write(`\n${formatFindings(warnings)}\n\n`);
    process.stdout.write(
      fonts.files.length > 0
        ? `fonts embedded: ${fonts.files.join(", ")}\n`
        : "fonts: none in brand/fonts, falling back to the platform UI face\n",
    );

    const chrome = findChrome();
    if (!options.audit) fs.mkdirSync(options.out, { recursive: true });
    process.stdout.write(
      `\n${options.audit ? "measuring layout" : `rendering to ${options.out}`}, ${documents.length} document(s)\n`,
    );
    const rows = await renderAll(chrome, documents, options);
    process.stdout.write(
      `\nresolved font family: ${[...new Set(rows.map((r) => r.font))].join(", ")}\n`,
    );
    assertNoOverflow(rows);

    if (options.audit) {
      process.stdout.write(`${rows.length} document(s) fit their canvas.\n`);
      return 0;
    }
    writeManifest(options.out, rows, options);
    process.stdout.write(
      `${rows.length} asset(s) written, manifest.json updated.\n`,
    );
    return 0;
  } catch (error) {
    process.stderr.write(`\n${error.message}\n`);
    return 1;
  }
}

/**
 * Whether this module was started directly rather than imported.
 *
 * Without this guard, importing the helpers above for a test would run a full
 * render as a side effect. Compared as file URLs rather than paths so symlinks
 * and Windows separators do not defeat it.
 *
 * @returns {boolean}
 */
function isEntryPoint() {
  const entry = process.argv[1];
  if (typeof entry !== "string" || entry.length === 0) return false;
  return pathToFileURL(entry).href === import.meta.url;
}

if (isEntryPoint()) {
  process.exitCode = await main();
}
