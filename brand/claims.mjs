/**
 * Claim-safety linter for Haechi visual posts.
 *
 * Four documents in this repository independently prohibit specific marketing
 * claims, because the implementation does not support them:
 *
 * 1. `dev-docs/README.md`, Claim Risk Notice: the legacy product material
 *    "must not be issued to customers, investors, or partners as descriptions
 *    of current capability until the corresponding release gates pass and
 *    ADR-010 defines permitted claims".
 * 2. `dev-docs/brand/brand-specification.md`, Voice: do not reuse "provably
 *    anonymous", "quantum-resistant" or "cryptographic no-logs guarantee".
 * 3. `dev-docs/adr/adr-004-capability-trust-model.md`: "No production
 *    'anonymous' or 'zero-knowledge' marketing claim until ADR-010 evidence
 *    and an external cryptographic review exist."
 * 4. `dev-docs/integration/haechi-integration-document.html` section 16:
 *    marketing claims must be traceable to test reports or audits.
 *
 * A prose rule that lives only in a document gets broken. This module makes
 * the rules executable, and `render.mjs` fails closed on any `fail` finding,
 * so a prohibited claim cannot reach a rendered asset.
 *
 * It also enforces C-08 of the engineering constitution: zero em dashes.
 */

import { WITHDRAWN_COLOURS } from "./tokens.mjs";

/** @typedef {"fail" | "warn"} Severity */

/**
 * @typedef {object} Finding
 * @property {string} rule Stable rule identifier.
 * @property {Severity} severity `fail` blocks the render, `warn` is advisory.
 * @property {string} where Human-readable location, e.g. `e1-security-gap.headline`.
 * @property {string} match The exact offending text.
 * @property {string} excerpt Surrounding context for the reader.
 * @property {string} reason Why the rule exists.
 * @property {string} citation The document that imposes the rule.
 */

const SPEC = "dev-docs/brand/brand-specification.md";
const ADR4 = "dev-docs/adr/adr-004-capability-trust-model.md";
const NOTICE = "dev-docs/README.md, Claim Risk Notice";
const TERMS = "website/terms.html, no-security-guarantee clause";
const C08 = "Engineering constitution C-08";

/**
 * Prohibited and guarded patterns.
 *
 * Patterns are deliberately narrow. "post-quantum transition" and "PQ
 * reinforced" are approved public language and must not trip the quantum
 * rules, so those match only the claim forms ("quantum-resistant",
 * "quantum-safe") and never the bare word.
 *
 * @type {ReadonlyArray<{id: string, pattern: RegExp, severity: Severity, reason: string, citation: string}>}
 */
