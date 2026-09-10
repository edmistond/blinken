package server

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/edmistond/blinken/internal/core"
	"github.com/edmistond/blinken/internal/storage"
)

func setup(t *testing.T) (*Server, *storage.DB, string) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	pid, _ := db.ResolveProject(ctx, "/tmp/p", ".git")
	g := &core.Guess{ProjectID: pid, Summary: "<b>escaped?</b>", Impact: "critical"}
	if err := db.InsertGuess(ctx, g); err != nil {
		t.Fatal(err)
	}
	s, err := New(db, Project{ID: pid, Name: "p", Root: "/tmp/p"}, Options{IncludeFollowup: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	go s.Serve(ctx)
	return s, db, g.ID
}

func TestBindsLoopbackOnlyAndServesAssets(t *testing.T) {
	s, _, _ := setup(t)
	if !strings.HasPrefix(s.URL(), "http://127.0.0.1:") {
		t.Fatalf("bound to %s", s.URL())
	}
	for _, p := range []string{"/", "/static/app.js", "/static/style.css", "/static/logo.png", "/api/review", "/api/projects", "/api/sessions"} {
		res, err := http.Get(s.URL() + p)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("GET %s: %v %v", p, res, err)
		}
		res.Body.Close()
	}
	res, _ := http.Get(s.URL() + "/")
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), s.Token()) || strings.Contains(string(body), "{{TOKEN}}") {
		t.Fatal("token not injected into index")
	}
}

func TestRejectsForeignHostAndMissingToken(t *testing.T) {
	s, _, id := setup(t)
	req, _ := http.NewRequest("GET", s.URL()+"/api/review", nil)
	req.Host = "evil.example"
	res, _ := http.DefaultClient.Do(req)
	if res.StatusCode != 403 {
		t.Fatalf("foreign host got %d", res.StatusCode)
	}
	req, _ = http.NewRequest("POST", s.URL()+"/api/guesses/"+id+"/status", strings.NewReader(`{"status":"accepted"}`))
	res, _ = http.DefaultClient.Do(req)
	if res.StatusCode != 403 {
		t.Fatalf("missing token got %d", res.StatusCode)
	}
}

func TestStatusUpdateWithToken(t *testing.T) {
	s, db, id := setup(t)
	req, _ := http.NewRequest("POST", s.URL()+"/api/guesses/"+id+"/status", strings.NewReader(`{"status":"followup","note":"check"}`))
	req.Header.Set("X-Blinken-Token", s.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("status update: %v %v", res, err)
	}
	g, _ := db.GetGuess(context.Background(), id)
	if g.Status != "followup" || g.ReviewNote != "check" {
		t.Fatalf("db not updated: %+v", g)
	}
}

func TestBulkStatusAndFilters(t *testing.T) {
	s, db, id := setup(t)
	ctx := context.Background()
	pid, _ := db.ResolveProject(ctx, "/tmp/p", ".git")
	g2 := &core.Guess{ProjectID: pid, Summary: "second", Kind: "tradeoff"}
	db.InsertGuess(ctx, g2)

	res, _ := http.Get(s.URL() + "/api/review?kind=tradeoff")
	body, _ := io.ReadAll(res.Body)
	if strings.Contains(string(body), "escaped?") || !strings.Contains(string(body), "second") {
		t.Fatalf("kind filter not applied: %s", body)
	}

	req, _ := http.NewRequest("POST", s.URL()+"/api/guesses/bulk", strings.NewReader(`{"ids":["`+id+`","`+g2.ID+`"],"status":"accepted","note":"batch"}`))
	req.Header.Set("X-Blinken-Token", s.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("bulk: %v %v", res, err)
	}
	body, _ = io.ReadAll(res.Body)
	if !strings.Contains(string(body), `"count":2`) {
		t.Fatalf("bulk count: %s", body)
	}
	g, _ := db.GetGuess(ctx, g2.ID)
	if g.Status != "accepted" || g.ReviewNote != "batch" {
		t.Fatalf("bulk not applied: %+v", g)
	}
}
