// Builds the self-contained Node origin bundle in dist/, which is what the
// container image runs.
//
// Every listed file is required. A missing file fails the build rather than
// producing a dist/ that starts and then crashes on first import.

import { copyFile, mkdir, readdir, rm } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const siteRoot = join(fileURLToPath(import.meta.url), "..", "..");
const distDir = join(siteRoot, "dist");

const requiredFiles = [
  "index.html",
  "privacy.html",
  "terms.html",
  "styles.css",
  "app.js",
  "config.js",
  "server.mjs",
  "storage.mjs",
  "migrate.mjs",
  "package.json",
];

const requiredDirs = ["assets", "db"];

async function copyRecursive(src, dest) {
  await mkdir(dest, { recursive: true });
  for (const entry of await readdir(src, { withFileTypes: true })) {
    const from = join(src, entry.name);
    const to = join(dest, entry.name);
    if (entry.isDirectory()) await copyRecursive(from, to);
    else await copyFile(from, to);
  }
}

async function build() {
  console.log("Building Haechi website...");

  await rm(distDir, { recursive: true, force: true });
  await mkdir(distDir, { recursive: true });

  for (const file of requiredFiles) {
    await copyFile(join(siteRoot, file), join(distDir, file));
    console.log(`  copied ${file}`);
  }

  for (const dir of requiredDirs) {
    await copyRecursive(join(siteRoot, dir), join(distDir, dir));
    console.log(`  copied ${dir}/`);
  }

  await mkdir(join(distDir, "runtime"), { recursive: true, mode: 0o700 });

  console.log(`\nBuild complete: ${distDir}`);
  console.log("Run with: node dist/server.mjs");
}

build().catch((error) => {
  console.error("Build failed:", error.message);
  process.exit(1);
});
