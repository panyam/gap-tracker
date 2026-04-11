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
`
