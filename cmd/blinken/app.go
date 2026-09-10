package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/mattn/go-isatty"

	"github.com/edmistond/blinken/internal/config"
	"github.com/edmistond/blinken/internal/core"
	"github.com/edmistond/blinken/internal/storage"
)

// Exit codes are part of the automation contract (docs/output-format.md).
const (
	exitOK       = 0
	exitUnexpect = 1
	exitUsage    = 2
	exitNotFound = 3
	exitStorage  = 4
)

var version = "dev"

// Globals are flags every command accepts.
type Globals struct {
	DB      string `help:"Database path (default: OS data dir)." env:"BLINKEN_DB" placeholder:"PATH"`
	Config  string `help:"Config file (default: OS config dir)." env:"BLINKEN_CONFIG" placeholder:"PATH"`
	Project string `help:"Project root (default: detected from cwd)." type:"path" placeholder:"DIR"`
	Quiet   bool   `short:"q" help:"Print nothing on success."`
}

// FilterFlags are shared by list, review, bulk, and delete commands.
type FilterFlags struct {
	Status     []string `help:"Filter by status." enum:"unreviewed,accepted,rejected,followup" placeholder:"S,..."`
	Kind       []string `help:"Filter by kind." enum:"assumption,interpretation,tradeoff,deviation,inference" placeholder:"K,..."`
	Confidence []string `help:"Filter by confidence." enum:"low,medium,high" placeholder:"C,..."`
	Impact     []string `help:"Filter by impact." enum:"low,medium,high,critical" placeholder:"I,..."`
	Session    string   `help:"Filter by session id." placeholder:"ID"`
	Since      string   `help:"Created on or after: YYYY-MM-DD, RFC3339, or a duration like 24h or 7d." placeholder:"WHEN"`
	Before     string   `help:"Created before: YYYY-MM-DD, RFC3339, or a duration like 24h or 7d." placeholder:"WHEN"`
}

func (f *FilterFlags) filter(projectID string) (storage.Filter, error) {
	out := storage.Filter{ProjectID: projectID, SessionID: f.Session, Statuses: f.Status, Kinds: f.Kind, Confidences: f.Confidence, Impacts: f.Impact}
	var err error
	if out.Since, err = parseWhen(f.Since); err != nil {
		return out, usageErr("--since: " + err.Error())
	}
	if out.Before, err = parseWhen(f.Before); err != nil {
		return out, usageErr("--before: " + err.Error())
	}
	return out, nil
}

// parseWhen accepts a date, an RFC3339 timestamp, or a duration ago (7d, 24h).
func parseWhen(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	if strings.HasSuffix(s, "d") {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("cannot parse %q", s)
}

// CLI is the Kong command tree.
type CLI struct {
	Globals

	Guess    GuessCmd    `cmd:"" help:"Record a guess. Prints the new guess id."`
	Guesses  GuessesCmd  `cmd:"" help:"Summarize or list guesses for the current project."`
	Show     ShowCmd     `cmd:"" help:"Show one guess."`
	Review   ReviewCmd   `cmd:"" help:"Review unresolved guesses, in the terminal or a browser."`
	Accept   StatusCmd   `cmd:"" help:"Mark a guess accepted."`
	Reject   StatusCmd   `cmd:"" help:"Mark a guess rejected."`
	Followup StatusCmd   `cmd:"" help:"Mark a guess for follow-up."`
	Delete   DeleteCmd   `cmd:"" help:"Soft-delete guesses. Hidden from every listing; recoverable until purged."`
	Purge    PurgeCmd    `cmd:"" help:"Permanently remove soft-deleted guesses."`
	Session  SessionCmd  `cmd:"" help:"Start or end a logical agent session."`
	Sessions SessionsCmd `cmd:"" help:"List sessions for the current project."`
	Version  VersionCmd  `cmd:"" help:"Print the version."`
}

// app carries per-invocation state into command handlers.
type app struct {
	g        *Globals
	cfg      config.Config
	redactor *core.Redactor
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
	ctx      context.Context
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var cli CLI
	exited := -1 // set by Kong after printing help or a version-style exit
	parser, err := kong.New(&cli,
		kong.Name("blinken"),
		kong.Description("Record the consequential guesses coding agents make, and review them.\n\n"+
			"Ranking flags default to medium confidence, medium impact, moderate reversibility when omitted; the defaults are labels, not measurements."),
		kong.UsageOnError(),
		kong.Writers(stdout, stderr),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true, NoExpandSubcommands: true}),
		kong.Exit(func(code int) { exited = code }),
	)
	if err != nil {
		panic(err)
	}
	kctx, err := parser.Parse(args)
	if exited >= 0 {
		return exited
	}
	if err != nil {
		fmt.Fprintln(stderr, "blinken:", err)
		return exitUsage
	}
	a := &app{g: &cli.Globals, stdin: stdin, stdout: stdout, stderr: stderr, ctx: context.Background()}
	if err := a.loadConfig(); err != nil {
		fmt.Fprintln(stderr, "blinken:", err)
		return exitUsage
	}
	if err := kctx.Run(a); err != nil {
		var ee exitError
		code := exitUnexpect
		if errors.As(err, &ee) {
			code = ee.code
		}
		fmt.Fprintln(stderr, "blinken:", err)
		return code
	}
	return exitOK
}

