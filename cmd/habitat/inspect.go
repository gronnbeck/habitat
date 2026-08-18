package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gronnbeck/habitat/internal/config"
	"github.com/gronnbeck/habitat/internal/report"
	"github.com/gronnbeck/habitat/internal/store"
	"github.com/gronnbeck/habitat/internal/suite"
)

// cmdValidate checks suites without executing anything. It is free — no
// target is called — which is what makes it the right thing to run first.
func cmdValidate(args []string) int {
	flags, positional := splitFlags(args)
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory containing habitat.yml")
	if err := fs.Parse(flags); err != nil {
		return exitInvalid
	}
	cfg, err := config.Load(*dir)
	if err != nil {
		return fail("%v", err)
	}

	paths, err := suitePaths(cfg, positional)
	if err != nil {
		return fail("%v", err)
	}
	if len(paths) == 0 {
		return fail("no suites found in %s", cfg.SuitesPath())
	}

	failures := 0
	for _, path := range paths {
		if _, err := suite.Load(path); err != nil {
			fmt.Fprintf(os.Stderr, "  INVALID  %v\n", err)
			failures++
			continue
		}
		fmt.Printf("  OK       %s\n", filepath.Base(path))
	}
	if failures > 0 {
		fmt.Fprintf(os.Stderr, "\n%d of %d suites are invalid\n", failures, len(paths))
		return exitInvalid
	}
	fmt.Printf("\n%d suites valid\n", len(paths))
	return exitPassed
}

func cmdList(args []string) int {
	flags, _ := splitFlags(args)
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory containing habitat.yml")
	if err := fs.Parse(flags); err != nil {
		return exitInvalid
	}
	cfg, err := config.Load(*dir)
	if err != nil {
		return fail("%v", err)
	}
	paths, err := suite.Discover(cfg.SuitesPath())
	if err != nil {
		return fail("reading %s: %v", cfg.SuitesPath(), err)
	}
	if len(paths) == 0 {
		fmt.Printf("no suites in %s\n", cfg.SuitesPath())
		return exitPassed
	}
	for _, path := range paths {
		printSuiteLine(path)
	}
	return exitPassed
}

func printSuiteLine(path string) {
	name := filepath.Base(path)
	s, err := suite.Load(path)
	if err != nil {
		fmt.Printf("  %-40s (invalid)\n", name)
		return
	}
	fmt.Printf("  %-40s %s, %d cases\n", name, s.Target, len(s.Cases))
}

func cmdShow(args []string) int {
	flags, positional := splitFlags(args)
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory containing habitat.yml")
	db := fs.String("db", "", "SQLite file to read from")
	asJSON := fs.Bool("json", false, "print the full structured payload")
	if err := fs.Parse(flags); err != nil {
		return exitInvalid
	}
	if len(positional) != 1 {
		return fail("show needs exactly one run id")
	}
	cfg, err := config.Load(*dir)
	if err != nil {
		return fail("%v", err)
	}
	path := *db
	if path == "" {
		path = cfg.DBPath()
	}
	return printRun(path, positional[0], *asJSON)
}

func printRun(dbPath, runID string, asJSON bool) int {
	db, err := store.Open(dbPath)
	if err != nil {
		return frameworkFail("opening %s: %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()

	run, err := db.GetRun(runID)
	if err != nil {
		return fail("%v", err)
	}
	if asJSON {
		if err := report.JSON(os.Stdout, run); err != nil {
			return frameworkFail("%v", err)
		}
	} else if err := report.Terminal(os.Stdout, run); err != nil {
		return frameworkFail("%v", err)
	}
	if run.Passed() {
		return exitPassed
	}
	return exitFailed
}

// suitePaths resolves the suites named on the command line, or every suite in
// the configured directory when none are named.
func suitePaths(cfg config.Config, names []string) ([]string, error) {
	if len(names) == 0 {
		return suite.Discover(cfg.SuitesPath())
	}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		path, err := resolveSuitePath(cfg, name)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func resolveSuitePath(cfg config.Config, name string) (string, error) {
	if _, err := os.Stat(name); err == nil {
		return name, nil
	}
	for _, candidate := range []string{name, name + ".yml", name + ".yaml"} {
		path := filepath.Join(cfg.SuitesPath(), candidate)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no suite %q in %s", name, cfg.SuitesPath())
}
