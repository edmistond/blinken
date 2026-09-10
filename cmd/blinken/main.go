// Command blinken records the consequential guesses coding agents make.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/alecthomas/kong"

	"github.com/edmistond/blinken/internal/core"
	"github.com/edmistond/blinken/internal/server"
	"github.com/edmistond/blinken/internal/storage"
)

// Exit codes are part of the automation contract.
const (
	exitOK       = 0
	exitUnexpect = 1
	exitUsage    = 2
	exitNotFound = 3
	exitStorage  = 4
)

type Globals struct {
	DB      string `help:"Database path (default: OS data dir)." env:"BLINKEN_DB" placeholder:"PATH"`
	Project string `help:"Project root (default: detected from cwd)." type:"path" placeholder:"DIR"`
	Quiet   bool   `short:"q" help:"Print nothing on success."`
}

type CLI struct {
	Globals

	Guess    GuessCmd   `cmd:"" help:"Record a guess. Prints the new guess id."`
	Guesses  GuessesCmd `cmd:"" help:"Summarize or list guesses for the current project."`
	Show     ShowCmd    `cmd:"" help:"Show one guess."`
	Review   ReviewCmd  `cmd:"" help:"Review unresolved guesses, in the terminal or a browser."`
	Accept   StatusCmd  `cmd:"" help:"Mark a guess accepted."`
	Reject   StatusCmd  `cmd:"" help:"Mark a guess rejected."`
	Followup StatusCmd  `cmd:"" help:"Mark a guess for follow-up."`
	Session  SessionCmd `cmd:"" help:"Start or end a logical agent session."`
	Version  VersionCmd `cmd:"" help:"Print the version."`
}

var version = "dev"

type VersionCmd struct{}

func (VersionCmd) Run() error { fmt.Println("blinken " + version); return nil }

// ---- guess ----

type GuessCmd struct {
	Text          string   `arg:"" optional:"" help:"Summary of the guess (same as --summary)."`
	Summary       string   `help:"Summary of the guess; use for multiline or scripted input." placeholder:"TEXT"`
	Kind          string   `help:"One of: ${kinds}." enum:",assumption,interpretation,tradeoff,deviation,inference" default:""`
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
}

func (c *GuessCmd) Run(g *Globals) error {
	summary := c.Text
	if c.Summary != "" {
		summary = c.Summary
	}
	if strings.TrimSpace(summary) == "" {
		return usageErr("a summary is required: blinken guess \"what you decided\"")
	}
	ctx := context.Background()
	db, proj, err := openProject(ctx, g)
	if err != nil {
		return err
	}
	defer db.Close()

	guess := core.Guess{
		ProjectID: proj.ID, SessionID: c.Session, Summary: summary, Ambiguity: c.Ambiguity, Chosen: c.Chosen,
		Kind: c.Kind, Confidence: c.Confidence, Impact: c.Impact, Reversibility: c.Reversibility,
		Reason: c.Reason, Alternative: c.Alternative, WouldAsk: c.WouldAsk,
	}
	if !c.NoCwd {
		if cwd, err := os.Getwd(); err == nil {
			guess.Cwd = cwd
			guess.CwdRel = core.RelPath(proj.Root, cwd)
		}
	}
	for _, f := range c.File {
		ref, err := core.ParseFileRef(f)
		if err != nil {
			return usageErr(err.Error())
		}
		guess.Files = append(guess.Files, ref)
	}
	if err := db.InsertGuess(ctx, &guess); err != nil {
		return storageErr(err)
	}
	if !g.Quiet {
		fmt.Println(guess.ID)
	}
	return nil
}

// ---- guesses ----

type GuessesCmd struct {
	Compact bool     `help:"One row per guess: ID IMPACT/CONFIDENCE KIND SUMMARY."`
	JSON    bool     `help:"JSON array on stdout."`
	Status  []string `help:"Filter by status (default: unreviewed, followup)." enum:"unreviewed,accepted,rejected,followup"`
	Session string   `help:"Filter by session id." placeholder:"ID"`
	All     bool     `help:"Include every status."`
}

