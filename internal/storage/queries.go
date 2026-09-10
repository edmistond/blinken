package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/edmistond/blinken/internal/core"
)

const timeLayout = time.RFC3339Nano

var (
	ErrNotFound  = errors.New("not found")
	ErrAmbiguous = errors.New("prefix matches more than one guess")
)

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
	ID        string            `json:"id"`
	ProjectID string            `json:"project_id,omitempty"`
	StartedAt time.Time         `json:"started_at"`
	EndedAt   *time.Time        `json:"ended_at,omitempty"`
	Agent     string            `json:"agent,omitempty"`
	Model     string            `json:"model,omitempty"`
	Cwd       string            `json:"cwd,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Guesses   int               `json:"guesses"`
}

// StartSession inserts a session and returns its id.
func (db *DB) StartSession(ctx context.Context, s Session) (string, error) {
	s.ID = core.NewID()
	s.StartedAt = now()
	var pid any
	if s.ProjectID != "" {
		pid = s.ProjectID
	}
	labels := "{}"
	if len(s.Labels) > 0 {
		b, err := json.Marshal(s.Labels)
		if err != nil {
			return "", err
		}
		labels = string(b)
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO sessions (id, project_id, started_at, agent, model, cwd, labels_json) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.ID, pid, fmtTime(s.StartedAt), s.Agent, s.Model, s.Cwd, labels)
	return s.ID, err
}

// ListSessions returns sessions for a project, newest first, with guess counts.
func (db *DB) ListSessions(ctx context.Context, projectID string) ([]Session, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT s.id, COALESCE(s.project_id,''), s.started_at, COALESCE(s.ended_at,''), s.agent, s.model, s.cwd, s.labels_json,
		(SELECT COUNT(*) FROM guesses g WHERE g.session_id = s.id AND g.deleted_at IS NULL)
		FROM sessions s WHERE (? = '' OR s.project_id = ?) ORDER BY s.started_at DESC, s.id DESC`, projectID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var s Session
		var started, ended, labels string
		if err := rows.Scan(&s.ID, &s.ProjectID, &started, &ended, &s.Agent, &s.Model, &s.Cwd, &labels, &s.Guesses); err != nil {
			return nil, err
		}
		s.StartedAt = parseTime(started)
		if ended != "" {
			t := parseTime(ended)
			s.EndedAt = &t
		}
		if labels != "" && labels != "{}" {
			_ = json.Unmarshal([]byte(labels), &s.Labels)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ProjectInfo describes a known project for selection lists.
type ProjectInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Root       string `json:"root"`
	Unresolved int    `json:"unresolved"`
}

// ListProjects returns every project with its first observed root path.
func (db *DB) ListProjects(ctx context.Context) ([]ProjectInfo, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT p.id, p.name,
		COALESCE((SELECT path FROM project_paths pp WHERE pp.project_id = p.id ORDER BY first_seen LIMIT 1), ''),
		(SELECT COUNT(*) FROM guesses g WHERE g.project_id = p.id AND g.deleted_at IS NULL AND g.status IN ('unreviewed','followup'))
		FROM projects p ORDER BY p.name, p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectInfo
	for rows.Next() {
		var p ProjectInfo
		if err := rows.Scan(&p.ID, &p.Name, &p.Root, &p.Unresolved); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
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

// ListGuesses returns guesses matching the filter, oldest first (callers sort).
func (db *DB) ListGuesses(ctx context.Context, f Filter) ([]core.Guess, error) {
	where, args := f.where()
	rows, err := db.sql.QueryContext(ctx, `SELECT `+guessCols+` FROM guesses WHERE `+where+` ORDER BY created_at, id`, args...)
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

// CountWhere returns how many guesses match, for confirmation prompts.
func (db *DB) CountWhere(ctx context.Context, f Filter) (int, error) {
	where, args := f.where()
	var n int
	err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM guesses WHERE `+where, args...).Scan(&n)
	return n, err
}

// ResolveGuessID accepts a full id or a unique prefix (case-insensitive).
func (db *DB) ResolveGuessID(ctx context.Context, prefix string) (string, error) {
	prefix = strings.ToUpper(strings.TrimSpace(prefix))
	if prefix == "" {
		return "", fmt.Errorf("empty id: %w", ErrNotFound)
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT id FROM guesses WHERE id >= ? AND id < ? LIMIT 2`, prefix, prefix+"~")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("guess %s: %w", prefix, ErrNotFound)
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("guess %s: %w", prefix, ErrAmbiguous)
	}
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

// BulkSetStatus applies one review state to every guess matching the filter
// in a single transaction and returns how many changed.
func (db *DB) BulkSetStatus(ctx context.Context, f Filter, status, note string) (int, error) {
	g := core.Guess{Summary: "x", Status: status}
	if err := g.Validate(); err != nil {
		return 0, err
	}
	where, args := f.where()
	ts := fmtTime(now())
	var n int64
	err := db.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE guesses SET status = ?, review_note = ?, reviewed_at = ?, updated_at = ? WHERE `+where,
			append([]any{status, note, ts, ts}, args...)...)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return int(n), err
}

// SoftDelete hides one guess. Deleting twice is not an error.
func (db *DB) SoftDelete(ctx context.Context, id string) error {
	res, err := db.sql.ExecContext(ctx, `UPDATE guesses SET deleted_at = COALESCE(deleted_at, ?), updated_at = ? WHERE id = ?`, fmtTime(now()), fmtTime(now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("guess %s: %w", id, ErrNotFound)
	}
	return nil
}

// SoftDeleteWhere hides every guess matching the filter.
func (db *DB) SoftDeleteWhere(ctx context.Context, f Filter) (int, error) {
	f.OnlyDeleted = false
	where, args := f.where()
	ts := fmtTime(now())
	res, err := db.sql.ExecContext(ctx, `UPDATE guesses SET deleted_at = ?, updated_at = ? WHERE `+where, append([]any{ts, ts}, args...)...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// PurgeDeleted permanently removes soft-deleted guesses (and their file
// references, by cascade). SQLite does not shrink the file; free pages are
// reused by later writes. Run VACUUM by hand to reclaim disk space.
func (db *DB) PurgeDeleted(ctx context.Context, f Filter) (int, error) {
	f.OnlyDeleted = true
	where, args := f.where()
	res, err := db.sql.ExecContext(ctx, `DELETE FROM guesses WHERE `+where, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
