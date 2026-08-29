import { copyFile, mkdir, readdir, stat, rm } from "node:fs/promises";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = join(fileURLToPath(import.meta.url), "..");
const siteRoot = join(__dirname, "..");
const distDir = join(siteRoot, "dist");

const staticFiles = [
  "index.html",
  "privacy.html",
  "terms.html",
  "styles.css",
  "app.js",
  "server.mjs",
  "package.json",
];

const assetDirs = ["assets"];

async function copyRecursive(src, dest) {
  const entries = await readdir(src, { withFileTypes: true });
  await mkdir(dest, { recursive: true });
  for (const entry of entries) {
    const srcPath = join(src, entry.name);
    const destPath = join(dest, entry.name);
    if (entry.isDirectory()) {
      await copyRecursive(srcPath, destPath);
    } else {
      await copyFile(srcPath, destPath);
    }
  }
}

async function build() {
  console.log("Building Haechi website...");

  await rm(distDir, { recursive: true, force: true });
  await mkdir(distDir, { recursive: true });

  for (const file of staticFiles) {
    const src = join(siteRoot, file);
    const dest = join(distDir, file);
    try {
      await copyFile(src, dest);
      console.log(`  Copied ${file}`);
    } catch (e) {
      console.warn(`  Skipped ${file} (not found)`);
    }
  }

  for (const dir of assetDirs) {
    const src = join(siteRoot, dir);
    const dest = join(distDir, dir);
    try {
      await copyRecursive(src, dest);
      console.log(`  Copied ${dir}/`);
    } catch (e) {
      console.warn(`  Skipped ${dir}/ (not found)`);
    }
  }

  const runtimeDir = join(distDir, "runtime");
  await mkdir(runtimeDir, { recursive: true, mode: 0o700 });

  console.log(`\nBuild complete: ${distDir}`);
  console.log("Run with: node dist/server.mjs");
}

build().catch((e) => {
  console.error("Build failed:", e);
  process.exit(1);
});
