/**
 * Unit tests for the visual post generator.
 *
 * Run with `npm run brand:test`. These are deliberately browser-free and take
 * well under a second: they cover the parts that can be wrong silently. The
 * parts that can only be judged by rendering, chiefly whether copy fits its
 * canvas, are covered by `npm run brand:audit`, which measures the real
 * layout in Chrome and fails on overflow.
 *
 * The most important tests here are the drift guards. The palette and the mark
 * are copies of the shipped website, and a copy with no test is a copy that
 * will diverge.
 */

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { describe, it } from "node:test";
import { fileURLToPath } from "node:url";

import { CANVASES, CANVAS_KEYS, canvasByKey, metrics } from "./canvases.mjs";
import { assertClean, lintColours, lintPost, lintText } from "./claims.mjs";
import { POSTS, POST_IDS, postById } from "./posts.mjs";
import {
  emphasise,
  escapeHtml,
  renderBody,
  renderDocument,
  renderSlideBody,
} from "./templates.mjs";
import {
  buildDocuments,
  buildPlan,
  gate,
  parseArgs,
  pngSize,
  weightFromName,
} from "./render.mjs";
import {
  COLOUR,
  MARK_SVG,
  STYLESHEET_PARITY,
  TYPE,
  WITHDRAWN_COLOURS,
} from "./tokens.mjs";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const REPO = path.resolve(HERE, "..");
const STYLESHEET = path.join(REPO, "website", "styles.css");
const MARK_FILE = path.join(REPO, "website", "assets", "haechi-mark.svg");

describe("token drift guards", () => {
  it("every parity colour matches the shipped stylesheet", () => {
    const css = fs.readFileSync(STYLESHEET, "utf8");
    const root = /:root\s*\{([\s\S]*?)\}/.exec(css);
    assert.ok(root !== null, "styles.css has no :root block");
    for (const [token, property] of Object.entries(STYLESHEET_PARITY)) {
      const declared = new RegExp(`${property}\\s*:\\s*([^;]+);`).exec(root[1]);
      assert.ok(declared !== null, `${property} is missing from styles.css`);
      assert.equal(
        declared[1].trim().toLowerCase(),
        COLOUR[token].toLowerCase(),
        `${token} has drifted from ${property}`,
      );
    }
  });

  it("the inlined mark matches website/assets/haechi-mark.svg", () => {
    const source = fs.readFileSync(MARK_FILE, "utf8");
    const paths = (text) =>
      [...text.matchAll(/\sd="([^"]+)"/g)].map((m) => m[1]);
    const fills = (text) =>
      [...text.matchAll(/fill="([^"]+)"/g)].map((m) => m[1]);
    assert.deepEqual(
      paths(MARK_SVG),
      paths(source),
      "mark geometry has drifted",
    );
    assert.deepEqual(
      fills(MARK_SVG),
      fills(source),
      "mark colours have drifted",
    );
    assert.equal(paths(MARK_SVG).length, 3, "the mark is three paths");
  });

  it("no withdrawn colour is present in the palette", () => {
    const live = Object.values(COLOUR).map((v) => v.toLowerCase());
    for (const withdrawn of WITHDRAWN_COLOURS) {
      assert.ok(
        !live.includes(withdrawn),
        `${withdrawn} is withdrawn but still in COLOUR`,
      );
    }
  });
});

