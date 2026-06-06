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
    eco_dependencies VARCHAR,      -- JSON (reserved; not yet populated)
    -- ecosyste.ms enrichment: community-health & bus-factor signals
    eco_subscribers      INTEGER,  -- true watchers/subscribers (GitHub Search's "watchers" is actually the star count)
    eco_total_commits    INTEGER,  -- commit_stats.total_commits
    eco_total_committers INTEGER,  -- commit_stats.total_committers
    eco_dds              DOUBLE,   -- commit_stats.dds: developer distribution score (lower = more bus-factor risk)
    eco_tags_count       INTEGER,  -- number of git tags / releases
    eco_files            VARCHAR,  -- JSON array of present governance-file kinds (readme, contributing, security, ...)
    eco_scorecard_score  DOUBLE    -- OSSF Scorecard aggregate score (0-10); NULL when no scorecard
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
    -- TRUE for an interior bisection node whose whole subtree is drained: a
    -- resume short-circuits the subtree with one lookup instead of re-counting
    -- every split. Interior windows OVERLAP their children, so coverage and the
    -- windows-done stat must count leaf windows only (interior IS NOT TRUE).
    interior    BOOLEAN DEFAULT FALSE,
    PRIMARY KEY (star_min, star_max, date_min, date_max)
);