func (c *GuessesCmd) Run(g *Globals) error {
	ctx := context.Background()
	db, proj, err := openProject(ctx, g)
	if err != nil {
		return err
	}
	defer db.Close()
	statuses := c.Status
	if len(statuses) == 0 && !c.All {
		statuses = []string{core.StatusUnreviewed, core.StatusFollowup}
	}
	gs, err := db.ListGuesses(ctx, storage.Filter{ProjectID: proj.ID, SessionID: c.Session, Statuses: statuses})
	if err != nil {
		return storageErr(err)
	}
	core.SortByPriority(gs)
	switch {
	case c.JSON:
		return writeJSON(gs)
	case c.Compact:
		for _, x := range gs {
			kind := x.Kind
			if kind == "" {
				kind = "-"
			}
			fmt.Printf("%s %s %-14s %s\n", x.ID, x.Abbrev(), kind, x.Summary)
		}
	default:
		counts, err := db.CountGuesses(ctx, proj.ID)
		if err != nil {
			return storageErr(err)
		}
		fmt.Printf("%d unreviewed | %d followup | %d high-impact | %d low-confidence\n", counts.Unreviewed, counts.Followup, counts.HighImpact, counts.LowConfidence)
	}
	return nil
}

// ---- show ----

type ShowCmd struct {
	ID   string `arg:"" help:"Guess id."`
	JSON bool   `help:"JSON on stdout."`
}

func (c *ShowCmd) Run(g *Globals) error {
	ctx := context.Background()
	db, err := openDB(ctx, g)
	if err != nil {
		return err
	}
	defer db.Close()
	x, err := db.GetGuess(ctx, c.ID)
	if err != nil {
		return storageErr(err)
	}
	if c.JSON {
		return writeJSON(x)
	}
	printGuess(&x)
	return nil
}