describe("canvas geometry", () => {
  it("defines exactly the five approved canvases with unique keys", () => {
    assert.equal(CANVASES.length, 5);
    assert.equal(new Set(CANVAS_KEYS).size, 5);
    const sizes = CANVASES.map((c) => `${c.w}x${c.h}`);
    assert.deepEqual(sizes, [
      "1200x627",
      "1200x630",
      "1600x900",
      "1080x1080",
      "1080x1350",
    ]);
  });

  it("scales geometry by width and type by width times density", () => {
    const round = (n) => Math.round(n * 100) / 100;
    const x = canvasByKey("x");
    const m = metrics(x);
    // Reported factors are rounded to two decimals for readable CSS; the
    // unrounded values are used internally to derive every measurement.
    assert.equal(m.k, round(1600 / 1200));
    assert.equal(m.t, round((1600 / 1200) * x.density));
    assert.equal(
      m.type.h1.size,
      round(TYPE.h1.size * (1600 / 1200) * x.density),
    );
    assert.ok(
      m.type.h1.size > TYPE.h1.size,
      "the 1600px canvas scales type up",
    );
    assert.ok(m.space.pad > 0 && m.space.hairline >= 1);
  });

  it("scales type down on the short canvases so dense layouts fit", () => {
    const linkedin = metrics(canvasByKey("linkedin"));
    const portrait = metrics(canvasByKey("portrait"));
    assert.ok(linkedin.t < 1, "the 627px-tall canvas must reduce type");
    assert.ok(portrait.t > linkedin.t, "the tall canvas can carry larger type");
  });

  it("keeps every hairline at one device pixel or more", () => {
    for (const canvas of CANVASES) {
      assert.ok(
        metrics(canvas).space.hairline >= 1,
        `${canvas.key} lost its hairline`,
      );
    }
  });

  it("rejects an unknown canvas key", () => {
    assert.throws(() => canvasByKey("story"), /unknown canvas 'story'/);
  });
});

describe("claim safety", () => {
  const prohibited = [
    ["provably anonymous transport", "claim/provable-anonymity"],
    ["a fully anonymous session", "claim/anonymity"],
    ["zero-knowledge orchestration", "claim/zero-knowledge"],
    ["quantum-resistant tunnels", "claim/quantum-resistance"],
    ["a strict no-logs policy", "claim/no-logs"],
    ["a cryptographic guarantee of privacy", "claim/security-guarantee"],
    ["AI-driven traffic morphing", "claim/traffic-morphing"],
    ["military-grade and unbreakable", "claim/absolute-security"],
    ["a seamless, world-class experience", "voice/superlative"],
    ["identity \u2014 never shared", "style/em-dash"],
  ];

  for (const [text, rule] of prohibited) {
    it(`rejects ${JSON.stringify(text)} under ${rule}`, () => {
      const found = lintText(text, "test");
      assert.ok(
        found.some((f) => f.rule === rule && f.severity === "fail"),
        `expected ${rule}, got ${found.map((f) => f.rule).join(", ") || "nothing"}`,
      );
    });
  }

  it("permits the approved public vocabulary", () => {
    const approved = [
      "Reinforce proven tunnels for the post-quantum transition.",
      "Entitlement in. Durable identity out.",
      "Deny by default, fail closed. No silent downgrade.",
      "Prohibited fields cannot cross the telemetry boundary.",
      "pq_mode: hybrid-pq or classical",
    ];
    for (const text of approved) {
      const failures = lintText(text, "test").filter(
        (f) => f.severity === "fail",
      );
      assert.deepEqual(failures, [], `unexpected failure in: ${text}`);
    }
  });

  it("warns rather than fails on guarded roadmap language", () => {
    const found = lintText("An unlinkable access prototype.", "test");
    assert.equal(found.length, 1);
    assert.equal(found[0].rule, "guard/unlinkable");
    assert.equal(found[0].severity, "warn");
    assert.deepEqual(assertClean(found), found, "warnings must pass the gate");
  });

  it("reports every occurrence, not just the first", () => {
    const found = lintText("anonymous and anonymous again", "test");
    assert.equal(found.filter((f) => f.rule === "claim/anonymity").length, 2);
  });

  it("cites the document that imposes each rule", () => {
    for (const finding of lintText("quantum-safe and unhackable", "test")) {
      assert.match(finding.citation, /\.md|\.html|constitution/);
      assert.ok(finding.reason.length > 20, "a rule must explain itself");
    }
  });

  it("throws on failures and names the count", () => {
    assert.throws(
      () => assertClean(lintText("unbreakable zero-knowledge", "test")),
      /claim safety gate failed with 2 violation/,
    );
  });

  it("flags withdrawn colours in markup but exempts the logo mark", () => {
    assert.deepEqual(lintColours(MARK_SVG, "mark"), [], "the mark is exempt");
    const found = lintColours('<div style="color:#1A1A1A"></div>', "doc");
    assert.equal(found.length, 1);
    assert.equal(found[0].rule, "palette/withdrawn-colour");
  });
});

