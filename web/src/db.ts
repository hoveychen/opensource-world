// Client-side full-database search via DuckDB-WASM.
//
// The aggregate step exports the whole repos table to a stars-DESC, ZSTD Parquet
// file (web/public/data/repos.parquet). Here we run DuckDB compiled to WASM right
// in the browser and point it at that file over HTTP. DuckDB issues HTTP *range*
// requests, so a `ORDER BY stars DESC LIMIT 40` query reads only the first row
// groups instead of downloading the whole ~hundreds-of-MB file — letting search
// cover the full ~2.5M repos with no backend, on a plain static host.
//
// WASM + worker assets are self-hosted (Vite `?url` imports) rather than pulled
// from a CDN, so the site has no third-party runtime dependency and the GitHub
// Pages base path is handled by Vite automatically.

import * as duckdb from "@duckdb/duckdb-wasm";
import mvp_wasm from "@duckdb/duckdb-wasm/dist/duckdb-mvp.wasm?url";
import mvp_worker from "@duckdb/duckdb-wasm/dist/duckdb-browser-mvp.worker.js?url";
import eh_wasm from "@duckdb/duckdb-wasm/dist/duckdb-eh.wasm?url";
import eh_worker from "@duckdb/duckdb-wasm/dist/duckdb-browser-eh.worker.js?url";

export type Repo = {
  full_name: string;
  stars: number;
  forks: number;
  language: string;
  description: string;
  html_url: string;
  topics: string[];
};

export type SearchFilter = { q: string; lang: string; topics: string[] };
export type SearchResult = { rows: Repo[]; total: number };

// The Parquet file is registered under this name inside DuckDB's virtual FS.
const PARQUET_VFS_NAME = "repos.parquet";

let connPromise: Promise<duckdb.AsyncDuckDBConnection> | null = null;

// connect lazily boots DuckDB-WASM once and registers the remote Parquet for
// range access. The promise is memoised so concurrent callers share one DB.
function connect(parquetURL: string): Promise<duckdb.AsyncDuckDBConnection> {
  if (connPromise) return connPromise;
  connPromise = (async () => {
    const bundle = await duckdb.selectBundle({
      mvp: { mainModule: mvp_wasm, mainWorker: mvp_worker },
      eh: { mainModule: eh_wasm, mainWorker: eh_worker },
    });
    const worker = new Worker(bundle.mainWorker!);
    const db = new duckdb.AsyncDuckDB(new duckdb.ConsoleLogger(), worker);
    await db.instantiate(bundle.mainModule, bundle.pthreadWorker);
    // HTTP protocol + directIO=false => DuckDB fetches byte ranges on demand.
    await db.registerFileURL(
      PARQUET_VFS_NAME,
      parquetURL,
      duckdb.DuckDBDataProtocol.HTTP,
      false
    );
    return db.connect();
  })().catch((e) => {
    // Reset so a later retry can re-attempt instead of resolving the failure.
    connPromise = null;
    throw e;
  });
  return connPromise;
}

// Kick off DuckDB init in the background (e.g. after first paint) so the first
// real search doesn't pay the whole boot cost. Errors are swallowed — search()
// will surface them (and the UI falls back to the in-memory top-1000 filter).
export function warmUp(parquetURL: string): void {
  connect(parquetURL).catch(() => {});
}

// Escape a LIKE/ILIKE pattern so user input is matched literally (the query
// appends its own surrounding %). Backslash is the ESCAPE char in the SQL below.
function likeLiteral(s: string): string {
  return s.replace(/[\\%_]/g, (c) => "\\" + c);
}

// buildWhere turns the filter into a SQL predicate plus positional params for a
// prepared statement (so user text can never break or inject the query).
function buildWhere(f: SearchFilter): { sql: string; params: string[] } {
  const clauses: string[] = [];
  const params: string[] = [];
  if (f.lang) {
    clauses.push("language = ?");
    params.push(f.lang);
  }
  for (const t of f.topics) {
    clauses.push("list_contains(topics, ?)");
    params.push(t);
  }
  const q = f.q.trim();
  if (q) {
    const like = "%" + likeLiteral(q) + "%";
    clauses.push(
      "(full_name ILIKE ? ESCAPE '\\' OR description ILIKE ? ESCAPE '\\'" +
        " OR len(list_filter(topics, x -> x ILIKE ? ESCAPE '\\')) > 0)"
    );
    params.push(like, like, like);
  }
  return { sql: clauses.length ? "WHERE " + clauses.join(" AND ") : "", params };
}

function toRepo(row: Record<string, unknown>): Repo {
  const t = row.topics;
  return {
    full_name: String(row.full_name ?? ""),
    stars: Number(row.stars ?? 0),
    forks: Number(row.forks ?? 0),
    language: String(row.language ?? ""),
    description: String(row.description ?? ""),
    html_url: String(row.html_url ?? ""),
    topics: t == null ? [] : Array.from(t as Iterable<unknown>, (x) => String(x)),
  };
}

// search runs the filter against the full Parquet dataset and returns the top
// `limit` matches by stars plus the exact total match count. Throws if DuckDB is
// unavailable — callers should fall back to the in-memory filter.
export async function search(
  parquetURL: string,
  f: SearchFilter,
  limit: number
): Promise<SearchResult> {
  const conn = await connect(parquetURL);
  const { sql: whereSQL, params } = buildWhere(f);
  const from = `read_parquet('${PARQUET_VFS_NAME}')`;

  const rowsStmt = await conn.prepare(
    `SELECT full_name, stars, forks, language, description, html_url, topics
     FROM ${from} ${whereSQL} ORDER BY stars DESC LIMIT ${limit}`
  );
  const countStmt = await conn.prepare(`SELECT count(*)::BIGINT AS n FROM ${from} ${whereSQL}`);
  try {
    const [rowsTbl, countTbl] = await Promise.all([
      rowsStmt.query(...params),
      countStmt.query(...params),
    ]);
    const rows = rowsTbl.toArray().map((r) => toRepo(r.toJSON() as Record<string, unknown>));
    const total = Number((countTbl.toArray()[0]?.toJSON() as { n: bigint }).n);
    return { rows, total };
  } finally {
    await rowsStmt.close();
    await countStmt.close();
  }
}
