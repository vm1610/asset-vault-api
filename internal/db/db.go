// Package db owns the SQLite-backed persistence layer for assets and the
// background jobs that process them. It is written against database/sql
// with portable SQL where practical, since the storage engine backing it
// may change (see ACTUAL_IMPLEMENTATION.md for the SQLite-vs-Postgres
// trade-off made in this build).
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS assets (
	id TEXT PRIMARY KEY,
	owner_sub TEXT NOT NULL,
	filename TEXT NOT NULL,
	content_type TEXT NOT NULL,
	size_bytes INTEGER NOT NULL,
	storage_key TEXT NOT NULL,
	thumbnail_key TEXT,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_assets_owner_sub ON assets(owner_sub);

CREATE TABLE IF NOT EXISTS jobs (
	id TEXT PRIMARY KEY,
	asset_id TEXT NOT NULL,
	status TEXT NOT NULL,
	error TEXT,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (asset_id) REFERENCES assets(id)
);

CREATE INDEX IF NOT EXISTS idx_jobs_asset_id ON jobs(asset_id);
`

// Open creates (if needed) the parent directory for path, opens a SQLite
// database there, and applies the schema.
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("db: creating data dir: %w", err)
		}
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("db: opening database: %w", err)
	}

	// SQLite handles one writer at a time; a single connection avoids
	// "database is locked" errors under concurrent access from this
	// process without needing a separate connection pool.
	conn.SetMaxOpenConns(1)

	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("db: applying schema: %w", err)
	}

	return conn, nil
}
