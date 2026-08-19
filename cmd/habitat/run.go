package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gronnbeck/habitat/internal/config"
	"github.com/gronnbeck/habitat/internal/engine"
	"github.com/gronnbeck/habitat/internal/push"
	"github.com/gronnbeck/habitat/internal/report"
	"github.com/gronnbeck/habitat/internal/result"
	"github.com/gronnbeck/habitat/internal/store"
	"github.com/gronnbeck/habitat/internal/suite"
)

type runFlags struct {
	format      string
	output      string
	db          string
	dir         string
	repetitions int
	timeout     time.Duration
	noPersist   bool
	server      string
	noPush      bool
}

func cmdRun(args []string) int {
	own, runner := splitRunner(args)
	flags, positional := splitFlags(own)

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	opts := runFlags{}
	fs.StringVar(&opts.format, "format", "terminal", "report format: terminal or json")
	fs.StringVar(&opts.output, "output", "", "write the report to a file instead of stdout")
	fs.StringVar(&opts.db, "db", "", "SQLite file to persist into")
	fs.StringVar(&opts.dir, "dir", ".", "project directory containing habitat.yml")
	fs.IntVar(&opts.repetitions, "repetitions", 0, "override the suite's repetition count")
	fs.DurationVar(&opts.timeout, "timeout", 30*time.Minute, "maximum wall-clock time for the run")
	fs.BoolVar(&opts.noPersist, "no-persist", false, "do not write this run to the store")
	fs.StringVar(&opts.server, "server", "", "habitat server to report this run to (overrides habitat.yml)")
	fs.BoolVar(&opts.noPush, "no-push", false, "do not report this run to a server")
	if err := fs.Parse(flags); err != nil {
		return exitInvalid
	}
	if len(positional) != 1 {
		return fail("run needs exactly one suite, got %d", len(positional))
	}

	cfg, err := config.Load(opts.dir)
	if err != nil {
		return fail("%v", err)
	}
	return executeRun(cfg, positional[0], runner, opts)
}

func executeRun(cfg config.Config, suiteRef string, runner []string, opts runFlags) int {
	s, err := resolveSuite(cfg, suiteRef)
	if err != nil {
		return fail("%v", err)
	}
	command := runner
	if len(command) == 0 {
		command = cfg.Runner
	}
	if len(command) == 0 {
		return fail("no runner command: pass one after -- or set `runner:` in %s", config.Filename)
	}

	run, runErr := engine.Run(context.Background(), s, engine.Options{
		Command: command, Dir: cfg.Dir, Repetitions: opts.repetitions,
		GitSHA: detectGitSHA(cfg.Dir), Timeout: opts.timeout,
		Stdout: os.Stderr, Stderr: os.Stderr,
	})
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "habitat: %v\n", runErr)
	}
	return finishRun(cfg, run, runErr, opts)
}

func finishRun(cfg config.Config, run result.Run, runErr error, opts runFlags) int {
	if !opts.noPersist && run.ID != "" {
		if err := persist(cfg, opts.db, run); err != nil {
			fmt.Fprintf(os.Stderr, "habitat: persisting run: %v\n", err)
			return exitFrameworkErr
		}
	}
	if err := writeReport(run, opts); err != nil {
		return frameworkFail("writing report: %v", err)
	}
	pushRun(cfg, run, opts)
	if runErr != nil {
		return exitFrameworkErr
	}
	if run.Passed() {
		return exitPassed
	}
	return exitFailed
}

// pushRun uploads the finished run to a configured server, after the terminal
// report has already printed.
//
// A push failure is reported but never changes the exit code: the suite's
// verdict was decided locally, and an unreachable server is not a failing
// evaluation. Getting that backwards would make a network blip look like a
// regression.
func pushRun(cfg config.Config, run result.Run, opts runFlags) {
	server := opts.server
	if server == "" {
		server = cfg.Server
	}
	if server == "" || opts.noPush || run.ID == "" {
		return
	}
	stored, err := push.Run(server, os.Getenv("HABITAT_TOKEN"), run)
	if err != nil {
		fmt.Fprintf(os.Stderr, "habitat: could not push this run: %v\n", err)
		return
	}
	if stored.URL != "" {
		fmt.Printf("  reported to %s\n\n", stored.URL)
		return
	}
	fmt.Printf("  reported to %s\n\n", server)
}

func persist(cfg config.Config, dbOverride string, run result.Run) error {
	path := dbOverride
	if path == "" {
		path = cfg.DBPath()
	}
	db, err := store.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return db.SaveRun(run)
}

func writeReport(run result.Run, opts runFlags) error {
	out := os.Stdout
	if opts.output != "" {
		file, err := os.Create(opts.output) // #nosec G304 -- operator-supplied path
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		out = file
	}
	if opts.format == "json" {
		return report.JSON(out, run)
	}
	return report.Terminal(out, run)
}

// resolveSuite accepts either a path to a suite file or a bare suite name
// resolved inside the configured suite directory.
func resolveSuite(cfg config.Config, ref string) (*suite.Suite, error) {
	if strings.HasSuffix(ref, ".yml") || strings.HasSuffix(ref, ".yaml") {
		if _, err := os.Stat(ref); err == nil {
			return suite.Load(ref)
		}
	}
	dir := cfg.SuitesPath()
	for _, candidate := range []string{ref, ref + ".yml", ref + ".yaml"} {
		path := filepath.Join(dir, candidate)
		if _, err := os.Stat(path); err == nil {
			return suite.Load(path)
		}
	}
	return nil, fmt.Errorf("no suite %q in %s", ref, dir)
}

// detectGitSHA records what was running, so a stored run can be traced back
// to the code that produced it. Absence is not an error.
func detectGitSHA(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
