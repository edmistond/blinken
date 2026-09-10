package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// env is one isolated database and config for a test.
type env struct {
	t     *testing.T
	db    string
	cfg   string
	dir   string // default --project
	stdin string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "proj")
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	e := &env{t: t, db: filepath.Join(tmp, "b.db"), cfg: filepath.Join(tmp, "config.toml"), dir: dir}
	return e
}

func (e *env) run(args ...string) (stdout, stderr string, code int) {
	e.t.Helper()
	full := append([]string{"--db", e.db, "--config", e.cfg}, args...)
	if !contains(args, "--project") {
		full = append(full, "--project", e.dir)
	}
	var out, errb bytes.Buffer
	code = run(full, strings.NewReader(e.stdin), &out, &errb)
	return out.String(), errb.String(), code
}

func contains(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

var ulidRe = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}\n$`)

func TestMinimalGuessPrintsOnlyAnID(t *testing.T) {
	e := newEnv(t)
	out, errs, code := e.run("guess", "Retry once on timeout")
	if code != 0 || errs != "" || !ulidRe.MatchString(out) {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	id := strings.TrimSpace(out)
	show, _, code := e.run("show", id, "--json")
	if code != 0 {
		t.Fatal("show failed")
	}
	var g map[string]any
	json.Unmarshal([]byte(show), &g)
	if g["summary"] != "Retry once on timeout" || g["confidence"] != "medium" || g["impact"] != "medium" || g["reversibility"] != "moderate" || g["status"] != "unreviewed" {
		t.Fatalf("defaults not applied: %v", g)
	}
	if g["cwd"] == "" {
		t.Fatal("cwd not captured")
	}
}

func TestQuietAndExitCodes(t *testing.T) {
	e := newEnv(t)
	out, errs, code := e.run("guess", "-q", "x")
	if out != "" || errs != "" || code != 0 {
		t.Fatalf("quiet: out=%q err=%q code=%d", out, errs, code)
	}
	out, errs, code = e.run("guess", "--impact", "huge", "x")
	if code != exitUsage || out != "" || !strings.HasPrefix(errs, "blinken:") {
		t.Fatalf("usage: out=%q err=%q code=%d", out, errs, code)
	}
	_, errs, code = e.run("guess")
	if code != exitUsage || !strings.Contains(errs, "summary is required") {
		t.Fatalf("empty summary: err=%q code=%d", errs, code)
	}
	_, errs, code = e.run("show", "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if code != exitNotFound || !strings.Contains(errs, "not found") {
		t.Fatalf("not found: err=%q code=%d", errs, code)
	}
	_, _, code = e.run("purge")
	if code != exitUsage {
		t.Fatalf("purge without --hard: code=%d", code)
	}
	_, _, code = e.run("accept")
	if code != exitUsage {
		t.Fatalf("accept without id or --all: code=%d", code)
	}
	out, _, code = e.run("guess", "--help")
	if code != 0 || !strings.Contains(out, "--confidence=\"medium\"") {
		t.Fatalf("help must show defaults: code=%d out=%q", code, out)
	}
	for _, s := range []string{out, errs} {
		if strings.Contains(s, "\x1b[") {
			t.Fatal("output contains ANSI escapes")
		}
	}
}

func TestProjectDetection(t *testing.T) {
	tmp := t.TempDir()
	db := filepath.Join(tmp, "b.db")
	mk := func(name string, marker string, asFile bool) string {
		dir := filepath.Join(tmp, name)
		os.MkdirAll(filepath.Join(dir, "deep", "er"), 0o755)
		if marker != "" {
			if asFile {
				os.WriteFile(filepath.Join(dir, marker), []byte("gitdir: /elsewhere"), 0o644)
			} else {
				os.MkdirAll(filepath.Join(dir, marker), 0o755)
			}
		}
		return dir
	}
	git := mk("git", ".git", false)
	worktree := mk("wt", ".git", true)
	jj := mk("jj", ".jj", false)
	plain := mk("plain", "", false)

	count := func(project string) int {
		var out bytes.Buffer
		run([]string{"--db", db, "--project", project, "guesses", "--compact"}, strings.NewReader(""), &out, &bytes.Buffer{})
		return len(strings.Split(strings.TrimSpace(out.String()), "\n")) - boolInt(out.Len() == 0)
	}
	for _, root := range []string{git, worktree, jj} {
		// recording from a nested directory must resolve to the repo root
		var out, errs bytes.Buffer
		if code := run([]string{"--db", db, "--project", filepath.Join(root, "deep", "er"), "guess", "nested"}, strings.NewReader(""), &out, &errs); code != 0 {
			t.Fatalf("%s: %s", root, errs.String())
		}
		if count(root) != 1 {
			t.Fatalf("%s: nested guess not attributed to root", root)
		}
	}
	// no VCS: each directory is its own project
	run([]string{"--db", db, "--project", filepath.Join(plain, "deep"), "guess", "plain"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if count(plain) != 0 || count(filepath.Join(plain, "deep")) != 1 {
		t.Fatal("non-repository directories should not share a project")
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestSessionPrecedence(t *testing.T) {
	e := newEnv(t)
	out, _, _ := e.run("session", "start", "--agent", "t")
	envSID := strings.TrimSpace(out)
	out, _, _ = e.run("session", "start", "--agent", "t")
	flagSID := strings.TrimSpace(out)
	t.Setenv("BLINKEN_SESSION", envSID)

	out, _, _ = e.run("guess", "via env")
	show, _, _ := e.run("show", strings.TrimSpace(out), "--json")
	if !strings.Contains(show, envSID) {
		t.Fatal("BLINKEN_SESSION not applied")
	}
	out, _, _ = e.run("guess", "via flag", "--session", flagSID)
	show, _, _ = e.run("show", strings.TrimSpace(out), "--json")
	if !strings.Contains(show, flagSID) || strings.Contains(show, envSID) {
		t.Fatal("--session did not override BLINKEN_SESSION")
	}
	if _, _, code := e.run("session", "end"); code != 0 {
		t.Fatal("session end should default to BLINKEN_SESSION")
	}
	out, _, _ = e.run("sessions")
	if !strings.Contains(out, envSID+" ended") || !strings.Contains(out, flagSID+" open") {
		t.Fatalf("sessions listing: %q", out)
	}
}

func TestRedactionHappensBeforePersistence(t *testing.T) {
	e := newEnv(t)
	os.WriteFile(e.cfg, []byte("[privacy]\nredact = [\"hunter2\"]\nignore_paths = [\".env\"]\n"), 0o600)
	out, _, code := e.run("guess", "the password is hunter2, key AKIAABCDEFGHIJKLMNOP", "--file", ".env", "--file", "a.go", "--reason", "token=xyz")
	if code != 0 {
		t.Fatal("guess failed")
	}
	show, _, _ := e.run("show", strings.TrimSpace(out), "--json")
	for _, secret := range []string{"hunter2", "AKIAABCDEFGHIJKLMNOP", "token=xyz", ".env"} {
		if strings.Contains(show, secret) {
			t.Fatalf("%q survived redaction: %s", secret, show)
		}
	}
	if !strings.Contains(show, "a.go") {
		t.Fatal("non-ignored file dropped")
	}
	raw, _ := os.ReadFile(e.db)
	if bytes.Contains(raw, []byte("hunter2")) {
		t.Fatal("secret present in database file")
	}
	_, _, code = e.run("accept", "--all", "--yes", "--note", "ok hunter2")
	show, _, _ = e.run("show", strings.TrimSpace(out), "--json")
	if strings.Contains(show, "hunter2") {
		t.Fatal("review note not redacted")
	}
}

func TestCompactOutputIsStable(t *testing.T) {
	e := newEnv(t)
	e.run("guess", "--kind", "tradeoff", "--impact", "critical", "--confidence", "low", "a b c")
	e.run("guess", "plain")
	out, _, _ := e.run("guesses", "--compact")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	re := regexp.MustCompile(`^[0-9A-Z]{26} [LMHC]/[LMH] \S+\s+.+$`)
	for _, l := range lines {
		if !re.MatchString(l) {
			t.Fatalf("row does not match contract: %q", l)
		}
	}
	if !strings.HasPrefix(strings.Fields(lines[0])[1], "C/L") || !strings.Contains(lines[0], "tradeoff") {
		t.Fatalf("highest priority first with I/C abbrev: %q", lines[0])
	}
	out, _, _ = e.run("guesses")
	if out != "2 unreviewed | 0 followup | 1 high-impact | 1 low-confidence\n" {
		t.Fatalf("summary line: %q", out)
	}
}

func TestBulkRequiresConfirmationAndDeletionScoping(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 3; i++ {
		e.run("guess", "--impact", "low", "x")
	}
	e.run("guess", "--impact", "high", "keep")
	_, errs, code := e.run("accept", "--all", "--impact", "low")
	if code != exitUsage || !strings.Contains(errs, "3 guesses would be affected") {
		t.Fatalf("non-tty bulk must refuse: code=%d err=%q", code, errs)
	}
	out, _, code := e.run("accept", "--all", "--impact", "low", "--yes")
	if code != 0 || out != "3\n" {
		t.Fatalf("bulk accept: code=%d out=%q", code, out)
	}
	out, _, _ = e.run("guesses")
	if !strings.HasPrefix(out, "1 unreviewed") {
		t.Fatalf("after bulk: %q", out)
	}
	out, _, code = e.run("delete", "--all", "--status", "accepted", "--yes")
	if code != 0 || out != "3\n" {
		t.Fatalf("bulk delete: %q", out)
	}
	out, _, _ = e.run("guesses", "--all", "--compact")
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("deleted rows still listed: %q", out)
	}
	out, _, code = e.run("purge", "--hard", "--yes")
	if code != 0 || out != "3\n" {
		t.Fatalf("purge: code=%d out=%q", code, out)
	}
	out, _, _ = e.run("purge", "--hard", "--yes")
	if out != "" {
		t.Fatalf("second purge should find nothing: %q", out)
	}
}
