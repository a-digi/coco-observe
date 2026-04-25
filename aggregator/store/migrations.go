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
    enabled          INTEGER NOT NULL DEFAULT 1,
    binary_amd64     TEXT,
    binary_arm64     TEXT,
    processes        TEXT NOT NULL DEFAULT '[]',
    track_os         INTEGER NOT NULL DEFAULT 1
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

CREATE INDEX IF NOT EXISTS idx_batches_agent_received
    ON metric_batches(agent_id, received_at);
`)
	if err != nil {
		return err
	}
	// Idempotent columns for existing databases that predate this migration.
	for _, stmt := range []string{
		`ALTER TABLE agents ADD COLUMN binary_amd64 TEXT`,
		`ALTER TABLE agents ADD COLUMN binary_arm64 TEXT`,
		`ALTER TABLE agents ADD COLUMN processes TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE agents ADD COLUMN track_os INTEGER NOT NULL DEFAULT 1`,
	} {
		_, _ = db.Exec(stmt)
	}
	// Drop the unique dedup index if it was created by a previous version.
	_, _ = db.Exec(`DROP INDEX IF EXISTS idx_batches_dedup`)
	return nil
}
