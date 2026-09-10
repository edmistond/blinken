package core

import (
	"fmt"
	"strings"
	"time"
)

// Enumerations. Values are stored as lowercase strings in SQLite.
const (
	ConfidenceLow    = "low"
	ConfidenceMedium = "medium"
	ConfidenceHigh   = "high"

	ImpactLow      = "low"
	ImpactMedium   = "medium"
	ImpactHigh     = "high"
	ImpactCritical = "critical"

	ReversibilityEasy     = "easy"
	ReversibilityModerate = "moderate"
	ReversibilityHard     = "hard"

	StatusUnreviewed = "unreviewed"
	StatusAccepted   = "accepted"
	StatusRejected   = "rejected"
	StatusFollowup   = "followup"
)

var (
	Kinds           = []string{"assumption", "interpretation", "tradeoff", "deviation", "inference"}
	Confidences     = []string{ConfidenceLow, ConfidenceMedium, ConfidenceHigh}
	Impacts         = []string{ImpactLow, ImpactMedium, ImpactHigh, ImpactCritical}
	Reversibilities = []string{ReversibilityEasy, ReversibilityModerate, ReversibilityHard}
	Statuses        = []string{StatusUnreviewed, StatusAccepted, StatusRejected, StatusFollowup}
)

// FileRef is an explicit file reference, optionally with a line number.
type FileRef struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

// Guess is the logical record of one consequential choice made under ambiguity.
type Guess struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id"`
	SessionID     string     `json:"session_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Summary       string     `json:"summary"`
	Ambiguity     string     `json:"ambiguity,omitempty"`
	Chosen        string     `json:"chosen_behavior,omitempty"`
	Kind          string     `json:"kind,omitempty"`
	Confidence    string     `json:"confidence"`
	Impact        string     `json:"impact"`
	Reversibility string     `json:"reversibility"`
	Reason        string     `json:"reason,omitempty"`
	Alternative   string     `json:"alternative,omitempty"`
	WouldAsk      string     `json:"would_ask,omitempty"`
	Cwd           string     `json:"cwd,omitempty"`
	CwdRel        string     `json:"cwd_rel,omitempty"`
	Files         []FileRef  `json:"files,omitempty"`
	Status        string     `json:"status"`
	ReviewNote    string     `json:"review_note,omitempty"`
	ReviewedAt    *time.Time `json:"reviewed_at,omitempty"`
}

func oneOf(field, v string, allowed []string) error {
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %s, got %q", field, strings.Join(allowed, ", "), v)
}

// Validate applies defaults and checks enumerations.
func (g *Guess) Validate() error {
	g.Summary = strings.TrimSpace(g.Summary)
	if g.Summary == "" {
		return fmt.Errorf("summary is required")
	}
	if g.Confidence == "" {
		g.Confidence = ConfidenceMedium
	}
	if g.Impact == "" {
		g.Impact = ImpactMedium
	}
	if g.Reversibility == "" {
		g.Reversibility = ReversibilityModerate
	}
	if g.Status == "" {
		g.Status = StatusUnreviewed
	}
	if g.Kind != "" {
		if err := oneOf("kind", g.Kind, Kinds); err != nil {
			return err
		}
	}
	if err := oneOf("confidence", g.Confidence, Confidences); err != nil {
		return err
	}
	if err := oneOf("impact", g.Impact, Impacts); err != nil {
		return err
	}
	if err := oneOf("reversibility", g.Reversibility, Reversibilities); err != nil {
		return err
	}
	return oneOf("status", g.Status, Statuses)
}

// ParseFileRef parses "path" or "path:line".
func ParseFileRef(s string) (FileRef, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return FileRef{}, fmt.Errorf("empty file reference")
	}
	if i := strings.LastIndex(s, ":"); i > 0 && i < len(s)-1 {
		var line int
		if _, err := fmt.Sscanf(s[i+1:], "%d", &line); err == nil && line > 0 {
			return FileRef{Path: s[:i], Line: line}, nil
		}
	}
	return FileRef{Path: s}, nil
}

// Abbrev returns the compact "I/C" label used by `guesses --compact`:
// impact letter, slash, confidence letter. Documented in docs/output-format.md.
func (g *Guess) Abbrev() string {
	return strings.ToUpper(g.Impact[:1]) + "/" + strings.ToUpper(g.Confidence[:1])
}