describe("post definitions", () => {
  it("has eight posts, five educational and three marketing", () => {
    assert.equal(POSTS.length, 8);
    assert.equal(POSTS.filter((p) => p.kind === "educational").length, 5);
    assert.equal(POSTS.filter((p) => p.kind === "marketing").length, 3);
    assert.equal(new Set(POST_IDS).size, 8, "post ids must be unique");
  });

  it("passes the claim gate for every post", () => {
    for (const post of POSTS) {
      const failures = lintPost(post).filter((f) => f.severity === "fail");
      assert.deepEqual(failures, [], `${post.id} has prohibited copy`);
    }
  });

  it("supplies the fields its template requires", () => {
    const required = { cards: "cards", rows: "rows", split: "panels" };
    for (const post of POSTS) {
      assert.ok(
        post.eyebrow && post.headline && post.lede,
        `${post.id} is missing head copy`,
      );
      assert.ok(post.sources.length > 0, `${post.id} cites no source`);
      const field = required[post.template];
      if (field === undefined) continue;
      assert.ok(
        Array.isArray(post[field]) && post[field].length > 0,
        `${post.id}.${field}`,
      );
      if (post.template === "split") {
        assert.equal(
          post.panels.length,
          2,
          `${post.id} needs exactly two panels`,
        );
        assert.deepEqual(
          [...post.panels].map((p) => p.tone).sort(),
          ["negative", "positive"],
          `${post.id} needs one positive and one negative panel`,
        );
      }
    }
  });

  it("marks exactly one carousel, and it has rows to page through", () => {
    const carousels = POSTS.filter((p) => p.carousel === true);
    assert.equal(carousels.length, 1);
    assert.equal(carousels[0].rows.length, 5);
    for (const row of carousels[0].rows) {
      assert.ok(row.points.length >= 3, `${row.title} needs supporting points`);
    }
  });

  it("rejects an unknown post id", () => {
    assert.throws(() => postById("nope"), /unknown post 'nope'/);
  });
});

describe("markup generation", () => {
  it("escapes every HTML metacharacter", () => {
    assert.equal(
      escapeHtml(`<img src=x onerror="alert('1')">&`),
      "&lt;img src=x onerror=&quot;alert(&#39;1&#39;)&quot;&gt;&amp;",
    );
  });

  it("turns the asterisk convention into an accent span", () => {
    assert.equal(
      emphasise("Communications that *reveal less.*"),
      'Communications that <span class="hl">reveal less.</span>',
    );
    assert.equal(emphasise("no markers here"), "no markers here");
  });

  it("escapes before emphasising so injected markup cannot survive", () => {
    const html = renderDocument(
      { ...postById("m1-reveal-less"), headline: "<script>bad</script> *ok*" },
      canvasByKey("og"),
    );
    assert.ok(!html.includes("<script>bad"), "injected markup must be escaped");
    assert.ok(html.includes('<span class="hl">ok</span>'));
  });

  it("produces a self-contained document with no external references", () => {
    for (const canvas of CANVASES) {
      const html = renderDocument(postById("e1-security-gap"), canvas);
      assert.match(html, /^<!doctype html>/);
      assert.ok(
        !/(src|href)="(https?:)?\/\//.test(html),
        `${canvas.key} fetches something remote`,
      );
      assert.ok(
        html.includes(`--w:${canvas.w}px`),
        `${canvas.key} has the wrong width`,
      );
      assert.ok(
        html.includes("Engineering preview"),
        `${canvas.key} lost the status badge`,
      );
      assert.ok(html.includes("haechi.co.uk"), `${canvas.key} lost the domain`);
    }
  });

  it("drops the lede on wide canvases for dense templates only", () => {
    const cards = postById("e1-security-gap");
    assert.ok(!renderBody(cards, canvasByKey("linkedin")).includes(cards.lede));
    assert.ok(renderBody(cards, canvasByKey("portrait")).includes(cards.lede));
    const statement = postById("m1-reveal-less");
    assert.ok(
      renderBody(statement, canvasByKey("linkedin")).includes(statement.lede),
    );
  });

  it("renders a cover slide and one slide per layer", () => {
    const post = postById("e2-five-layers");
    assert.ok(
      renderSlideBody(post, 0).includes("Swipe through the five layers"),
    );
    for (let i = 1; i <= post.rows.length; i += 1) {
      const html = renderSlideBody(post, i);
      assert.ok(html.includes(post.rows[i - 1].title), `slide ${i} title`);
      assert.ok(
        html.includes('<span class="on">'),
        `slide ${i} progress marker`,
      );
    }
    assert.throws(() => renderSlideBody(post, 6), /out of range/);
  });

  it("rejects a post naming a template that does not exist", () => {
    assert.throws(
      () =>
        renderBody(
          { ...postById("m1-reveal-less"), template: "poster" },
          canvasByKey("og"),
        ),
      /unknown template 'poster'/,
    );
  });
});