export const RULES = Object.freeze([
  {
    id: "claim/provable-anonymity",
    pattern: /\bprovabl[ey]\s+anonymous\b|\bprovable\s+anonymity\b/gi,
    severity: "fail",
    reason: "Anonymity is not implemented or externally reviewed.",
    citation: SPEC,
  },
  {
    id: "claim/anonymity",
    pattern: /\banonymous\b|\banonymity\b|\banonymise[sd]?\b/gi,
    severity: "fail",
    reason:
      "No production anonymity claim is permitted before ADR-010 evidence and external cryptographic review.",
    citation: ADR4,
  },
  {
    id: "claim/zero-knowledge",
    pattern:
      /\bzero[- ]knowledge\b|\bZKO\b|\bzero[- ]knowledge\s+orchestration\b/gi,
    severity: "fail",
    reason: "No production zero-knowledge claim is permitted before ADR-010.",
    citation: ADR4,
  },
  {
    id: "claim/quantum-resistance",
    pattern: /\bquantum[- ](resistant|resistance|proof|safe|secure)\b/gi,
    severity: "fail",
    reason:
      "Post-quantum reinforcement is simulated. Say 'post-quantum transition' or 'PQ reinforced' instead.",
    citation: SPEC,
  },
  {
    id: "claim/no-logs",
    pattern: /\bno[- ]logs?\b|\bzero[- ]logs?\b/gi,
    severity: "fail",
    reason: "There is no implemented or audited no-logs property.",
    citation: SPEC,
  },
  {
    id: "claim/security-guarantee",
    pattern:
      /\b(security|privacy|cryptographic|cryptographically)\s+guarantee[sd]?\b|\bguarantee[sd]?\s+(security|privacy|protection)\b/gi,
    severity: "fail",
    reason:
      "The published terms state there is no security guarantee. Claim design intent, not outcome.",
    citation: TERMS,
  },
  {
    id: "claim/traffic-morphing",
    pattern: /\btraffic\s+morphing\b|\bAI[- ]driven\s+traffic\b/gi,
    severity: "fail",
    reason: "Traffic morphing is legacy material, not implemented behaviour.",
    citation: NOTICE,
  },
  {
    id: "claim/absolute-security",
    pattern:
      /\bunbreakable\b|\bunhackable\b|\bimpenetrable\b|\buntraceable\b|\bundetectable\b|\b(military|bank|intelligence|defen[cs]e)[- ]grade\b|\b100%\s+secure\b|\bfully\s+secure\b/gi,
    severity: "fail",
    reason: "Absolute-security language is unverifiable and off-voice.",
    citation: SPEC,
  },
  {
    id: "voice/superlative",
    pattern:
      /\brevolutionary\b|\bcutting[- ]edge\b|\bworld[- ]class\b|\bnext[- ]generation\b|\bgame[- ]chang\w*\b|\bbest[- ]in[- ]class\b|\bindustry[- ]leading\b|\bstate[- ]of[- ]the[- ]art\b|\bunparalleled\b|\bseamless(ly)?\b|\beffortless(ly)?\b/gi,
    severity: "fail",
    reason:
      "The shipped voice uses no superlatives. Claims are hedged with process, not adjectives.",
    citation: SPEC,
  },
  {
    id: "style/em-dash",
    pattern: /\u2014/g,
    severity: "fail",
    reason:
      "Em dashes are prohibited in all writing. Use a period, comma, colon, or restructure.",
    citation: C08,
  },
  {
    id: "guard/vpn",
    pattern: /\bVPN\b/gi,
    severity: "warn",
    reason:
      "The shipped site never uses this word, and readiness.md states this is not a production VPN.",
    citation: "dev-docs/engineering/readiness.md",
  },
  {
    id: "guard/unlinkable",
    pattern: /\bunlinkab\w+\b/gi,
    severity: "warn",
    reason:
      "Approved on the public roadmap as a prototype. Keep the prototype framing attached.",
    citation: ADR4,
  },
]);

/** Number of characters of context shown on each side of a match. */
const EXCERPT_RADIUS = 36;

/**
 * Build a readable excerpt around a match.
 *
 * @param {string} text The full text being linted.
 * @param {number} index Start offset of the match.
 * @param {number} length Length of the match.
 * @returns {string} Context with the match delimited by `[[` and `]]`.
 */
function excerptAt(text, index, length) {
  const from = Math.max(0, index - EXCERPT_RADIUS);
  const to = Math.min(text.length, index + length + EXCERPT_RADIUS);
  const lead = from > 0 ? "..." : "";
  const tail = to < text.length ? "..." : "";
  const before = text.slice(from, index);
  const hit = text.slice(index, index + length);
  const after = text.slice(index + length, to);
  return `${lead}${before}[[${hit}]]${after}${tail}`.replace(/\s+/g, " ");
}

/**
 * Lint one string against every rule.
 *
 * Each rule's `lastIndex` is reset before use because the patterns are global
 * and shared across calls. Bounded: at most `RULES.length` passes over `text`.
 *
 * @param {string} text Text to check.
 * @param {string} where Location label reported in findings.
 * @returns {Finding[]} Empty when the text is clean.
 */
export function lintText(text, where) {
  if (typeof text !== "string" || text.length === 0) return [];

  /** @type {Finding[]} */
  const findings = [];
  for (const rule of RULES) {
    rule.pattern.lastIndex = 0;
    let hit = rule.pattern.exec(text);
    while (hit !== null) {
      findings.push({
        rule: rule.id,
        severity: rule.severity,
        where,
        match: hit[0],
        excerpt: excerptAt(text, hit.index, hit[0].length),
        reason: rule.reason,
        citation: rule.citation,
      });
      if (hit[0].length === 0) rule.pattern.lastIndex += 1;
      hit = rule.pattern.exec(text);
    }
  }
  return findings;
}

