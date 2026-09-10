package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/edmistond/blinken/internal/core"
	"github.com/edmistond/blinken/internal/server"
)

type ReviewCmd struct {
	FilterFlags `embed:""`
	JSON        bool `help:"JSON array on stdout instead of the verbose listing."`
	Serve       bool `help:"Serve the browser UI on loopback instead of printing."`
	NoOpen      bool `help:"With --serve, do not open the browser."`
	Port        int  `help:"With --serve, bind this port instead of a random one." default:"0"`
}

func (c *ReviewCmd) Run(a *app) error {
	db, proj, err := a.openProject()
	if err != nil {
		return err
	}
	defer db.Close()

	if c.Serve {
		srv, err := server.New(db, server.Project{ID: proj.ID, Name: proj.Name, Root: proj.Root}, server.Options{Port: c.Port, IncludeFollowup: a.cfg.Review.IncludeFollowup, Redactor: a.redactor})
		if err != nil {
			return err
		}
		fmt.Fprintln(a.stdout, srv.URL())
		if a.cfg.Review.OpenBrowser && !c.NoOpen {
			if err := server.OpenBrowser(srv.URL()); err != nil {
				fmt.Fprintln(a.stderr, "could not open browser:", err)
			}
		}
		ctx, stop := signal.NotifyContext(a.ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		return srv.Serve(ctx)
	}

	f, err := c.filter(proj.ID)
	if err != nil {
		return err
	}
	if len(f.Statuses) == 0 {
		f.Statuses = []string{core.StatusUnreviewed}
		if a.cfg.Review.IncludeFollowup {
			f.Statuses = append(f.Statuses, core.StatusFollowup)
		}
	}
	gs, err := db.ListGuesses(a.ctx, f)
	if err != nil {
		return storageErr(err)
	}
	core.SortByPriority(gs)
	if c.JSON {
		return a.writeJSON(withPriority(gs))
	}
	if len(gs) == 0 {
		fmt.Fprintln(a.stderr, "nothing to review")
		return nil
	}
	for i := range gs {
		if i > 0 {
			fmt.Fprintln(a.stdout)
		}
		printGuess(a.stdout, &gs[i])
	}
	return nil
}
