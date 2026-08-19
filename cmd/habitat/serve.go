package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gronnbeck/habitat/internal/config"
	"github.com/gronnbeck/habitat/internal/server"
	"github.com/gronnbeck/habitat/internal/store"
)

func cmdServe(args []string) int {
	flags, _ := splitFlags(args)
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory containing habitat.yml")
	db := fs.String("db", "", "SQLite file to serve")
	addr := fs.String("addr", "127.0.0.1:7878", "address to listen on")
	if err := fs.Parse(flags); err != nil {
		return exitInvalid
	}

	cfg, err := config.Load(*dir)
	if err != nil {
		return fail("%v", err)
	}
	path := *db
	if path == "" {
		path = cfg.DBPath()
	}
	// PORT is what a container platform sets; honour it over the default so
	// the deployed server needs no bespoke flag.
	listen := *addr
	if port := os.Getenv("PORT"); port != "" && !wasSet(fs, "addr") {
		listen = "0.0.0.0:" + port
	}
	return serve(path, listen)
}

// wasSet reports whether a flag was given on the command line, as opposed to
// carrying its default. An explicit --addr must win over PORT.
func wasSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func serve(dbPath, addr string) int {
	db, err := store.Open(dbPath)
	if err != nil {
		return frameworkFail("opening %s: %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()

	// A server reachable by other people must not be readable without signing
	// in. Rather than trusting a flag to be set correctly, refuse to start:
	// binding beyond loopback with nobody able to log in can only be an
	// accident, and the failure mode is publishing everyone's prompts.
	loopback := server.IsLoopback(addr)
	if err := bootstrapUser(db); err != nil {
		return frameworkFail("%v", err)
	}
	users, err := db.CountUsers()
	if err != nil {
		return frameworkFail("counting users: %v", err)
	}
	if !loopback && users == 0 {
		fmt.Fprintf(os.Stderr,
			"habitat: refusing to serve %s with no accounts — anyone could read every run.\n"+
				"Create one first:\n  habitat admin create-user <email> --db %s\n", addr, dbPath)
		return exitInvalid
	}

	srv, err := server.New(db, server.Config{
		RequireAuth:   !loopback,
		SecureCookies: os.Getenv("HABITAT_INSECURE_COOKIES") == "",
	})
	if err != nil {
		return frameworkFail("building server: %v", err)
	}

	describe(dbPath, addr, loopback)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := httpServer.ListenAndServe(); err != nil {
		return frameworkFail("%v", err)
	}
	return exitPassed
}

// bootstrapUser creates the first account from the environment, and only when
// there are none.
//
// Without it a first deploy cannot succeed: the server refuses to serve with
// no accounts, so its health check fails, so the deploy rolls back before
// anyone can create one. This breaks that cycle without weakening the rule —
// the server still never serves data to an anonymous stranger.
func bootstrapUser(db *store.Store) error {
	email := os.Getenv("HABITAT_BOOTSTRAP_EMAIL")
	password := os.Getenv("HABITAT_BOOTSTRAP_PASSWORD")
	if email == "" || password == "" {
		return nil
	}
	users, err := db.CountUsers()
	if err != nil {
		return err
	}
	// Only ever the first account. Otherwise leaving the variable set would
	// silently recreate an account someone had deliberately removed.
	if users > 0 {
		return nil
	}
	if _, err := db.CreateUser(email, password); err != nil {
		return fmt.Errorf("creating the bootstrap account: %w", err)
	}
	fmt.Printf("created the first account for %s from HABITAT_BOOTSTRAP_EMAIL\n", email)
	return nil
}

func describe(dbPath, addr string, loopback bool) {
	mode := "authenticated"
	if loopback {
		mode = "open (loopback only)"
	}
	fmt.Printf("habitat serving %s on http://%s — %s\n", dbPath, addr, mode)
}
