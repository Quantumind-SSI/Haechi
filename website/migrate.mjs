// Applies the SQL migrations in db/ to the configured PostgreSQL database.
//
//   HAECHI_DATABASE_URL="postgresql://..." npm run migrate
//
// Migrations are applied in file-name order inside a single transaction each,
// and every migration is written to be idempotent, so re-running is safe.
//
// In Azure the database has no public endpoint, so this runs as a Container
// Apps job inside the virtual network rather than from a CI runner.

import { readdir, readFile } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const migrationsDir = join(fileURLToPath(import.meta.url), "..", "db");

async function main() {
  const connectionString = process.env.HAECHI_DATABASE_URL;
  if (!connectionString) {
    throw new Error("HAECHI_DATABASE_URL is required");
  }

  const files = (await readdir(migrationsDir))
    .filter((name) => name.endsWith(".sql"))
    .sort();
  if (files.length === 0) {
    throw new Error(`no .sql migrations found in ${migrationsDir}`);
  }

  const { default: pg } = await import("pg");
  const client = new pg.Client({
    connectionString,
    ssl:
      connectionString.includes("localhost") ||
      connectionString.includes("127.0.0.1")
        ? false
        : { rejectUnauthorized: true },
  });
  await client.connect();

  try {
    for (const file of files) {
      const sql = await readFile(join(migrationsDir, file), "utf8");
      await client.query("BEGIN");
      try {
        await client.query(sql);
        await client.query("COMMIT");
        console.log(`applied ${file}`);
      } catch (error) {
        await client.query("ROLLBACK");
        throw new Error(`${file} failed: ${error.message}`);
      }
    }
  } finally {
    await client.end();
  }
  console.log(`\n${files.length} migration(s) applied.`);
}

main().catch((error) => {
  console.error(`Migration failed: ${error.message}`);
  process.exit(1);
});
