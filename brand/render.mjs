#!/usr/bin/env node
/**
 * Render every Haechi visual post to PNG.
 *
 * Pipeline: post data -> claim gate -> HTML document -> Chrome headless
 * screenshot -> PNG dimension check -> manifest.
 *
 * The claim gate runs before anything is written. A post containing a
 * prohibited marketing claim, a withdrawn palette colour, or an em dash aborts
 * the whole run rather than producing an asset somebody might publish.
 *
 * Usage:
 *   node brand/render.mjs [--out=DIR] [--only=ID] [--canvas=KEY]
 *                         [--scale=N] [--keep-html] [--audit] [--list]
 *
 * Exit codes: 0 success, 1 gate or render failure, 2 bad arguments.
 */

import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { CANVASES, CANVAS_KEYS, canvasByKey } from "./canvases.mjs";
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

/** Chrome must finish within this many milliseconds per screenshot. */
const CHROME_TIMEOUT_MS = 60_000;
/**
 * Virtual time the audit harness is allowed to advance while its iframes load.
 *
 * Used only by `audit()`. It is deliberately absent from `shoot()`: combining
 * `--virtual-time-budget` with `--screenshot` makes Chrome hang indefinitely,
 * because virtual time never reaches the budget while a capture is pending.
 */
const VIRTUAL_TIME_MS = 4_000;
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
      throw new Error(`CHROME is set to '${override}' but that path does not exist`);
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
 * @param {string} name File name.
 * @returns {number} A CSS font weight.
 */
function weightFromName(name) {
  const table = [
    [/thin/i, 100],
    [/extralight|extra-light/i, 200],
    [/light/i, 300],
    [/medium/i, 500],
    [/semibold|semi-bold/i, 600],
    [/extrabold|extra-bold/i, 800],
    [/black|heavy/i, 900],
    [/bold/i, 700],
  ];
  const hit = table.find(([pattern]) => pattern.test(name));
  return hit === undefined ? 400 : hit[1];
}

/**
 * Build `@font-face` rules from any font files in `brand/fonts/`, embedded as
 * data URIs so a render never touches the filesystem or network at load time.
 *
 * Returns an empty string when the directory is absent or holds no supported
 * files, which is the current state of the repository: no font files ship, so
 * renders fall back to the platform UI face. Dropping Inter into that
 * directory makes every subsequent render match the brand specification with
 * no code change.
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
function pngSize(buffer) {
  const SIGNATURE = "89504e470d0a1a0a";
  if (buffer.length < 24 || buffer.subarray(0, 8).toString("hex") !== SIGNATURE) {
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
function buildPlan(filters) {
  const posts = filters.only === null ? POSTS : [POSTS.find((p) => p.id === filters.only)];
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
    throw new Error(`plan of ${plan.length} renders exceeds the ${MAX_RENDERS} limit`);
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
function gate(documents) {
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
 * Capture one document with Chrome.
 *
 * @param {string} chrome Executable path.
 * @param {{htmlPath: string, pngPath: string, w: number, h: number, scale: number}} job
 * @returns {void}
 * @throws {Error} If Chrome fails, times out, or the PNG is the wrong size.
 */
function shoot(chrome, job) {
  const result = spawnSync(
    chrome,
    [
      "--headless",
      "--disable-gpu",
      "--hide-scrollbars",
      "--no-first-run",
      "--no-default-browser-check",
      "--disable-lcd-text",
      `--force-device-scale-factor=${job.scale}`,
      `--window-size=${job.w},${job.h}`,
      `--screenshot=${job.pngPath}`,
      `file://${job.htmlPath}`,
    ],
    { timeout: CHROME_TIMEOUT_MS, encoding: "utf8" },
  );
  if (result.error !== undefined) {
    throw new Error(`chrome failed for ${path.basename(job.pngPath)}: ${result.error.message}`);
  }
  if (!fs.existsSync(job.pngPath)) {
    throw new Error(
      `chrome produced no file for ${path.basename(job.pngPath)}\n${result.stderr ?? ""}`,
    );
  }
  const size = pngSize(fs.readFileSync(job.pngPath));
  const want = { w: job.w * job.scale, h: job.h * job.scale };
  if (size.w !== want.w || size.h !== want.h) {
    throw new Error(
      `${path.basename(job.pngPath)} is ${size.w}x${size.h}, expected ${want.w}x${want.h}`,
    );
  }
}

