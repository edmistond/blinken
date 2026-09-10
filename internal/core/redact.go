package core

import (
	"fmt"
	"regexp"
	"strings"
)

// Placeholder replaces redacted spans.
const Placeholder = "[redacted]"

// BuiltinSecretPatterns catch common credential shapes. This list is a
// convenience, not a guarantee; Blinken does not claim complete detection.
var BuiltinSecretPatterns = []string{
	`AKIA[0-9A-Z]{16}`,                         // AWS access key id
	`(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}`, // GitHub tokens
	`github_pat_[A-Za-z0-9_]{22,}`,             // GitHub fine-grained PAT
	`xox[baprs]-[A-Za-z0-9-]{10,}`,             // Slack tokens
	`sk-[A-Za-z0-9_-]{20,}`,                    // OpenAI-style secret keys
	`sk-ant-[A-Za-z0-9_-]{20,}`,                // Anthropic keys
	`AIza[0-9A-Za-z_-]{35}`,                    // Google API keys
	`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`,
	`(?i)\b(?:password|passwd|secret|token|api[_-]?key)\s*[=:]\s*\S+`, // key=value leaks
}

// Redactor rewrites free text and filters file references before persistence.
type Redactor struct {
	literals []string
	patterns []*regexp.Regexp
	ignore   []*regexp.Regexp
}

// NewRedactor compiles the rules. Invalid patterns are reported, not ignored.
func NewRedactor(literals, patterns, ignorePaths []string, builtin bool) (*Redactor, error) {
	r := &Redactor{}
	for _, l := range literals {
		if l != "" {
			r.literals = append(r.literals, l)
		}
	}
	if builtin {
		patterns = append(append([]string{}, BuiltinSecretPatterns...), patterns...)
	}
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("redact pattern %q: %w", p, err)
		}
		r.patterns = append(r.patterns, re)
	}
	for _, g := range ignorePaths {
		re, err := globToRegexp(g)
		if err != nil {
			return nil, fmt.Errorf("ignore path %q: %w", g, err)
		}
		r.ignore = append(r.ignore, re)
	}
	return r, nil
}

// Text applies literal and pattern redaction.
func (r *Redactor) Text(s string) string {
	if r == nil || s == "" {
		return s
	}
	for _, l := range r.literals {
		s = strings.ReplaceAll(s, l, Placeholder)
	}
	for _, re := range r.patterns {
		s = re.ReplaceAllString(s, Placeholder)
	}
	return s
}

// IgnoresPath reports whether a file reference must be dropped.
func (r *Redactor) IgnoresPath(p string) bool {
	if r == nil {
		return false
	}
	p = strings.ReplaceAll(p, "\\", "/")
	for _, re := range r.ignore {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}

// Guess redacts every free-text field in place and drops ignored files.
func (r *Redactor) Guess(g *Guess) {
	if r == nil {
		return
	}
	g.Summary = r.Text(g.Summary)
	g.Ambiguity = r.Text(g.Ambiguity)
	g.Chosen = r.Text(g.Chosen)
	g.Reason = r.Text(g.Reason)
	g.Alternative = r.Text(g.Alternative)
	g.WouldAsk = r.Text(g.WouldAsk)
	g.ReviewNote = r.Text(g.ReviewNote)
	kept := g.Files[:0]
	for _, f := range g.Files {
		if !r.IgnoresPath(f.Path) {
			kept = append(kept, f)
		}
	}
	g.Files = kept
}

// globToRegexp supports *, ?, and ** (any depth). A rule matches when it
// matches the whole path or any suffix starting at a path-segment boundary,
// so ".env" matches "config/.env" and "secrets/**" matches "app/secrets/x".
func globToRegexp(glob string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString(`(?:^|/)`)
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case c == '*' && i+1 < len(glob) && glob[i+1] == '*':
			b.WriteString(`.*`)
			i++
			if i+1 < len(glob) && glob[i+1] == '/' {
				i++
			}
		case c == '*':
			b.WriteString(`[^/]*`)
		case c == '?':
			b.WriteString(`[^/]`)
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString(`(?:$|/)`)
	return regexp.Compile(b.String())
}
