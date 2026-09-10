// Package storage is the SQLite persistence layer.
//
// Driver: modernc.org/sqlite (pure Go, no CGO). Connection settings that
// matter for concurrent local writers are set in the DSN:
//   - journal_mode=WAL      readers never block the single writer
//   - busy_timeout=5000ms   a second writer waits instead of failing
//   - _txlock=immediate     write transactions take the lock up front, which
//     avoids SQLITE_BUSY on read-to-write lock upgrades
//   - foreign_keys=1        enforced per connection, so it lives in the DSN
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"

	_ "modernc.org/sqlite"
)

const busyTimeoutMs = 5000

// DB wraps the connection pool.
type DB struct {
	sql  *sql.DB
	Path string
}

// DefaultPath follows OS conventions from spec section 13.
func DefaultPath() (string, error) {
	if p := os.Getenv("BLINKEN_DB"); p != "" {
		return p, nil
	}
	var base string
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support")
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("LOCALAPPDATA is not set")
		}
	default:
		base = os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".local", "share")
		}
	}
	return filepath.Join(base, "blinken", "blinken.db"), nil
}

// DSN builds the modernc connection string for a file path.
func DSN(path string) string {
	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeoutMs))
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Set("_txlock", "immediate")
	return "file:" + filepath.ToSlash(path) + "?" + q.Encode()
}

// Open creates the parent directory if needed, opens the database, and runs migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	sqldb, err := sql.Open("sqlite", DSN(path))
	if err != nil {
		return nil, err
	}
	// One process is one writer. A single connection keeps transactions
	// serialized in-process; cross-process contention is handled by SQLite.
	sqldb.SetMaxOpenConns(1)
	sqldb.SetConnMaxLifetime(0)
	db := &DB{sql: sqldb, Path: path}
	if err := db.migrate(ctx); err != nil {
		sqldb.Close()
		return nil, err
	}
	return db, nil
}

// Close releases the pool.
func (db *DB) Close() error { return db.sql.Close() }

// JournalMode reports the active journal mode; used by tests to prove WAL.
func (db *DB) JournalMode(ctx context.Context) (string, error) {
	var mode string
	err := db.sql.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode)
	return mode, err
}

// tx runs fn inside a short write transaction.
func (db *DB) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }
