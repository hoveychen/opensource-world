# opensource-world

Build a local database of (almost) every public, non-fork GitHub repository with
**at least 10 stars**, as the data foundation for a visualization project.

## Why two data sources

The exact target — *current, all non-fork repos with stars ≥ 10* — can't be served
by a single source, so this project combines two:

| Source | Role | Why |
|---|---|---|
| **GitHub Search API** | Enumerate the repo list | The only source that natively filters `stars:>=10 fork:false` on **current** data. |
| **ecosyste.ms** | Enrich each repo | Rich per-repo metadata (language, dependencies, SBOM, topics, license). |

### What we learned probing ecosyste.ms (June 2026)

- GitHub coverage on ecosyste.ms: ~286M repos, kept in sync (its live API is current).
- But the ecosyste.ms **list** API can't enumerate by stars: no star filter, any
  `sort` on the host-level `/repositories` endpoint returns 500, and pagination is
  hard-capped at **100 pages** (max 100k rows/query).
- Its downloadable **data dump** (`/open-data`) is a one-off snapshot frozen at
  **2023-08-30** (211 GB, ~168M repos) — too stale for a current view.

So ecosyste.ms is used for **enrichment**, not enumeration.

### The enumeration challenge

GitHub Search returns at most **1000 results per query**, but `stars:>=10 fork:false`
matches **~2.75M** repos (and a single bucket like `stars:10` alone is ~200k). The
crawler walks the keyspace by **recursively bisecting `stars × created-date`** until
every leaf window holds ≤ 1000 results, guaranteeing full coverage without relying on
sort.

## Stack

- **Language:** Go
- **Storage:** DuckDB (single-file, columnar — fast group-by/ranking for visualization)

## Auth

A GitHub token is required for the Search API. The crawler resolves it from, in order:
`GITHUB_TOKEN` → `GH_TOKEN` → `gh auth token`.

`enrich` joins the ecosyste.ms **polite pool** by sending a contact email as the
`mailto=` query parameter, which raises the rate limit from ~5000 to ~15000 req/hr
(≈3× faster). Set the email with `-mailto` or `ECOSYSTEMS_MAILTO`; pass `-mailto ""`
to stay anonymous. Note: only the `mailto=` query parameter engages the polite pool
— putting the email in the User-Agent does **not** (verified 2026-06).

## Usage

```bash
# Build (CGO required for the DuckDB driver)
CGO_ENABLED=1 go build -o bin/crawler ./cmd/crawler

# Verify the GitHub token resolves, then create the database
./bin/crawler token-check
./bin/crawler init-db

# 1) Enumerate via GitHub Search (resumable — re-run to continue after Ctrl-C).
#    Full run over ~2.75M repos is rate-limited to 30 req/min ⇒ many hours;
#    Ctrl-C any time and re-run to resume from where it stopped.
./bin/crawler enumerate                 # stars >= 10, all dates (the full target)
./bin/crawler enumerate -min-stars 1000 # a smaller slice to start with

# 2) Enrich each stored repo with ecosyste.ms metadata (resumable).
./bin/crawler enrich            # all pending
./bin/crawler enrich -limit 500 # just the first 500 pending (highest-star first)

# Inspect progress at any time
./bin/crawler stats
```

`DB_PATH` overrides the DuckDB file (default `data/repos.duckdb`).

### How resumability works

- **enumerate**: every fully-drained `(stars × created-date)` leaf window is recorded
  in `crawl_windows`; a re-run skips windows already done. Window bounds are
  deterministic (fixed star ceiling), so a re-run is an exact no-op once complete.
- **enrich**: repos are processed in `eco_synced_at IS NULL` order; each processed
  repo (including 404s) is stamped, so a re-run only picks up the remainder.

## Schema

`repos` — one row per repository (GitHub fields + `eco_*` enrichment columns).
`crawl_windows` — enumeration checkpoints for resumability.

Query the DuckDB file directly for visualization, e.g.:

```sql
SELECT language, count(*) AS repos, sum(stars) AS stars
FROM repos GROUP BY language ORDER BY stars DESC LIMIT 20;
```

## Running on GitHub Actions

`.github/workflows/crawl.yml` runs the whole crawl in CI so you don't have to keep
a local machine online. Each run:

1. restores the DuckDB from the `db` GitHub Release (asset `repos.duckdb.gz`),
2. builds the crawler,
3. runs `enumerate` then `enrich`, each bounded by `-max-runtime`,
4. gzips and re-uploads the DB to the release (`gh release upload --clobber`).

A `cron` (every 6h) chains runs; resumability means each picks up where the last
stopped. The `db` release is created automatically on the first run. Trigger
manually from the Actions tab ("Run workflow") to tune the inputs:

| Input | Default | Meaning |
|---|---|---|
| `min_stars` | `10` | lower star bound for enumerate |
| `enumerate_minutes` | `180` | time budget for enumerate (`0` to skip) |
| `enrich_minutes` | `120` | time budget for enrich (`0` to skip) |
| `mailto` | contact email | ecosyste.ms polite-pool address |

Notes:
- **Use a public repo.** Actions standard runners are free with no monthly cap on
  public repos; private repos are limited to 2000 min/month — far too little for the
  multi-day full crawl.
- The full DB approaches the 2 GB per-asset release limit; `repos.duckdb.gz` keeps
  headroom. If it ever exceeds 2 GB, switch the workflow to external object storage.
- Once enumeration is complete, set `enumerate_minutes=0` on the scheduled run to
  avoid re-counting the internal window tree each time, leaving the full budget for
  enrich.
- Per-job time is capped at 6h; `-max-runtime` makes each phase exit cleanly before
  then with progress saved.
