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
	signupToken := os.Getenv("HABITAT_SIGNUP_TOKEN")
	users, err := db.CountUsers()
	if err != nil {
		return frameworkFail("counting users: %v", err)
	}
	// Nobody can sign in and nobody can sign up: the server would be a wall
	// with no door, and starting anyway risks it being a door with no wall.
	if !loopback && users == 0 && signupToken == "" {
		fmt.Fprintf(os.Stderr,
			"habitat: refusing to serve %s with no accounts and no signup token.\n"+
				"Set HABITAT_SIGNUP_TOKEN so an account can be created at /signup,\n"+
				"or create one directly:\n  habitat admin create-user <email> --db %s\n", addr, dbPath)
		return exitInvalid
	}

	srv, err := server.New(db, server.Config{
		RequireAuth:   !loopback,
		SecureCookies: os.Getenv("HABITAT_INSECURE_COOKIES") == "",
		SignupToken:   signupToken,
	})
	if err != nil {
		return frameworkFail("building server: %v", err)
	}

	describe(dbPath, addr, loopback, signupToken != "")
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

func describe(dbPath, addr string, loopback, signupOpen bool) {
	mode := "authenticated"
	if loopback {
		mode = "open (loopback only)"
	}
	if signupOpen {
		mode += ", signup enabled at /signup"
	}
	fmt.Printf("habitat serving %s on http://%s — %s\n", dbPath, addr, mode)
}
