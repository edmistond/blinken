package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/edmistond/blinken/internal/core"
)

func openTemp(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "blinken.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func TestFreshDatabaseIsWALWithSchema(t *testing.T) {
	db, _ := openTemp(t)
	ctx := context.Background()
	mode, err := db.JournalMode(ctx)
	if err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v; want wal", mode, err)
	}
	v, err := db.SchemaVersion(ctx)
	if err != nil || v != len(migrations) {
		t.Fatalf("schema version = %d, %v; want %d", v, err, len(migrations))
	}
	var fk int
	if err := db.sql.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d, %v; want 1", fk, err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db, path := openTemp(t)
	db.Close()
	db2, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	v, _ := db2.SchemaVersion(context.Background())
	if v != len(migrations) {
		t.Fatalf("version after reopen = %d", v)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	db, _ := openTemp(t)
	g := &core.Guess{ProjectID: "does-not-exist", Summary: "orphan"}
	if err := db.InsertGuess(context.Background(), g); err == nil {
		t.Fatal("expected foreign key violation inserting guess for unknown project")
	}
}

// TestConcurrentWriters simulates many agent sessions writing at once. Each
// writer opens its own *sql.DB on the same file, which is what separate
// processes do. This is the Phase 0 proof that WAL + busy_timeout +
// BEGIN IMMEDIATE avoids ordinary SQLITE_BUSY failures.
func TestConcurrentWriters(t *testing.T) {
	db, path := openTemp(t)
	ctx := context.Background()
	pid, err := db.ResolveProject(ctx, "/tmp/proj", ".git")
	if err != nil {
		t.Fatal(err)
	}

	const writers, perWriter = 16, 50
	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)
	start := time.Now()
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			wdb, err := Open(ctx, path) // separate pool, like a separate process
			if err != nil {
				errs <- err
				return
			}
			defer wdb.Close()
			for i := 0; i < perWriter; i++ {
				g := &core.Guess{ProjectID: pid, Summary: fmt.Sprintf("writer %d guess %d", w, i)}
				if err := wdb.InsertGuess(ctx, g); err != nil {
					errs <- fmt.Errorf("writer %d: %w", w, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	gs, err := db.ListGuesses(ctx, Filter{ProjectID: pid})
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != writers*perWriter {
		t.Fatalf("got %d guesses, want %d", len(gs), writers*perWriter)
	}
	t.Logf("%d writers x %d inserts in %s", writers, perWriter, time.Since(start).Round(time.Millisecond))
}

func TestReviewStatusRoundTrip(t *testing.T) {
	db, _ := openTemp(t)
	ctx := context.Background()
	pid, _ := db.ResolveProject(ctx, "/tmp/proj", ".git")
	g := &core.Guess{ProjectID: pid, Summary: "retry once", Files: []core.FileRef{{Path: "a.go", Line: 3}}}
	if err := db.InsertGuess(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err := db.SetStatus(ctx, g.ID, core.StatusRejected, "no retries"); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetGuess(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != core.StatusRejected || got.ReviewNote != "no retries" || got.ReviewedAt == nil {
		t.Fatalf("unexpected review state: %+v", got)
	}
	if len(got.Files) != 1 || got.Files[0].Line != 3 {
		t.Fatalf("files not round-tripped: %+v", got.Files)
	}
	if err := db.SetStatus(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV", core.StatusAccepted, ""); err == nil {
		t.Fatal("expected not found")
	}
	c, err := db.CountGuesses(ctx, pid)
	if err != nil || c.Unreviewed != 0 {
		t.Fatalf("counts = %+v, %v", c, err)
	}
}