/**
 * Measure layout overflow for every document in a single Chrome run.
 *
 * Each document is loaded into an exactly-sized iframe, where the probe script
 * embedded by `templates.mjs` records whether `main` overflowed its box. One
 * browser launch covers the whole set.
 *
 * @param {string} chrome Executable path.
 * @param {Array<{name: string, html: string, canvas: {w: number, h: number}}>} documents
 * @returns {Array<{name: string, overflow: string, slack: number, font: string}>}
 * @throws {Error} If Chrome produces no parsable audit payload.
 */
function audit(chrome, documents) {
  const payload = documents.map((d) => ({
    name: d.name,
    w: d.canvas.w,
    h: d.canvas.h,
    html: d.html,
  }));
  const page = auditPage(payload);
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "haechi-brand-audit-"));
  const file = path.join(dir, "audit.html");
  try {
    fs.writeFileSync(file, page, "utf8");
    const result = spawnSync(
      chrome,
      [
        "--headless",
        "--disable-gpu",
        "--no-first-run",
        `--virtual-time-budget=${VIRTUAL_TIME_MS * documents.length + 10_000}`,
        "--dump-dom",
        `file://${file}`,
      ],
      { timeout: CHROME_TIMEOUT_MS * 4, encoding: "utf8", maxBuffer: 256 * 1024 * 1024 },
    );
    const match = /<pre id="audit-result">([\s\S]*?)<\/pre>/.exec(result.stdout ?? "");
    if (match === null || match[1].trim() === "") {
      throw new Error(`audit produced no result\n${result.stderr ?? ""}`);
    }
    return JSON.parse(match[1].replace(/&quot;/g, '"').replace(/&amp;/g, "&"));
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

/**
 * Build the single-page audit harness.
 *
 * @param {Array<{name: string, w: number, h: number, html: string}>} payload
 * @returns {string} A full HTML document.
 */
function auditPage(payload) {
  const data = JSON.stringify(payload).replace(/</g, "\\u003c");
  return `<!doctype html><html><head><meta charset="utf-8"><title>audit</title></head>
<body><pre id="audit-result"></pre>
<script>
const DOCS = ${data};
(async function () {
  const out = [];
  for (const d of DOCS) {
    const f = document.createElement('iframe');
    f.setAttribute('width', d.w);
    f.setAttribute('height', d.h);
    f.style.cssText = 'border:0;position:absolute;left:-99999px;top:0';
    document.body.appendChild(f);
    await new Promise(function (resolve) { f.onload = resolve; f.srcdoc = d.html; });
    const de = f.contentDocument.documentElement;
    out.push({
      name: d.name,
      overflow: de.getAttribute('data-overflow'),
      slack: Number(de.getAttribute('data-slack')),
      font: de.getAttribute('data-font')
    });
    f.remove();
  }
  document.getElementById('audit-result').textContent = JSON.stringify(out);
})();
</script></body></html>`;
}

/**
 * Parse argv into options.
 *
 * @param {string[]} argv Arguments after the script name.
 * @returns {{out: string, only: string | null, canvas: string | null, scale: number, keepHtml: boolean, audit: boolean, list: boolean}}
 * @throws {Error} On an unknown flag or an invalid value.
 */
function parseArgs(argv) {
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
    const [flag, value] = arg.includes("=") ? arg.split(/=(.*)/s, 2) : [arg, ""];
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
  if (!Number.isInteger(options.scale) || options.scale < 1 || options.scale > 4) {
    throw new Error("--scale must be an integer between 1 and 4");
  }
  return options;
}

/**
 * Generate HTML for every entry in the plan.
 *
 * @param {ReturnType<typeof buildPlan>} plan
 * @param {string} fontCss
 * @returns {Array<{name: string, post: object, canvas: object, html: string}>}
 */
function buildDocuments(plan, fontCss) {
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
 * Rasterise every document and return manifest rows.
 *
 * @param {string} chrome
 * @param {ReturnType<typeof buildDocuments>} documents
 * @param {{out: string, scale: number, keepHtml: boolean}} options
 * @returns {Array<{name: string, file: string, width: number, height: number, bytes: number, sha256: string, post: string, kind: string, canvas: string, sources: string[]}>}
 */
function rasterise(chrome, documents, options) {
  const htmlDir = path.join(options.out, "html");
  fs.mkdirSync(htmlDir, { recursive: true });
  const rows = [];
  for (const [i, doc] of documents.entries()) {
    const htmlPath = path.join(htmlDir, `${doc.name}.html`);
    const pngPath = path.join(options.out, `${doc.name}.png`);
    fs.writeFileSync(htmlPath, doc.html, "utf8");
    shoot(chrome, {
      htmlPath,
      pngPath,
      w: doc.canvas.w,
      h: doc.canvas.h,
      scale: options.scale,
    });
    const bytes = fs.readFileSync(pngPath);
    const size = pngSize(bytes);
    rows.push({
      name: doc.name,
      file: path.basename(pngPath),
      width: size.w,
      height: size.h,
      bytes: bytes.length,
      sha256: createHash("sha256").update(bytes).digest("hex"),
      post: doc.post.id,
      kind: doc.post.kind,
      canvas: doc.canvas.key,
      sources: [...doc.post.sources],
    });
    process.stdout.write(
      `  [${String(i + 1).padStart(2, " ")}/${documents.length}] ${doc.name}.png  ${size.w}x${size.h}  ${(bytes.length / 1024).toFixed(0)}kB\n`,
    );
  }
  if (!options.keepHtml) fs.rmSync(htmlDir, { recursive: true, force: true });
  return rows;
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
 * Report overflow findings and fail if any document does not fit its canvas.
 *
 * @param {Array<{name: string, overflow: string, slack: number, font: string}>} results
 * @returns {void}
 * @throws {Error} If any document overflows.
 */
function reportAudit(results) {
  const bad = results.filter((r) => r.overflow !== "false");
  const fonts = [...new Set(results.map((r) => r.font))];
  for (const row of results) {
    const state = row.overflow === "false" ? "fits" : "OVERFLOW";
    process.stdout.write(
      `  ${row.name.padEnd(44)} ${state.padEnd(9)} slack ${String(row.slack).padStart(5)}px\n`,
    );
  }
  process.stdout.write(`\n  resolved font family: ${fonts.join(", ")}\n`);
  if (bad.length > 0) {
    throw new Error(
      `${bad.length} document(s) overflow their canvas:\n  ${bad.map((b) => b.name).join("\n  ")}`,
    );
  }
}

/**
 * Entry point.
 *
 * @returns {number} Process exit code.
 */
function main() {
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
    process.stdout.write(
      `claim gate passed: ${new Set(documents.map((d) => d.post.id)).size} post(s), ${documents.length} document(s)\n`,
    );
    if (warnings.length > 0) {
      process.stdout.write(`\n${formatFindings(warnings)}\n\n`);
    }
    process.stdout.write(
      fonts.files.length > 0
        ? `fonts embedded: ${fonts.files.join(", ")}\n`
        : "fonts: none in brand/fonts, falling back to the platform UI face\n",
    );

    const chrome = findChrome();
    if (options.audit) {
      process.stdout.write(`\nauditing ${documents.length} document(s)\n`);
      reportAudit(audit(chrome, documents));
      return 0;
    }

    fs.mkdirSync(options.out, { recursive: true });
    process.stdout.write(`\nrendering to ${options.out}\n`);
    const rows = rasterise(chrome, documents, options);
    const manifest = {
      generated: new Date().toISOString(),
      generator: "brand/render.mjs",
      scale: options.scale,
      note: "Derived output. Regenerate with 'npm run brand:posts'.",
      assets: rows,
    };
    fs.writeFileSync(
      path.join(options.out, "manifest.json"),
      `${JSON.stringify(manifest, null, 2)}\n`,
      "utf8",
    );
    process.stdout.write(`\n${rows.length} asset(s) written, manifest.json updated.\n`);
    return 0;
  } catch (error) {
    process.stderr.write(`\n${error.message}\n`);
    return 1;
  }
}

process.exitCode = main();
