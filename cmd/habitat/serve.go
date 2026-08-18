package main

import (
	"flag"
	"fmt"
	"net/http"
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
	return serve(path, *addr)
}

func serve(dbPath, addr string) int {
	db, err := store.Open(dbPath)
	if err != nil {
		return frameworkFail("opening %s: %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()

	srv, err := server.New(db)
	if err != nil {
		return frameworkFail("building server: %v", err)
	}

	fmt.Printf("habitat serving %s on http://%s\n", dbPath, addr)
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
