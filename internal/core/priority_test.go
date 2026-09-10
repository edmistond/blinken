package core

import (
	"testing"
	"time"
)

func TestPriorityScore(t *testing.T) {
	cases := []struct {
		c, i, r string
		want    int
		reason  string
	}{
		{"medium", "medium", "moderate", 8, "medium impact, moderate to reverse"},
		{"low", "critical", "hard", 45, "low confidence, critical impact, hard to reverse"},
		{"high", "low", "easy", 1, "low impact, easy to reverse"},
		{"high", "critical", "hard", 15, "critical impact, hard to reverse"},
	}
	for _, tc := range cases {
		g := &Guess{Confidence: tc.c, Impact: tc.i, Reversibility: tc.r}
		p := PriorityOf(g)
		if p.Score != tc.want || p.Reason != tc.reason {
			t.Errorf("%s/%s/%s: got %d %q, want %d %q", tc.c, tc.i, tc.r, p.Score, p.Reason, tc.want, tc.reason)
		}
	}
}

func TestSortByPriorityTieBreaks(t *testing.T) {
	t0 := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	gs := []Guess{
		{ID: "d", Confidence: "medium", Impact: "medium", Reversibility: "moderate", Status: "followup", CreatedAt: t0},
		{ID: "c", Confidence: "medium", Impact: "medium", Reversibility: "moderate", Status: "unreviewed", CreatedAt: t0.Add(time.Minute)},
		{ID: "b", Confidence: "medium", Impact: "medium", Reversibility: "moderate", Status: "unreviewed", CreatedAt: t0},
		{ID: "a", Confidence: "low", Impact: "critical", Reversibility: "hard", Status: "unreviewed", CreatedAt: t0.Add(time.Hour)},
		{ID: "e", Confidence: "high", Impact: "high", Reversibility: "hard", Status: "unreviewed", CreatedAt: t0}, // 9
		{ID: "f", Confidence: "low", Impact: "high", Reversibility: "easy", Status: "unreviewed", CreatedAt: t0},  // 9
	}
	SortByPriority(gs)
	got := ""
	for _, g := range gs {
		got += g.ID
	}
	// a=45; e,f=9 (equal impact, same time, id order); b,c,d=8 (unreviewed first, then oldest)
	if got != "aefbcd" {
		t.Fatalf("order = %s, want aefbcd", got)
	}
}
