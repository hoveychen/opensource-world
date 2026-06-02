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

## Status

Under construction. See the crawler under `cmd/` once scaffolded.
