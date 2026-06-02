-- Core table: one row per GitHub repository (non-fork, stars >= 10).
-- Populated by the GitHub Search enumerator, then enriched from ecosyste.ms.
CREATE TABLE IF NOT EXISTS repos (
    repo_id          BIGINT PRIMARY KEY,
    full_name        VARCHAR NOT NULL,
    owner            VARCHAR,
    name             VARCHAR,
    description      VARCHAR,
    stars            INTEGER,
    forks            INTEGER,
    open_issues      INTEGER,
    watchers         INTEGER,
    language         VARCHAR,
    topics           VARCHAR,      -- JSON array as text
    license          VARCHAR,
    homepage         VARCHAR,
    size_kb          INTEGER,
    default_branch   VARCHAR,
    archived         BOOLEAN,
    is_fork          BOOLEAN,
    html_url         VARCHAR,
    created_at       TIMESTAMP,
    pushed_at        TIMESTAMP,
    updated_at       TIMESTAMP,
    -- bookkeeping
    source_synced_at TIMESTAMP,    -- when the GitHub Search row was captured
    eco_synced_at    TIMESTAMP,    -- when ecosyste.ms enrichment ran (NULL = pending)
    eco_language     VARCHAR,
    eco_license      VARCHAR,
    eco_topics       VARCHAR,      -- JSON array as text
    eco_dependencies VARCHAR       -- JSON
);

CREATE INDEX IF NOT EXISTS idx_repos_stars ON repos(stars);
CREATE INDEX IF NOT EXISTS idx_repos_language ON repos(language);
CREATE INDEX IF NOT EXISTS idx_repos_eco_pending ON repos(eco_synced_at);

-- Resumable enumeration: records each fully-fetched (stars x created-date) window
-- so a re-run skips windows already drained. star_max = -1 means unbounded (>=).
CREATE TABLE IF NOT EXISTS crawl_windows (
    star_min    INTEGER NOT NULL,
    star_max    INTEGER NOT NULL,
    date_min    DATE NOT NULL,
    date_max    DATE NOT NULL,
    total_count INTEGER,           -- total_count GitHub reported for the window
    fetched     INTEGER,           -- rows we actually stored from it
    done_at     TIMESTAMP,
    PRIMARY KEY (star_min, star_max, date_min, date_max)
);