func (a *app) loadConfig() error {
	path := a.g.Config
	if path == "" {
		var err error
		if path, err = config.DefaultPath(); err != nil {
			return err
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	a.cfg = cfg
	a.redactor, err = core.NewRedactor(cfg.Privacy.Redact, cfg.Privacy.RedactPatterns, cfg.Privacy.IgnorePaths, cfg.Privacy.BuiltinSecretHeuristics)
	return err
}

// ---- version ----

type VersionCmd struct{}

func (VersionCmd) Run(a *app) error { fmt.Fprintln(a.stdout, "blinken "+version); return nil }

// ---- guess ----

type GuessCmd struct {
	Text          string   `arg:"" optional:"" help:"Summary of the guess (same as --summary)."`
	Summary       string   `help:"Summary of the guess; use for multiline or scripted input." placeholder:"TEXT"`
	Kind          string   `help:"One of: assumption, interpretation, tradeoff, deviation, inference." enum:",assumption,interpretation,tradeoff,deviation,inference" default:""`
	Confidence    string   `help:"How sure the agent was." enum:"low,medium,high" default:"medium"`
	Impact        string   `help:"How much the choice matters." enum:"low,medium,high,critical" default:"medium"`
	Reversibility string   `help:"How hard the choice is to undo." enum:"easy,moderate,hard" default:"moderate"`
	Ambiguity     string   `help:"What the available information failed to determine."`
	Chosen        string   `help:"More detail on the chosen behavior."`
	Reason        string   `help:"Evidence or rationale for the choice."`
	Alternative   string   `help:"A plausible competing choice."`
	WouldAsk      string   `name:"would-ask" help:"What the agent would have asked if asking were free."`
	File          []string `help:"File reference, path or path:line. Repeatable." placeholder:"PATH[:LINE]"`
	Session       string   `help:"Session id (overrides $BLINKEN_SESSION)." env:"BLINKEN_SESSION" placeholder:"ID"`
	NoCwd         bool     `help:"Do not record the working directory."`
	NoFiles       bool     `help:"Ignore --file references."`
}

func (c *GuessCmd) Run(a *app) error {
	summary := c.Text
	if c.Summary != "" {
		summary = c.Summary
	}
	if strings.TrimSpace(summary) == "" {
		return usageErr("a summary is required: blinken guess \"what you decided\"")
	}
	db, proj, err := a.openProject()
	if err != nil {
		return err
	}
	defer db.Close()

	g := core.Guess{
		ProjectID: proj.ID, SessionID: c.Session, Summary: summary, Ambiguity: c.Ambiguity, Chosen: c.Chosen,
		Kind: c.Kind, Confidence: c.Confidence, Impact: c.Impact, Reversibility: c.Reversibility,
		Reason: c.Reason, Alternative: c.Alternative, WouldAsk: c.WouldAsk,
	}
	if a.cfg.Recording.CaptureCwd && !c.NoCwd {
		if cwd, err := os.Getwd(); err == nil {
			g.Cwd = cwd
			g.CwdRel = core.RelPath(proj.Root, cwd)
		}
	}
	if a.cfg.Recording.CaptureFiles && !c.NoFiles {
		for _, f := range c.File {
			ref, err := core.ParseFileRef(f)
			if err != nil {
				return usageErr(err.Error())
			}
			g.Files = append(g.Files, ref)
		}
	}
	a.redactor.Guess(&g) // redaction happens before persistence, never only at display
	if err := db.InsertGuess(a.ctx, &g); err != nil {
		return storageErr(err)
	}
	a.printID(g.ID)
	return nil
}

// ---- guesses ----

type GuessesCmd struct {
	FilterFlags `embed:""`
	Compact     bool `help:"One row per guess: ID IMPACT/CONFIDENCE KIND SUMMARY."`
	JSON        bool `help:"JSON array on stdout."`
	All         bool `help:"Include every status (default: unreviewed and followup)."`
}

func (c *GuessesCmd) Run(a *app) error {
	db, proj, err := a.openProject()
	if err != nil {
		return err
	}
	defer db.Close()
	f, err := c.filter(proj.ID)
	if err != nil {
		return err
	}
	if len(f.Statuses) == 0 && !c.All {
		f.Statuses = []string{core.StatusUnreviewed, core.StatusFollowup}
	}
	gs, err := db.ListGuesses(a.ctx, f)
	if err != nil {
		return storageErr(err)
	}
	core.SortByPriority(gs)
	switch {
	case c.JSON:
		return a.writeJSON(withPriority(gs))
	case c.Compact:
		for _, x := range gs {
			kind := x.Kind
			if kind == "" {
				kind = "-"
			}
			fmt.Fprintf(a.stdout, "%s %s %-14s %s\n", x.ID, x.Abbrev(), kind, x.Summary)
		}
	default:
		counts, err := db.CountGuesses(a.ctx, proj.ID)
		if err != nil {
			return storageErr(err)
		}
		fmt.Fprintf(a.stdout, "%d unreviewed | %d followup | %d high-impact | %d low-confidence\n", counts.Unreviewed, counts.Followup, counts.HighImpact, counts.LowConfidence)
	}
	return nil
}

type guessJSON struct {
	core.Guess
	Priority core.Priority `json:"priority"`
}

func withPriority(gs []core.Guess) []guessJSON {
	out := make([]guessJSON, len(gs))
	for i := range gs {
		out[i] = guessJSON{gs[i], core.PriorityOf(&gs[i])}
	}
	return out
}

// ---- show ----

type ShowCmd struct {
	ID   string `arg:"" help:"Guess id or unique prefix."`
	JSON bool   `help:"JSON on stdout."`
}

func (c *ShowCmd) Run(a *app) error {
	db, err := a.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	id, err := db.ResolveGuessID(a.ctx, c.ID)
	if err != nil {
		return storageErr(err)
	}
	x, err := db.GetGuess(a.ctx, id)
	if err != nil {
		return storageErr(err)
	}
	if c.JSON {
		return a.writeJSON(guessJSON{x, core.PriorityOf(&x)})
	}
	printGuess(a.stdout, &x)
	return nil
}

func printGuess(w io.Writer, x *core.Guess) {
	p := core.PriorityOf(x)
	fmt.Fprintf(w, "%s  %s  %s\n", x.ID, x.Status, p.Reason)
	fmt.Fprintf(w, "  %s\n", x.Summary)
	field := func(k, v string) {
		if v != "" {
			fmt.Fprintf(w, "  %-12s %s\n", k+":", v)
		}
	}
	field("kind", x.Kind)
	field("ambiguity", x.Ambiguity)
	field("chosen", x.Chosen)
	field("reason", x.Reason)
	field("alternative", x.Alternative)
	field("would ask", x.WouldAsk)
	if x.CwdRel != "" {
		field("cwd", "./"+x.CwdRel)
	} else {
		field("cwd", x.Cwd)
	}
	var files []string
	for _, f := range x.Files {
		if f.Line > 0 {
			files = append(files, fmt.Sprintf("%s:%d", f.Path, f.Line))
		} else {
			files = append(files, f.Path)
		}
	}
	field("files", strings.Join(files, ", "))
	field("note", x.ReviewNote)
	field("session", x.SessionID)
	field("created", x.CreatedAt.Local().Format("2006-01-02 15:04"))
}

// ---- accept / reject / followup ----

type StatusCmd struct {
	ID          string `arg:"" optional:"" help:"Guess id or unique prefix."`
	All         bool   `help:"Apply to every guess matching the filters instead of one id."`
	FilterFlags `embed:""`
	Note        string `help:"Optional review note."`
	Yes         bool   `short:"y" help:"With --all, skip the confirmation prompt."`
}

func (c *StatusCmd) Run(a *app, kctx *kong.Context) error {
	status := map[string]string{"accept": core.StatusAccepted, "reject": core.StatusRejected, "followup": core.StatusFollowup}[kctx.Selected().Name]
	verb := kctx.Selected().Name
	note := a.redactor.Text(c.Note)
	if c.All == (c.ID != "") {
		return usageErr(fmt.Sprintf("%s needs either a guess id or --all with filters", verb))
	}
	if !c.All {
		db, err := a.openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		id, err := db.ResolveGuessID(a.ctx, c.ID)
		if err != nil {
			return storageErr(err)
		}
		if err := db.SetStatus(a.ctx, id, status, note); err != nil {
			return storageErr(err)
		}
		a.printID(id)
		return nil
	}
	db, proj, err := a.openProject()
	if err != nil {
		return err
	}
	defer db.Close()
	f, err := c.filter(proj.ID)
	if err != nil {
		return err
	}
	if len(f.Statuses) == 0 {
		f.Statuses = []string{core.StatusUnreviewed, core.StatusFollowup}
	}
	n, err := db.CountWhere(a.ctx, f)
	if err != nil {
		return storageErr(err)
	}
	if err := a.confirm(n, verb, c.Yes); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	if _, err := db.BulkSetStatus(a.ctx, f, status, note); err != nil {
		return storageErr(err)
	}
	if !a.g.Quiet {
		fmt.Fprintf(a.stdout, "%d\n", n)
	}
	return nil
}

// ---- delete / purge ----

type DeleteCmd struct {
	ID          string `arg:"" optional:"" help:"Guess id or unique prefix."`
	All         bool   `help:"Delete every guess matching the filters instead of one id."`
	FilterFlags `embed:""`
	Yes         bool `short:"y" help:"With --all, skip the confirmation prompt."`
}

func (c *DeleteCmd) Run(a *app) error {
	if c.All == (c.ID != "") {
		return usageErr("delete needs either a guess id or --all with filters")
	}
	if !c.All {
		db, err := a.openDB()
		if err != nil {
			return err
		}
		defer db.Close()
		id, err := db.ResolveGuessID(a.ctx, c.ID)
		if err != nil {
			return storageErr(err)
		}
		if err := db.SoftDelete(a.ctx, id); err != nil {
			return storageErr(err)
		}
		a.printID(id)
		return nil
	}
	db, proj, err := a.openProject()
	if err != nil {
		return err
	}
	defer db.Close()
	f, err := c.filter(proj.ID)
	if err != nil {
		return err
	}
	n, err := db.CountWhere(a.ctx, f)
	if err != nil {
		return storageErr(err)
	}
	if err := a.confirm(n, "delete", c.Yes); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	if _, err := db.SoftDeleteWhere(a.ctx, f); err != nil {
		return storageErr(err)
	}
	if !a.g.Quiet {
		fmt.Fprintf(a.stdout, "%d\n", n)
	}
	return nil
}

type PurgeCmd struct {
	Hard          bool   `help:"Required. Acknowledges that purged guesses cannot be recovered."`
	DeletedBefore string `help:"Only purge guesses deleted before this time (YYYY-MM-DD, RFC3339, or duration ago)." placeholder:"WHEN"`
	AllProjects   bool   `help:"Purge across every project, not only the current one."`
	Yes           bool   `short:"y" help:"Skip the confirmation prompt."`
}

func (c *PurgeCmd) Run(a *app) error {
	if !c.Hard {
		return usageErr("purge is permanent; pass --hard to confirm you understand")
	}
	before, err := parseWhen(c.DeletedBefore)
	if err != nil {
		return usageErr("--deleted-before: " + err.Error())
	}
	f := storage.Filter{OnlyDeleted: true, DeletedBefore: before}
	var db *storage.DB
	if c.AllProjects {
		if db, err = a.openDB(); err != nil {
			return err
		}
	} else {
		var proj project
		if db, proj, err = a.openProject(); err != nil {
			return err
		}
		f.ProjectID = proj.ID
	}
	defer db.Close()
	n, err := db.CountWhere(a.ctx, f)
	if err != nil {
		return storageErr(err)
	}
	if err := a.confirm(n, "permanently purge", c.Yes); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	if _, err := db.PurgeDeleted(a.ctx, f); err != nil {
		return storageErr(err)
	}
	if !a.g.Quiet {
		fmt.Fprintf(a.stdout, "%d\n", n)
	}
	fmt.Fprintln(a.stderr, "note: SQLite reuses freed pages; run VACUUM on the database to shrink the file")
	return nil
}

// ---- session / sessions ----

type SessionCmd struct {
	Start SessionStartCmd `cmd:"" help:"Start a session and print its id."`
	End   SessionEndCmd   `cmd:"" help:"End a session."`
}

type SessionStartCmd struct {
	Agent string            `help:"Agent name, e.g. codex, claude-code."`
	Model string            `help:"Model name."`
	Label map[string]string `help:"Key=value label. Repeatable." placeholder:"KEY=VALUE"`
}

func (c *SessionStartCmd) Run(a *app) error {
	db, proj, err := a.openProject()
	if err != nil {
		return err
	}
	defer db.Close()
	cwd := ""
	if a.cfg.Recording.CaptureCwd {
		cwd, _ = os.Getwd()
	}
	id, err := db.StartSession(a.ctx, storage.Session{ProjectID: proj.ID, Agent: c.Agent, Model: c.Model, Cwd: cwd, Labels: c.Label})
	if err != nil {
		return storageErr(err)
	}
	a.printID(id)
	return nil
}

type SessionEndCmd struct {
	ID string `arg:"" optional:"" help:"Session id (default: $BLINKEN_SESSION)." env:"BLINKEN_SESSION"`
}

func (c *SessionEndCmd) Run(a *app) error {
	if c.ID == "" {
		return usageErr("session id required: pass it or set BLINKEN_SESSION")
	}
	db, err := a.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.EndSession(a.ctx, c.ID); err != nil {
		return storageErr(err)
	}
	a.printID(c.ID)
	return nil
}

type SessionsCmd struct {
	JSON bool `help:"JSON array on stdout."`
}

func (c *SessionsCmd) Run(a *app) error {
	db, proj, err := a.openProject()
	if err != nil {
		return err
	}
	defer db.Close()
	ss, err := db.ListSessions(a.ctx, proj.ID)
	if err != nil {
		return storageErr(err)
	}
	if c.JSON {
		return a.writeJSON(ss)
	}
	for _, s := range ss {
		state := "open"
		if s.EndedAt != nil {
			state = "ended"
		}
		agent := s.Agent
		if agent == "" {
			agent = "-"
		}
		fmt.Fprintf(a.stdout, "%s %-5s %2d %s %s\n", s.ID, state, s.Guesses, s.StartedAt.Local().Format("2006-01-02 15:04"), agent)
	}
	return nil
}

// ---- plumbing ----

type project struct{ ID, Name, Root string }

func (a *app) openDB() (*storage.DB, error) {
	path := a.g.DB
	if path == "" {
		var err error
		if path, err = storage.DefaultPath(); err != nil {
			return nil, storageErr(err)
		}
	}
	db, err := storage.Open(a.ctx, path)
	if err != nil {
		return nil, storageErr(err)
	}
	return db, nil
}

func (a *app) openProject() (*storage.DB, project, error) {
	db, err := a.openDB()
	if err != nil {
		return nil, project{}, err
	}
	start := a.g.Project
	if start == "" {
		start, _ = os.Getwd()
	}
	root, marker := core.ProjectRoot(start)
	id, err := db.ResolveProject(a.ctx, root, marker)
	if err != nil {
		db.Close()
		return nil, project{}, storageErr(err)
	}
	return db, project{ID: id, Name: baseName(root), Root: root}, nil
}

func baseName(p string) string {
	p = strings.TrimRight(p, "/\\")
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func (a *app) printID(id string) {
	if !a.g.Quiet {
		fmt.Fprintln(a.stdout, id)
	}
}

func (a *app) writeJSON(v any) error {
	enc := json.NewEncoder(a.stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// confirm reports the affected count and asks before a bulk action. Without a
// terminal it refuses unless --yes was given, so scripts cannot bulk-modify
// by accident.
func (a *app) confirm(n int, verb string, yes bool) error {
	if n == 0 {
		fmt.Fprintf(a.stderr, "no guesses match; nothing to %s\n", verb)
		return nil
	}
	if yes {
		return nil
	}
	if f, ok := a.stdin.(*os.File); !ok || !isatty.IsTerminal(f.Fd()) {
		return usageErr(fmt.Sprintf("%d guesses would be affected; pass --yes to %s them without a prompt", n, verb))
	}
	fmt.Fprintf(a.stderr, "%s %d guesses? [y/N] ", verb, n)
	line, _ := bufio.NewReader(a.stdin).ReadString('\n')
	if l := strings.ToLower(strings.TrimSpace(line)); l != "y" && l != "yes" {
		return usageErr("cancelled")
	}
	return nil
}

type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string { return e.err.Error() }
func (e exitError) Unwrap() error { return e.err }

func usageErr(msg string) error { return exitError{exitUsage, errors.New(msg)} }
func storageErr(err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return exitError{exitNotFound, err}
	}
	if errors.Is(err, storage.ErrAmbiguous) {
		return exitError{exitUsage, err}
	}
	return exitError{exitStorage, err}
}
