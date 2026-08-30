// Verifies that the brand specification and the stylesheet agree.
//
// The specification copies the implemented palette, so it can drift silently.
// This check fails when the two disagree, which is the only way a copied
// source of truth stays true.
//
//   node scripts/check-brand-palette.mjs
//
// Rules enforced:
//   1. Every hex colour in the stylesheet appears in the specification.
//   2. No withdrawn colour reappears in the stylesheet.
//
// Colours listed in the specification but absent from the stylesheet are
// allowed: the reserved extended palette exists precisely to be unused.

import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const siteRoot = join(fileURLToPath(import.meta.url), "..", "..");
const repoRoot = join(siteRoot, "..");
const stylesheet = join(siteRoot, "styles.css");
const specification = join(
  repoRoot,
  "dev-docs",
  "brand",
  "brand-specification.md",
);

// Withdrawn by decision. Reintroducing one is a brand regression, not a typo.
const WITHDRAWN = new Map([
  ["#00C4B4", "teal, removed 2026-08-30"],
  ["#1A1A1A", "dark charcoal, superseded by the surface scale"],
  ["#CCCCCC", "neutral grey, superseded by the warm text scale"],
]);

function hexColours(text) {
  const found = new Set();
  for (const match of text.matchAll(/#[0-9a-fA-F]{6}\b/g)) {
    found.add(match[0].toUpperCase());
  }
  return found;
}

async function main() {
  const [css, spec] = await Promise.all([
    readFile(stylesheet, "utf8"),
    readFile(specification, "utf8"),
  ]);

  const used = hexColours(css);
  const documented = hexColours(spec);
  const failures = [];

  for (const colour of [...used].sort()) {
    if (!documented.has(colour)) {
      failures.push(
        `${colour} is used in styles.css but absent from the brand specification`,
      );
    }
    const reason = WITHDRAWN.get(colour);
    if (reason) {
      failures.push(
        `${colour} is withdrawn (${reason}) but used in styles.css`,
      );
    }
  }

  if (failures.length > 0) {
    console.error("Brand palette check failed:\n");
    for (const failure of failures) console.error(`  ${failure}`);
    console.error(
      `\nUpdate dev-docs/brand/brand-specification.md in the same change as the stylesheet.`,
    );
    process.exit(1);
  }

  console.log(
    `Brand palette check passed: ${used.size} colours in styles.css, all documented, no withdrawn colours in use.`,
  );
}

main().catch((error) => {
  console.error(`Brand palette check errored: ${error.message}`);
  process.exit(1);
});
