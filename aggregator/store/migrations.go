package store

import "database/sql"

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS agents (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    api_key          TEXT NOT NULL UNIQUE,
    api_secret       TEXT NOT NULL,
    registered_at    TEXT NOT NULL,
    last_seen_at     TEXT,
    enabled          INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS metric_batches (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    captured_at TEXT NOT NULL,
    received_at TEXT NOT NULL,
    sequence    INTEGER NOT NULL,
    buffered    INTEGER NOT NULL DEFAULT 0,
    payload     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_batches_agent_time
    ON metric_batches(agent_id, captured_at);
`)
	return err
}
