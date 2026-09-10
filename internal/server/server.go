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

// Server serves the embedded UI for one project.
type Server struct {
	db      *storage.DB
	project Project
	token   string
	index   []byte
	http    *http.Server
	ln      net.Listener
}

// New prepares a server bound to a random loopback port (or the given one).
func New(db *storage.DB, project Project, port int) (*Server, error) {
	raw, err := web.Index()
	if err != nil {
		return nil, err
	}
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		return nil, err
	}
	s := &Server{db: db, project: project, token: hex.EncodeToString(tok)}
	s.index = bytes.ReplaceAll(raw, []byte("{{TOKEN}}"), []byte(s.token))

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}
	s.ln = ln

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(mustSub(web.FS(), "static"))))
	mux.HandleFunc("GET /api/review", s.handleReview)
	mux.HandleFunc("POST /api/guesses/{id}/status", s.handleStatus)

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
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(s.index)
}

type reviewGuess struct {
	core.Guess
	Priority core.Priority `json:"priority"`
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	gs, err := s.db.ListGuesses(ctx, storage.Filter{ProjectID: s.project.ID, Statuses: []string{core.StatusUnreviewed, core.StatusFollowup}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	core.SortByPriority(gs)
	out := make([]reviewGuess, len(gs))
	for i := range gs {
		out[i] = reviewGuess{Guess: gs[i], Priority: core.PriorityOf(&gs[i])}
	}
	counts, err := s.db.CountGuesses(ctx, s.project.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"project": s.project, "counts": counts, "guesses": out})
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
	if err := s.db.SetStatus(r.Context(), id, body.Status, body.Note); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, storage.ErrNotFound) {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	writeJSON(w, map[string]string{"id": id, "status": body.Status})
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