describe("render planning", () => {
  it("plans every post on every canvas plus the carousel slides", () => {
    const plan = buildPlan({ only: null, canvas: null });
    assert.equal(plan.length, POSTS.length * CANVASES.length + 6);
    assert.equal(
      new Set(plan.map((p) => p.name)).size,
      plan.length,
      "names must be unique",
    );
  });

  it("honours the post and canvas filters", () => {
    assert.equal(buildPlan({ only: "m1-reveal-less", canvas: null }).length, 5);
    assert.equal(buildPlan({ only: null, canvas: "square" }).length, 8);
    assert.equal(
      buildPlan({ only: "e2-five-layers", canvas: "portrait" }).length,
      7,
    );
    assert.equal(
      buildPlan({ only: "e2-five-layers", canvas: "square" }).length,
      1,
    );
  });

  it("passes the claim gate for the whole plan", () => {
    const documents = buildDocuments(
      buildPlan({ only: null, canvas: null }),
      "",
    );
    assert.deepEqual(
      gate(documents).filter((f) => f.severity === "fail"),
      [],
    );
  });
});

describe("command line and helpers", () => {
  it("defaults sensibly and accepts every flag", () => {
    const options = parseArgs([
      "--only=m1-reveal-less",
      "--canvas=og",
      "--scale=2",
      "--keep-html",
    ]);
    assert.equal(options.only, "m1-reveal-less");
    assert.equal(options.canvas, "og");
    assert.equal(options.scale, 2);
    assert.equal(options.keepHtml, true);
    assert.equal(parseArgs([]).scale, 1);
  });

  it("rejects bad input rather than guessing", () => {
    assert.throws(() => parseArgs(["--only=nope"]), /--only must be one of/);
    assert.throws(
      () => parseArgs(["--canvas=story"]),
      /--canvas must be one of/,
    );
    assert.throws(() => parseArgs(["--scale=0"]), /--scale must be an integer/);
    assert.throws(
      () => parseArgs(["--scale=1.5"]),
      /--scale must be an integer/,
    );
    assert.throws(() => parseArgs(["--wat"]), /unknown argument '--wat'/);
  });

  it("reads PNG dimensions and rejects non-PNG input", () => {
    const png = Buffer.concat([
      Buffer.from("89504e470d0a1a0a", "hex"),
      Buffer.from([0, 0, 0, 13]),
      Buffer.from("IHDR"),
      (() => {
        const b = Buffer.alloc(8);
        b.writeUInt32BE(1200, 0);
        b.writeUInt32BE(630, 4);
        return b;
      })(),
    ]);
    assert.deepEqual(pngSize(png), { w: 1200, h: 630 });
    assert.throws(
      () => pngSize(Buffer.from("not a png at all, really")),
      /not a PNG/,
    );
  });

  it("maps Inter file names to weights, longest match first", () => {
    assert.equal(weightFromName("Inter-Regular.woff2"), 400);
    assert.equal(weightFromName("Inter-SemiBold.woff2"), 600);
    assert.equal(weightFromName("Inter-ExtraBold.woff2"), 800);
    assert.equal(weightFromName("Inter-Bold.woff2"), 700);
    assert.equal(weightFromName("Inter-Light.ttf"), 300);
  });
});
