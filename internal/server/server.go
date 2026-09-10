// Package server hosts the loopback-only review UI and its JSON API.
package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/edmistond/blinken/internal/core"
	"github.com/edmistond/blinken/internal/storage"
	"github.com/edmistond/blinken/web"
)

// Project is what the UI shows in its header.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Root string `json:"root"`
}

// Options tune the server.
type Options struct {
	Port            int
	IncludeFollowup bool
	Redactor        *core.Redactor // applied to review notes before persistence
}

// Server serves the embedded UI for one database.
type Server struct {
	db      *storage.DB
	project Project // the project the command was launched in
	opts    Options
	token   string
	index   []byte
	http    *http.Server
	ln      net.Listener
}

// New prepares a server bound to a random loopback port (or opts.Port).
func New(db *storage.DB, project Project, opts Options) (*Server, error) {
	raw, err := web.Index()
	if err != nil {
		return nil, err
	}
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		return nil, err
	}
	s := &Server{db: db, project: project, opts: opts, token: hex.EncodeToString(tok)}
	s.index = bytes.ReplaceAll(raw, []byte("{{TOKEN}}"), []byte(s.token))

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", opts.Port))
	if err != nil {
		return nil, err
	}
	s.ln = ln

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(mustSub(web.FS(), "static"))))
	mux.HandleFunc("GET /api/projects", s.handleProjects)
	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	mux.HandleFunc("GET /api/review", s.handleReview)
	mux.HandleFunc("POST /api/guesses/{id}/status", s.handleStatus)
	mux.HandleFunc("POST /api/guesses/bulk", s.handleBulk)

	s.http = &http.Server{
		Handler:           s.guard(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return s, nil
}

func mustSub(f fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// URL is the exact bound address, printed to the terminal.
func (s *Server) URL() string { return "http://" + s.ln.Addr().String() }

// Token is the per-run CSRF token; exported for tests.
func (s *Server) Token() string { return s.token }

// Serve blocks until ctx is cancelled, then shuts down gracefully.
func (s *Server) Serve(ctx context.Context) error {
	errc := make(chan error, 1)
	go func() { errc <- s.http.Serve(s.ln) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// guard rejects non-loopback Host headers (DNS rebinding) and requires the
// per-run token on every state-changing request (CSRF).
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			got := r.Header.Get("X-Blinken-Token")
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
				http.Error(w, "missing or invalid token", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(s.index)
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.db.ListProjects(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"current": s.project.ID, "projects": ps})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	pid := r.URL.Query().Get("project")
	if pid == "" {
		pid = s.project.ID
	}
	ss, err := s.db.ListSessions(r.Context(), pid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ss == nil {
		ss = []storage.Session{}
	}
	writeJSON(w, ss)
}

type reviewGuess struct {
	core.Guess
	Priority core.Priority `json:"priority"`
}

func csv(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f := storage.Filter{
		ProjectID:   q.Get("project"),
		SessionID:   q.Get("session"),
		Statuses:    csv(q.Get("status")),
		Kinds:       csv(q.Get("kind")),
		Confidences: csv(q.Get("confidence")),
		Impacts:     csv(q.Get("impact")),
	}
	if f.ProjectID == "" {
		f.ProjectID = s.project.ID
	}
	if len(f.Statuses) == 0 {
		f.Statuses = []string{core.StatusUnreviewed}
		if s.opts.IncludeFollowup {
			f.Statuses = append(f.Statuses, core.StatusFollowup)
		}
	}
	gs, err := s.db.ListGuesses(ctx, f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	core.SortByPriority(gs)
	out := make([]reviewGuess, len(gs))
	for i := range gs {
		out[i] = reviewGuess{Guess: gs[i], Priority: core.PriorityOf(&gs[i])}
	}
	counts, err := s.db.CountGuesses(ctx, f.ProjectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	proj := s.project
	if f.ProjectID != s.project.ID {
		proj = Project{ID: f.ProjectID}
		if ps, err := s.db.ListProjects(ctx); err == nil {
			for _, p := range ps {
				if p.ID == f.ProjectID {
					proj = Project{ID: p.ID, Name: p.Name, Root: p.Root}
				}
			}
		}
	}
	writeJSON(w, map[string]any{"project": proj, "counts": counts, "guesses": out, "statuses": f.Statuses})
}

func (s *Server) note(n string) string {
	if s.opts.Redactor == nil {
		return n
	}
	return s.opts.Redactor.Text(n)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	if err := s.db.SetStatus(r.Context(), id, body.Status, s.note(body.Note)); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, storage.ErrNotFound) {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	writeJSON(w, map[string]string{"id": id, "status": body.Status})
}

// handleBulk applies one status to an explicit list of ids. The page shows
// the count and asks the user before calling this.
func (s *Server) handleBulk(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs    []string `json:"ids"`
		Status string   `json:"status"`
		Note   string   `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if len(body.IDs) == 0 {
		http.Error(w, "no ids", http.StatusBadRequest)
		return
	}
	n, err := s.db.BulkSetStatus(r.Context(), storage.Filter{IDs: body.IDs}, body.Status, s.note(body.Note))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"count": n, "status": body.Status})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// OpenBrowser launches the platform's default opener. Failure is not fatal.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
