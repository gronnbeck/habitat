// Package engine orchestrates one run: expand a suite into executions, hand
// them to a runner, grade what comes back, and apply the suite's policy.
//
// The runner is a subprocess written in the target's own language. The engine
// never calls application code itself — it only ever grades what a runner
// reports, which is what keeps habitat language-agnostic.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/gronnbeck/habitat/internal/graders"
	"github.com/gronnbeck/habitat/internal/ingest"
	"github.com/gronnbeck/habitat/internal/policy"
	"github.com/gronnbeck/habitat/internal/result"
	"github.com/gronnbeck/habitat/internal/suite"
	"github.com/gronnbeck/habitat/pkg/protocol"
)

// Options configure one run.
type Options struct {
	// Command is the runner to launch, e.g. ["bundle", "exec", "ruby", "eval/run.rb"].
	Command []string
	// Dir is the working directory for the runner.
	Dir string
	// Repetitions overrides every case's repetition count when > 0.
	Repetitions int
	// GitSHA is recorded with the run.
	GitSHA string
	// Timeout bounds the whole run.
	Timeout time.Duration
	// Stdout and Stderr receive the runner's output.
	Stdout, Stderr *os.File
}

// Plan expands a suite into the executions a runner must perform.
func Plan(s *suite.Suite, runID string, repetitionOverride int) protocol.Plan {
	plan := protocol.Plan{RunID: runID, Suite: s.Name, Target: s.Target}
	for _, c := range s.Cases {
		reps := s.RepetitionsFor(c)
		if repetitionOverride > 0 {
			reps = repetitionOverride
		}
		for i := 0; i < reps; i++ {
			plan.Executions = append(plan.Executions, protocol.Execution{
				CaseID: c.ID, RepetitionIndex: i, Input: c.Input,
			})
		}
	}
	return plan
}

// Run executes a suite end to end and returns the graded, policy-evaluated
// result. A non-nil error means the run could not be completed; a completed
// run that simply failed its expectations returns a Run and no error.
func Run(ctx context.Context, s *suite.Suite, opts Options) (result.Run, error) {
	runID := newID()
	plan := Plan(s, runID, opts.Repetitions)
	if len(plan.Executions) == 0 {
		return result.Run{}, errors.New("suite expands to no executions")
	}

	token := newID()
	server := ingest.New(plan, token)
	url, closeServer, err := server.Listen()
	if err != nil {
		return result.Run{}, fmt.Errorf("starting ingest server: %w", err)
	}
	defer func() { _ = closeServer() }()

	started := time.Now()
	runErr := driveRunner(ctx, server, url, token, opts)
	finished := time.Now()

	run := assemble(s, runID, plan, server.Submissions(), opts.GitSHA, started, finished)
	if runErr != nil {
		run.Status = result.StatusErrored
		return run, runErr
	}
	return run, nil
}

// driveRunner launches the runner and waits for it to finish reporting.
func driveRunner(ctx context.Context, server *ingest.Server, url, token string, opts Options) error {
	if len(opts.Command) == 0 {
		return errors.New("no runner command given")
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, opts.Command[0], opts.Command[1:]...) // #nosec G204 -- operator-supplied
	cmd.Dir = opts.Dir
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	cmd.Env = append(os.Environ(),
		"HABITAT_URL="+url,
		"HABITAT_RUN_TOKEN="+token,
		"HABITAT_PROTOCOL="+protocol.Version,
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting runner: %w", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	select {
	case <-server.Done():
		// Every planned execution reported. Give the runner a moment to exit
		// on its own before moving on to grading.
		return waitBriefly(exited)
	case err := <-exited:
		if err != nil {
			return fmt.Errorf("runner exited: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitBriefly(exited chan error) error {
	select {
	case err := <-exited:
		if err != nil {
			return fmt.Errorf("runner exited: %w", err)
		}
		return nil
	case <-time.After(5 * time.Second):
		return nil
	}
}

// assemble grades every submission and applies the suite's policy.
func assemble(
	s *suite.Suite, runID string, plan protocol.Plan,
	submissions map[string]protocol.Result, gitSHA string, started, finished time.Time,
) result.Run {
	run := result.Run{
		ID: runID, SuiteName: s.Name, SuitePath: s.Path, Target: s.Target,
		Digest: s.Digest(), GitSHA: gitSHA, StartedAt: started, FinishedAt: finished,
	}
	for _, c := range s.Cases {
		run.Cases = append(run.Cases, gradeCase(c, plan, submissions))
	}
	run.PolicyViolations = policy.Evaluate(s.EffectivePolicy(), run)
	run.Status = statusFor(run)
	return run
}

func gradeCase(c suite.Case, plan protocol.Plan, submissions map[string]protocol.Result) result.Case {
	graded := result.Case{ID: c.ID, Title: c.Title, Critical: c.Critical, Tags: c.Tags}
	for _, planned := range plan.Executions {
		if planned.CaseID != c.ID {
			continue
		}
		res, reported := submissions[ingest.Key(c.ID, planned.RepetitionIndex)]
		if !reported {
			res = protocol.Result{Error: &protocol.Error{
				Type:    result.MissingErrorType,
				Class:   "NoResult",
				Message: "the runner reported no result for this execution",
			}}
		}
		graded.Executions = append(graded.Executions, result.Execution{
			CaseID: c.ID, RepetitionIndex: planned.RepetitionIndex,
			Result: res, Grades: gradeExecution(c, &res),
		})
	}
	return graded
}

func gradeExecution(c suite.Case, res *protocol.Result) []result.Grade {
	grades := make([]result.Grade, 0, len(c.Expectations))
	for _, expectation := range c.Expectations {
		outcome := graders.Grade(expectation.Type, res, expectation.Options)
		grades = append(grades, result.Grade{
			Type: expectation.Type, Passed: outcome.Passed, Detail: outcome.Detail,
		})
	}
	return grades
}

func statusFor(run result.Run) string {
	if len(run.PolicyViolations) > 0 {
		return result.StatusFailed
	}
	for _, c := range run.Cases {
		if !c.Passed() {
			return result.StatusFailed
		}
	}
	return result.StatusPassed
}

func newID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
