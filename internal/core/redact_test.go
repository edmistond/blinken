package core

import "testing"

func TestRedactorTextAndFiles(t *testing.T) {
	r, err := NewRedactor([]string{"hunter2"}, []string{`ticket-\d+`}, []string{".env", "secrets/**", "*.pem"}, true)
	if err != nil {
		t.Fatal(err)
	}
	g := &Guess{
		Summary:  "use hunter2 for ticket-42 with key AKIAABCDEFGHIJKLMNOP",
		Reason:   "token=abc123 was in the env",
		WouldAsk: "is ghp_0123456789012345678901234567890123456789 rotated?",
		Files:    []FileRef{{Path: "src/a.go"}, {Path: "config/.env"}, {Path: "app/secrets/db.yaml"}, {Path: "certs/x.pem"}, {Path: "envelope.go"}},
	}
	r.Guess(g)
	want := "use [redacted] for [redacted] with key [redacted]"
	if g.Summary != want {
		t.Errorf("summary = %q, want %q", g.Summary, want)
	}
	if g.Reason != "[redacted] was in the env" {
		t.Errorf("reason = %q", g.Reason)
	}
	if g.WouldAsk != "is [redacted] rotated?" {
		t.Errorf("would_ask = %q", g.WouldAsk)
	}
	if len(g.Files) != 2 || g.Files[0].Path != "src/a.go" || g.Files[1].Path != "envelope.go" {
		t.Errorf("files = %+v", g.Files)
	}
}

func TestRedactorRejectsBadPattern(t *testing.T) {
	if _, err := NewRedactor(nil, []string{"("}, nil, false); err == nil {
		t.Fatal("expected error")
	}
}

func TestNilRedactorIsNoop(t *testing.T) {
	var r *Redactor
	g := &Guess{Summary: "AKIAABCDEFGHIJKLMNOP", Files: []FileRef{{Path: ".env"}}}
	r.Guess(g)
	if g.Summary != "AKIAABCDEFGHIJKLMNOP" || len(g.Files) != 1 {
		t.Fatal("nil redactor changed the guess")
	}
}