func printGuess(x *core.Guess) {
	p := core.PriorityOf(x)
	fmt.Printf("%s  %s  %s\n", x.ID, x.Status, p.Reason)
	fmt.Printf("  %s\n", x.Summary)
	field := func(k, v string) {
		if v != "" {
			fmt.Printf("  %-12s %s\n", k+":", v)
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

// ---- review ----

type ReviewCmd struct {
	Serve  bool `help:"Serve the browser UI on loopback instead of printing."`
	NoOpen bool `help:"With --serve, do not open the browser."`
	Port   int  `help:"With --serve, bind this port instead of a random one." default:"0"`
}

func (c *ReviewCmd) Run(g *Globals) error {
	ctx := context.Background()
	db, proj, err := openProject(ctx, g)
	if err != nil {
		return err
	}
	defer db.Close()

	if !c.Serve {
		gs, err := db.ListGuesses(ctx, storage.Filter{ProjectID: proj.ID, Statuses: []string{core.StatusUnreviewed, core.StatusFollowup}})
		if err != nil {
			return storageErr(err)
		}
		core.SortByPriority(gs)
		if len(gs) == 0 {
			fmt.Fprintln(os.Stderr, "nothing to review")
			return nil
		}
		for i := range gs {
			if i > 0 {
				fmt.Println()
			}
			printGuess(&gs[i])
		}
		return nil
	}

	srv, err := server.New(db, server.Project{ID: proj.ID, Name: proj.Name, Root: proj.Root}, c.Port)
	if err != nil {
		return err
	}
	fmt.Println(srv.URL())
	if !c.NoOpen {
		if err := server.OpenBrowser(srv.URL()); err != nil {
			fmt.Fprintln(os.Stderr, "could not open browser:", err)
		}
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Serve(ctx)
}

// ---- accept / reject / followup ----

type StatusCmd struct {
	ID   string `arg:"" help:"Guess id."`
	Note string `help:"Optional review note."`
}

func (c *StatusCmd) Run(g *Globals, kctx *kong.Context) error {
	status := map[string]string{"accept": core.StatusAccepted, "reject": core.StatusRejected, "followup": core.StatusFollowup}[kctx.Selected().Name]
	ctx := context.Background()
	db, err := openDB(ctx, g)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.SetStatus(ctx, c.ID, status, c.Note); err != nil {
		return storageErr(err)
	}
	if !g.Quiet {
		fmt.Println(c.ID)
	}
	return nil
}

// ---- session ----

type SessionCmd struct {
	Start SessionStartCmd `cmd:"" help:"Start a session and print its id."`
	End   SessionEndCmd   `cmd:"" help:"End a session."`
}

type SessionStartCmd struct {
	Agent string `help:"Agent name, e.g. codex, claude-code."`
	Model string `help:"Model name."`
}

func (c *SessionStartCmd) Run(g *Globals) error {
	ctx := context.Background()
	db, proj, err := openProject(ctx, g)
	if err != nil {
		return err
	}
	defer db.Close()
	cwd, _ := os.Getwd()
	id, err := db.StartSession(ctx, storage.Session{ProjectID: proj.ID, Agent: c.Agent, Model: c.Model, Cwd: cwd})
	if err != nil {
		return storageErr(err)
	}
	if !g.Quiet {
		fmt.Println(id)
	}
	return nil
}

type SessionEndCmd struct {
	ID string `arg:"" optional:"" help:"Session id (default: $BLINKEN_SESSION)." env:"BLINKEN_SESSION"`
}

func (c *SessionEndCmd) Run(g *Globals) error {
	if c.ID == "" {
		return usageErr("session id required: pass it or set BLINKEN_SESSION")
	}
	ctx := context.Background()
	db, err := openDB(ctx, g)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.EndSession(ctx, c.ID); err != nil {
		return storageErr(err)
	}
	if !g.Quiet {
		fmt.Println(c.ID)
	}
	return nil
}

// ---- plumbing ----

type project struct{ ID, Name, Root string }

func openDB(ctx context.Context, g *Globals) (*storage.DB, error) {
	path := g.DB
	if path == "" {
		var err error
		if path, err = storage.DefaultPath(); err != nil {
			return nil, storageErr(err)
		}
	}
	db, err := storage.Open(ctx, path)
	if err != nil {
		return nil, storageErr(err)
	}
	return db, nil
}

func openProject(ctx context.Context, g *Globals) (*storage.DB, project, error) {
	db, err := openDB(ctx, g)
	if err != nil {
		return nil, project{}, err
	}
	start := g.Project
	if start == "" {
		start, _ = os.Getwd()
	}
	root, marker := core.ProjectRoot(start)
	id, err := db.ResolveProject(ctx, root, marker)
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

func writeJSON(v any) error {
	enc := jsonEncoder(os.Stdout)
	return enc.Encode(v)
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
	return exitError{exitStorage, err}
}

func main() {
	var cli CLI
	parser, err := kong.New(&cli,
		kong.Name("blinken"),
		kong.Description("Record the consequential guesses coding agents make, and review them.\n\nRanking flags default to medium confidence, medium impact, moderate reversibility when omitted; the defaults are labels, not measurements."),
		kong.UsageOnError(),
		kong.Vars{"kinds": strings.Join(core.Kinds, ", ")},
		kong.ConfigureHelp(kong.HelpOptions{Compact: true, NoExpandSubcommands: true}),
		kong.Exit(func(code int) { os.Exit(code) }),
	)
	if err != nil {
		panic(err)
	}
	kctx, err := parser.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "blinken:", err)
		os.Exit(exitUsage)
	}
	if err := kctx.Run(&cli.Globals); err != nil {
		var ee exitError
		code := exitUnexpect
		if errors.As(err, &ee) {
			code = ee.code
		}
		fmt.Fprintln(os.Stderr, "blinken:", err)
		os.Exit(code)
	}
}
