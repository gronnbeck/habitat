// Package result holds the shape of a finished run: what was executed, how
// each execution was graded, and the aggregates every report and policy is
// computed from.
//
// It deliberately has no behaviour beyond arithmetic — grading lives in
// graders, pass/fail rules in policy, rendering in report.
package result

import (
	"time"

	"github.com/gronnbeck/habitat/pkg/protocol"
)

// Run statuses. A run that never finished executing is neither passed nor
// failed: it is errored, and reports say so rather than implying a verdict.
const (
	StatusPassed  = "passed"
	StatusFailed  = "failed"
	StatusErrored = "errored"
)

// Grade is one expectation's verdict on one execution.
type Grade struct {
	Type   string `json:"type"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

// Execution is one attempt at one case, with every expectation's verdict.
type Execution struct {
	CaseID          string          `json:"case_id"`
	RepetitionIndex int             `json:"repetition_index"`
	Result          protocol.Result `json:"result"`
	Grades          []Grade         `json:"grades"`
}

// MissingErrorType marks an execution the runner never reported. It is graded
// as a failure so nothing silently passes, but reports distinguish it from a
// genuine wrong answer — a crashed runner is not evidence about the agent.
const MissingErrorType = "missing"

// Missing reports whether the runner never submitted a result for this
// execution.
func (e Execution) Missing() bool {
	return e.Result.Error != nil && e.Result.Error.Type == MissingErrorType
}

// Passed reports whether every expectation held for this attempt.
func (e Execution) Passed() bool {
	for _, g := range e.Grades {
		if !g.Passed {
			return false
		}
	}
	return len(e.Grades) > 0
}

// FailedGrades lists the expectations that did not hold.
func (e Execution) FailedGrades() []Grade {
	var failed []Grade
	for _, g := range e.Grades {
		if !g.Passed {
			failed = append(failed, g)
		}
	}
	return failed
}

// Case collects every repetition of one case.
type Case struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Critical   bool        `json:"critical"`
	Tags       []string    `json:"tags"`
	Executions []Execution `json:"executions"`
}

// PassedCount is how many repetitions passed.
func (c Case) PassedCount() int {
	count := 0
	for _, e := range c.Executions {
		if e.Passed() {
			count++
		}
	}
	return count
}

// Passed requires every repetition to pass. The X/Y fraction stays visible in
// reports regardless, so a consistently wrong case reads differently from a
// merely flaky one.
func (c Case) Passed() bool {
	return len(c.Executions) > 0 && c.PassedCount() == len(c.Executions)
}

// Run is one execution of one suite.
type Run struct {
	ID               string    `json:"id"`
	SuiteName        string    `json:"suite_name"`
	SuitePath        string    `json:"suite_path"`
	Target           string    `json:"target"`
	Digest           string    `json:"digest"`
	GitSHA           string    `json:"git_sha,omitempty"`
	StartedAt        time.Time `json:"started_at"`
	FinishedAt       time.Time `json:"finished_at"`
	Cases            []Case    `json:"cases"`
	PolicyViolations []string  `json:"policy_violations,omitempty"`
	Status           string    `json:"status"`
}

// Executions flattens every attempt in the run.
func (r Run) Executions() []Execution {
	var all []Execution
	for _, c := range r.Cases {
		all = append(all, c.Executions...)
	}
	return all
}

// PassRate is the fraction of executions that passed, which is a finer
// measure than the fraction of cases that passed.
func (r Run) PassRate() float64 {
	all := r.Executions()
	if len(all) == 0 {
		return 0
	}
	passed := 0
	for _, e := range all {
		if e.Passed() {
			passed++
		}
	}
	return float64(passed) / float64(len(all))
}

// ErrorRate is the fraction of executions where the target itself errored.
func (r Run) ErrorRate() float64 {
	all := r.Executions()
	if len(all) == 0 {
		return 0
	}
	errored := 0
	for _, e := range all {
		if e.Result.Failed() {
			errored++
		}
	}
	return float64(errored) / float64(len(all))
}

// AverageCost reports the mean cost per execution, and whether any execution
// reported a cost at all. Cost is target-dependent, never assumed suite-wide:
// when nothing reports one, reports omit cost instead of printing $0.00.
func (r Run) AverageCost() (float64, bool) {
	total, counted := 0.0, 0
	for _, e := range r.Executions() {
		if cost, ok := e.Result.Cost(); ok {
			total += cost
			counted++
		}
	}
	if counted == 0 {
		return 0, false
	}
	return total / float64(counted), true
}

// AverageDurationMS is the mean wall-clock duration per execution.
func (r Run) AverageDurationMS() float64 {
	all := r.Executions()
	if len(all) == 0 {
		return 0
	}
	total := int64(0)
	for _, e := range all {
		total += e.Result.DurationMS
	}
	return float64(total) / float64(len(all))
}

// MissingCount is how many planned executions the runner never reported.
func (r Run) MissingCount() int {
	count := 0
	for _, e := range r.Executions() {
		if e.Missing() {
			count++
		}
	}
	return count
}

// CasesPassed counts the cases where every repetition passed.
func (r Run) CasesPassed() int {
	count := 0
	for _, c := range r.Cases {
		if c.Passed() {
			count++
		}
	}
	return count
}

// Passed is the run's overall verdict: every case passed and no policy rule
// was violated.
func (r Run) Passed() bool {
	return r.Status == StatusPassed
}
