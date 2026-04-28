package store

// schemaSQL is the canonical DDL for the gap-tracker SQLite database.
//
// We ship raw DDL instead of using GORM's AutoMigrate because AutoMigrate
// is unreliable as a "no-op on matching schema" primitive against SQLite —
// it tries to rewrite tables even when the diff is cosmetic (case
// differences in TEXT vs text, single- vs double-quoted defaults), and it
// has bugs around composite primary keys + foreign keys that corrupt the
// temp-table dance.
//
// This DDL is byte-compatible with the Python-era schema in main.py's
// _create_tables function. Keep the two in sync until Python is retired.
//
// Schema evolution: add new CREATE TABLE / CREATE INDEX / ALTER TABLE
// statements below the existing ones. CREATE TABLE IF NOT EXISTS is a
// no-op on existing tables, so this is safe to run on every Open().
const schemaSQL = `
CREATE TABLE IF NOT EXISTS issues (
    repo        TEXT NOT NULL,
    number      INTEGER NOT NULL,
    title       TEXT NOT NULL,
    state       TEXT NOT NULL,
    labels      TEXT DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    url         TEXT NOT NULL,
    body        TEXT DEFAULT '',
    synced_at   TEXT NOT NULL DEFAULT (datetime('now')),
    notes       TEXT DEFAULT '[]',
    PRIMARY KEY (repo, number)
);

CREATE TABLE IF NOT EXISTS gaps (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    feature         TEXT NOT NULL UNIQUE,
    description     TEXT DEFAULT '',
    target_issue    INTEGER,
    source_evidence TEXT DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'open',
    priority        TEXT DEFAULT '',
    notes           TEXT DEFAULT '[]',
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS sync_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    repo        TEXT NOT NULL,
    synced_at   TEXT NOT NULL DEFAULT (datetime('now')),
    issue_count INTEGER NOT NULL,
    offset_used INTEGER DEFAULT 0,
    count_used  INTEGER DEFAULT 100
);

CREATE TABLE IF NOT EXISTS triage (
    repo          TEXT NOT NULL,
    number        INTEGER NOT NULL,
    bucket        TEXT NOT NULL,
    target        TEXT DEFAULT '',
    rationale     TEXT DEFAULT '',
    test          TEXT DEFAULT '',
    classified_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (repo, number),
    FOREIGN KEY (repo, number) REFERENCES issues(repo, number)
);

-- Proposals track upstream enhancement proposals (SEPs, PEPs, JEPs, etc.)
-- that drive a project's roadmap. They overlay the issues table — a proposal
-- is always backed by an issue, but carries extra lifecycle metadata that
-- issues don't have (stage, priority, theme, release targeting).
CREATE TABLE IF NOT EXISTS proposals (
    repo        TEXT NOT NULL,
    number      INTEGER NOT NULL,
    kind        TEXT NOT NULL,          -- "SEP", "PEP", "JEP", etc.
    identifier  TEXT NOT NULL,          -- "SEP-2356", "PEP-484", etc.
    priority    TEXT DEFAULT '',        -- "P0", "P1", etc.
    stage       TEXT DEFAULT '',        -- "proposal", "draft", "in-review", "accepted", "accepted-with-changes", "final", "rejected"
    theme       TEXT DEFAULT '',        -- "Transport Evolution", "Enterprise Readiness"
    release     TEXT DEFAULT '',        -- "2026-06-30", target release version/date
    sponsor     TEXT DEFAULT '',        -- who is driving the SEP
    updated_at  TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (repo, number),
    FOREIGN KEY (repo, number) REFERENCES issues(repo, number)
);

CREATE INDEX IF NOT EXISTS idx_proposals_kind ON proposals(kind);
CREATE INDEX IF NOT EXISTS idx_proposals_release ON proposals(release);
CREATE INDEX IF NOT EXISTS idx_proposals_stage ON proposals(stage);

-- FTS5 full-text index over issues. External-content mode avoids
-- duplicating data — the virtual table reads from the issues table.
-- We use rowid as the content_rowid; SQLite assigns rowids to all
-- tables automatically (even those with composite primary keys).
-- Rebuild is triggered explicitly after UpsertIssues / SetNotes.
CREATE VIRTUAL TABLE IF NOT EXISTS issues_fts USING fts5(
    title, body, labels, notes,
    content=issues,
    content_rowid=rowid
);
`
