package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/edmistond/blinken/internal/core"
)

const timeLayout = time.RFC3339Nano

var ErrNotFound = errors.New("not found")

func fmtTime(t time.Time) string { return t.UTC().Format(timeLayout) }
func parseTime(s string) time.Time {
	t, _ := time.Parse(timeLayout, s)
	return t
}

// ResolveProject returns the project id for a canonical root path, creating
// the project and path alias on first sight.
func (db *DB) ResolveProject(ctx context.Context, root, marker string) (string, error) {
	var id string
	err := db.tx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT project_id FROM project_paths WHERE path = ?`, root).Scan(&id)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		id = core.NewID()
		ts := fmtTime(now())
		if _, err := tx.ExecContext(ctx, `INSERT INTO projects (id, name, created_at) VALUES (?, ?, ?)`, id, filepath.Base(root), ts); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO project_paths (path, project_id, marker, first_seen) VALUES (?, ?, ?, ?)`, root, id, marker, ts)
		return err
	})
	return id, err
}

// Session is a logical agent run.
type Session struct {
	ID        string
	ProjectID string
	StartedAt time.Time
	EndedAt   *time.Time
	Agent     string
	Model     string
	Cwd       string
}

// StartSession inserts a session and returns its id.
func (db *DB) StartSession(ctx context.Context, s Session) (string, error) {
	s.ID = core.NewID()
	s.StartedAt = now()
	var pid any
	if s.ProjectID != "" {
		pid = s.ProjectID
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO sessions (id, project_id, started_at, agent, model, cwd) VALUES (?, ?, ?, ?, ?, ?)`,
		s.ID, pid, fmtTime(s.StartedAt), s.Agent, s.Model, s.Cwd)
	return s.ID, err
}

// EndSession marks a session ended. Ending twice is harmless.
func (db *DB) EndSession(ctx context.Context, id string) error {
	res, err := db.sql.ExecContext(ctx, `UPDATE sessions SET ended_at = COALESCE(ended_at, ?) WHERE id = ?`, fmtTime(now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("session %s: %w", id, ErrNotFound)
	}
	return nil
}

// InsertGuess validates and persists a guess, assigning id and timestamps.
func (db *DB) InsertGuess(ctx context.Context, g *core.Guess) error {
	if err := g.Validate(); err != nil {
		return err
	}
	g.ID = core.NewID()
	g.CreatedAt = now()
	g.UpdatedAt = g.CreatedAt
	var sid any
	if g.SessionID != "" {
		sid = g.SessionID
	}
	return db.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO guesses
			(id, project_id, session_id, created_at, updated_at, summary, ambiguity, chosen_behavior, kind,
			 confidence, impact, reversibility, reason, alternative, would_ask, cwd, cwd_rel, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			g.ID, g.ProjectID, sid, fmtTime(g.CreatedAt), fmtTime(g.UpdatedAt), g.Summary, g.Ambiguity, g.Chosen, g.Kind,
			g.Confidence, g.Impact, g.Reversibility, g.Reason, g.Alternative, g.WouldAsk, g.Cwd, g.CwdRel, g.Status)
		if err != nil {
			return err
		}
		for _, f := range g.Files {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO guess_files (guess_id, path, line) VALUES (?, ?, ?)`, g.ID, f.Path, f.Line); err != nil {
				return err
			}
		}
		return nil
	})
}

// Filter scopes list queries.
type Filter struct {
	ProjectID string
	SessionID string
	Statuses  []string
}

const guessCols = `id, project_id, COALESCE(session_id,''), created_at, updated_at, summary, ambiguity, chosen_behavior, kind,
	confidence, impact, reversibility, reason, alternative, would_ask, cwd, cwd_rel, status, review_note, COALESCE(reviewed_at,'')`

func scanGuess(row interface{ Scan(...any) error }) (core.Guess, error) {
	var g core.Guess
	var created, updated, reviewed string
	err := row.Scan(&g.ID, &g.ProjectID, &g.SessionID, &created, &updated, &g.Summary, &g.Ambiguity, &g.Chosen, &g.Kind,
		&g.Confidence, &g.Impact, &g.Reversibility, &g.Reason, &g.Alternative, &g.WouldAsk, &g.Cwd, &g.CwdRel, &g.Status, &g.ReviewNote, &reviewed)
	if err != nil {
		return g, err
	}
	g.CreatedAt, g.UpdatedAt = parseTime(created), parseTime(updated)
	if reviewed != "" {
		t := parseTime(reviewed)
		g.ReviewedAt = &t
	}
	return g, nil
}