/**
 * Recursively collect every string in a value, with a dotted path label.
 *
 * @param {unknown} value Any JSON-shaped value.
 * @param {string} path Path prefix for labels.
 * @param {Array<{path: string, text: string}>} out Accumulator, mutated.
 * @returns {void}
 */
function collectStrings(value, path, out) {
  if (typeof value === "string") {
    out.push({ path, text: value });
    return;
  }
  if (Array.isArray(value)) {
    value.forEach((item, i) => collectStrings(item, `${path}[${i}]`, out));
    return;
  }
  if (value !== null && typeof value === "object") {
    for (const [key, child] of Object.entries(value)) {
      collectStrings(child, path === "" ? key : `${path}.${key}`, out);
    }
  }
}

/**
 * Lint every string field of a post definition.
 *
 * @param {{id: string}} post A post from `posts.mjs`.
 * @returns {Finding[]}
 */
export function lintPost(post) {
  /** @type {Array<{path: string, text: string}>} */
  const strings = [];
  collectStrings(post, "", strings);
  return strings.flatMap((entry) =>
    lintText(entry.text, `${post.id}.${entry.path}`),
  );
}

/**
 * Detect withdrawn palette colours in generated markup.
 *
 * The logo mark legitimately contains `#ff4500`, `#0b0b0c` and `#fff`; the
 * last is the one sanctioned use of pure white, per the brand specification.
 * The mark is therefore removed before scanning rather than special-cased per
 * colour.
 *
 * @param {string} html Generated markup.
 * @param {string} where Location label.
 * @returns {Finding[]}
 */
export function lintColours(html, where) {
  const withoutMark = html.replace(/<svg[\s\S]*?<\/svg>/g, "");
  /** @type {Finding[]} */
  const findings = [];
  for (const hex of WITHDRAWN_COLOURS) {
    const short =
      hex.length === 7 &&
      hex[1] === hex[2] &&
      hex[3] === hex[4] &&
      hex[5] === hex[6]
        ? `#${hex[1]}${hex[3]}${hex[5]}`
        : hex;
    const pattern = new RegExp(`${hex}\\b|${short}\\b`, "gi");
    let hit = pattern.exec(withoutMark);
    while (hit !== null) {
      findings.push({
        rule: "palette/withdrawn-colour",
        severity: "fail",
        where,
        match: hit[0],
        excerpt: excerptAt(withoutMark, hit.index, hit[0].length),
        reason: `${hex} was withdrawn from the palette on 2026-08-30.`,
        citation: SPEC,
      });
      hit = pattern.exec(withoutMark);
    }
  }
  return findings;
}

/**
 * Render findings as an aligned, readable report.
 *
 * @param {Finding[]} findings
 * @returns {string} Empty string when there is nothing to report.
 */
export function formatFindings(findings) {
  if (findings.length === 0) return "";
  return findings
    .map((f) => {
      const head = `${f.severity.toUpperCase()} ${f.rule} at ${f.where}`;
      return `${head}\n    match:    ${f.match}\n    context:  ${f.excerpt}\n    reason:   ${f.reason}\n    citation: ${f.citation}`;
    })
    .join("\n\n");
}

/**
 * Throw if any finding is a hard failure. Warnings are returned for the caller
 * to print.
 *
 * @param {Finding[]} findings
 * @returns {Finding[]} The `warn`-severity findings.
 * @throws {Error} If any finding has severity `fail`.
 */
export function assertClean(findings) {
  const failures = findings.filter((f) => f.severity === "fail");
  if (failures.length > 0) {
    throw new Error(
      `claim safety gate failed with ${failures.length} violation(s):\n\n${formatFindings(failures)}`,
    );
  }
  return findings.filter((f) => f.severity === "warn");
}
