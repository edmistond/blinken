package storage

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// retryBusy retries fn on SQLITE_BUSY with jittered backoff, up to ~2s total.
func retryBusy(ctx context.Context, fn func() error) error {
	var err error
	delay := 5 * time.Millisecond
	for attempt := 0; attempt < 12; attempt++ {
		if err = fn(); err == nil || !isBusy(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay + time.Duration(rand.IntN(int(delay)))):
		}
		if delay < 250*time.Millisecond {
			delay *= 2
		}
	}
	return err
}

func isBusy(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked")
}

// migrations are applied in order inside one transaction each. Never edit a
// released entry; append a new one.
var migrations = []string{
	// 1: initial schema
	`
CREATE TABLE projects (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  created_at  TEXT NOT NULL
);
CREATE TABLE project_paths (
  path        TEXT PRIMARY KEY,
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  marker      TEXT NOT NULL DEFAULT '',
  first_seen  TEXT NOT NULL
);
CREATE TABLE sessions (
  id          TEXT PRIMARY KEY,
  project_id  TEXT REFERENCES projects(id) ON DELETE SET NULL,
  started_at  TEXT NOT NULL,
  ended_at    TEXT,
  agent       TEXT NOT NULL DEFAULT '',
  model       TEXT NOT NULL DEFAULT '',
  cwd         TEXT NOT NULL DEFAULT '',
  labels_json TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE guesses (
  id              TEXT PRIMARY KEY,
  project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  session_id      TEXT REFERENCES sessions(id) ON DELETE SET NULL,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  summary         TEXT NOT NULL,
  ambiguity       TEXT NOT NULL DEFAULT '',
  chosen_behavior TEXT NOT NULL DEFAULT '',
  kind            TEXT NOT NULL DEFAULT '',
  confidence      TEXT NOT NULL,
  impact          TEXT NOT NULL,
  reversibility   TEXT NOT NULL,
  reason          TEXT NOT NULL DEFAULT '',
  alternative     TEXT NOT NULL DEFAULT '',
  would_ask       TEXT NOT NULL DEFAULT '',
  cwd             TEXT NOT NULL DEFAULT '',
  cwd_rel         TEXT NOT NULL DEFAULT '',
  status          TEXT NOT NULL DEFAULT 'unreviewed',
  review_note     TEXT NOT NULL DEFAULT '',
  reviewed_at     TEXT,
  deleted_at      TEXT,
  subject_id      TEXT,
  metadata_json   TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX guesses_project_status ON guesses(project_id, status) WHERE deleted_at IS NULL;
CREATE TABLE guess_files (
  guess_id  TEXT NOT NULL REFERENCES guesses(id) ON DELETE CASCADE,
  path      TEXT NOT NULL,
  line      INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (guess_id, path, line)
);
`,
}

func (db *DB) migrate(ctx context.Context) error {
	// The bootstrap must run inside an explicit transaction. With _txlock=immediate
	// that takes the write lock up front, so concurrent fresh processes wait on
	// busy_timeout. An autocommit CREATE TABLE IF NOT EXISTS starts as a read and
	// upgrades to a write, and SQLite returns SQLITE_BUSY immediately on that
	// upgrade path without consulting the busy handler (deadlock avoidance).
	//
	// Even so, several processes opening a brand-new file at the same instant
	// can see SQLITE_BUSY from the initial WAL conversion, which SQLite does
	// not route through the busy handler. That happens once per database
	// lifetime, so a short bounded retry is the proportionate fix.
	err := retryBusy(ctx, func() error {
		return db.tx(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
				version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`)
			return err
		})
	})
	if err != nil {
		return fmt.Errorf("init migrations table: %w", err)
	}
	for i, stmt := range migrations {
		version := i + 1
		err := db.tx(ctx, func(tx *sql.Tx) error {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&exists); err != nil {
				return err
			}
			if exists > 0 {
				return nil
			}
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("migration %d: %w", version, err)
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, version, now().Format(timeLayout))
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion returns the highest applied migration.
func (db *DB) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	err := db.sql.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	return int(v.Int64), err
}