// ListGuesses returns non-deleted guesses matching the filter, unsorted.
func (db *DB) ListGuesses(ctx context.Context, f Filter) ([]core.Guess, error) {
	where := []string{"deleted_at IS NULL"}
	var args []any
	if f.ProjectID != "" {
		where = append(where, "project_id = ?")
		args = append(args, f.ProjectID)
	}
	if f.SessionID != "" {
		where = append(where, "session_id = ?")
		args = append(args, f.SessionID)
	}
	if len(f.Statuses) > 0 {
		where = append(where, "status IN ("+strings.Repeat("?,", len(f.Statuses)-1)+"?)")
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+guessCols+` FROM guesses WHERE `+strings.Join(where, " AND ")+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Guess
	for rows.Next() {
		g, err := scanGuess(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := db.attachFiles(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (db *DB) attachFiles(ctx context.Context, gs []core.Guess) error {
	if len(gs) == 0 {
		return nil
	}
	idx := make(map[string]int, len(gs))
	ids := make([]any, len(gs))
	for i, g := range gs {
		idx[g.ID] = i
		ids[i] = g.ID
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT guess_id, path, line FROM guess_files WHERE guess_id IN (`+strings.Repeat("?,", len(ids)-1)+`?) ORDER BY path, line`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var f core.FileRef
		if err := rows.Scan(&id, &f.Path, &f.Line); err != nil {
			return err
		}
		gs[idx[id]].Files = append(gs[idx[id]].Files, f)
	}
	return rows.Err()
}

// GetGuess fetches one guess by id.
func (db *DB) GetGuess(ctx context.Context, id string) (core.Guess, error) {
	g, err := scanGuess(db.sql.QueryRowContext(ctx, `SELECT `+guessCols+` FROM guesses WHERE id = ? AND deleted_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return g, fmt.Errorf("guess %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return g, err
	}
	gs := []core.Guess{g}
	if err := db.attachFiles(ctx, gs); err != nil {
		return g, err
	}
	return gs[0], nil
}

// SetStatus updates review state in place.
func (db *DB) SetStatus(ctx context.Context, id, status, note string) error {
	g := core.Guess{Summary: "x", Status: status}
	if err := g.Validate(); err != nil {
		return err
	}
	ts := fmtTime(now())
	res, err := db.sql.ExecContext(ctx, `UPDATE guesses SET status = ?, review_note = ?, reviewed_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		status, note, ts, ts, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("guess %s: %w", id, ErrNotFound)
	}
	return nil
}

// Counts is the one-line summary for `blinken guesses`.
type Counts struct {
	Unreviewed    int `json:"unreviewed"`
	Followup      int `json:"followup"`
	HighImpact    int `json:"high_impact"`
	LowConfidence int `json:"low_confidence"`
}

// CountGuesses summarizes unresolved guesses for a project.
func (db *DB) CountGuesses(ctx context.Context, projectID string) (Counts, error) {
	var c Counts
	err := db.sql.QueryRowContext(ctx, `SELECT
		SUM(status = 'unreviewed'),
		SUM(status = 'followup'),
		SUM(status IN ('unreviewed','followup') AND impact IN ('high','critical')),
		SUM(status IN ('unreviewed','followup') AND confidence = 'low')
		FROM guesses WHERE project_id = ? AND deleted_at IS NULL`, projectID).
		Scan(&nullInt{&c.Unreviewed}, &nullInt{&c.Followup}, &nullInt{&c.HighImpact}, &nullInt{&c.LowConfidence})
	return c, err
}

type nullInt struct{ p *int }

func (n *nullInt) Scan(v any) error {
	switch x := v.(type) {
	case nil:
		*n.p = 0
	case int64:
		*n.p = int(x)
	default:
		return fmt.Errorf("unexpected count type %T", v)
	}
	return nil
}
