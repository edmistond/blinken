package core

import (
	"sort"
	"strings"
)

// Weights are the v0.1 heuristic from the spec, section 12.
var (
	uncertaintyWeight   = map[string]int{ConfidenceLow: 3, ConfidenceMedium: 2, ConfidenceHigh: 1}
	impactWeight        = map[string]int{ImpactLow: 1, ImpactMedium: 2, ImpactHigh: 3, ImpactCritical: 5}
	reversibilityWeight = map[string]int{ReversibilityEasy: 1, ReversibilityModerate: 2, ReversibilityHard: 3}
)

// Priority is the product score plus the plain-language reason shown to humans.
type Priority struct {
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

// PriorityOf computes review priority = uncertainty × impact × difficulty of reversal.
func PriorityOf(g *Guess) Priority {
	u, i, r := uncertaintyWeight[g.Confidence], impactWeight[g.Impact], reversibilityWeight[g.Reversibility]
	var parts []string
	if g.Confidence == ConfidenceLow {
		parts = append(parts, "low confidence")
	}
	parts = append(parts, g.Impact+" impact")
	switch g.Reversibility {
	case ReversibilityHard:
		parts = append(parts, "hard to reverse")
	case ReversibilityEasy:
		parts = append(parts, "easy to reverse")
	default:
		parts = append(parts, "moderate to reverse")
	}
	return Priority{Score: u * i * r, Reason: strings.Join(parts, ", ")}
}

// SortByPriority orders guesses for review: highest score first, then
// unreviewed before followup, then higher impact, then oldest first.
// Ties are fully deterministic.
func SortByPriority(gs []Guess) {
	sort.SliceStable(gs, func(a, b int) bool {
		pa, pb := PriorityOf(&gs[a]).Score, PriorityOf(&gs[b]).Score
		if pa != pb {
			return pa > pb
		}
		if gs[a].Status != gs[b].Status {
			return gs[a].Status == StatusUnreviewed
		}
		ia, ib := impactWeight[gs[a].Impact], impactWeight[gs[b].Impact]
		if ia != ib {
			return ia > ib
		}
		if !gs[a].CreatedAt.Equal(gs[b].CreatedAt) {
			return gs[a].CreatedAt.Before(gs[b].CreatedAt)
		}
		return gs[a].ID < gs[b].ID
	})
}
