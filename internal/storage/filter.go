package storage

import (
	"strings"
	"time"
)

// Filter scopes list, count, bulk-status, and delete queries.
type Filter struct {
	ProjectID     string
	SessionID     string
	Statuses      []string
	Kinds         []string
	Confidences   []string
	Impacts       []string
	Since         time.Time // created_at >= Since when non-zero
	Before        time.Time // created_at < Before when non-zero
	IDs           []string  // explicit selection
	OnlyDeleted   bool      // soft-deleted rows only (for purge)
	DeletedBefore time.Time // with OnlyDeleted: deleted_at < DeletedBefore
}

func inClause(col string, vals []string, args *[]any) string {
	for _, v := range vals {
		*args = append(*args, v)
	}
	return col + " IN (" + strings.Repeat("?,", len(vals)-1) + "?)"
}

// where renders the filter as SQL. Soft-deleted rows are excluded unless
// OnlyDeleted is set.
func (f Filter) where() (string, []any) {
	var w []string
	var args []any
	if f.OnlyDeleted {
		w = append(w, "deleted_at IS NOT NULL")
		if !f.DeletedBefore.IsZero() {
			w = append(w, "deleted_at < ?")
			args = append(args, fmtTime(f.DeletedBefore))
		}
	} else {
		w = append(w, "deleted_at IS NULL")
	}
	if f.ProjectID != "" {
		w = append(w, "project_id = ?")
		args = append(args, f.ProjectID)
	}
	if f.SessionID != "" {
		w = append(w, "session_id = ?")
		args = append(args, f.SessionID)
	}
	if len(f.Statuses) > 0 {
		w = append(w, inClause("status", f.Statuses, &args))
	}
	if len(f.Kinds) > 0 {
		w = append(w, inClause("kind", f.Kinds, &args))
	}
	if len(f.Confidences) > 0 {
		w = append(w, inClause("confidence", f.Confidences, &args))
	}
	if len(f.Impacts) > 0 {
		w = append(w, inClause("impact", f.Impacts, &args))
	}
	if !f.Since.IsZero() {
		w = append(w, "created_at >= ?")
		args = append(args, fmtTime(f.Since))
	}
	if !f.Before.IsZero() {
		w = append(w, "created_at < ?")
		args = append(args, fmtTime(f.Before))
	}
	if len(f.IDs) > 0 {
		w = append(w, inClause("id", f.IDs, &args))
	}
	return strings.Join(w, " AND "), args
}
