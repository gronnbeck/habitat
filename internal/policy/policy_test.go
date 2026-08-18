package policy

import (
	"strings"
	"testing"

	"github.com/gronnbeck/habitat/internal/result"
	"github.com/gronnbeck/habitat/internal/suite"
	"github.com/gronnbeck/habitat/pkg/protocol"
)

func execution(passed bool) result.Execution {
	return result.Execution{Grades: []result.Grade{{Type: "no_error", Passed: passed}}}
}

func runWith(cases ...result.Case) result.Run {
	return result.Run{Cases: cases}
}

func TestMinimumPassRate(t *testing.T) {
	run := runWith(result.Case{
		ID:         "c",
		Executions: []result.Execution{execution(true), execution(false)},
	})
	threshold := 0.9
	violations := Evaluate(suite.Policy{MinimumPassRate: &threshold}, run)
	if len(violations) != 1 || !strings.Contains(violations[0], "pass rate") {
		t.Fatalf("expected a pass-rate violation, got %v", violations)
	}
}

func TestCriticalCaseMustPass(t *testing.T) {
	// A critical case failing sinks the suite regardless of the overall rate,
	// which is the whole point of marking one critical.
	run := runWith(
		result.Case{ID: "critical", Critical: true, Executions: []result.Execution{execution(false)}},
		result.Case{ID: "ok", Executions: []result.Execution{execution(true)}},
	)
	violations := Evaluate(suite.Policy{CriticalCasesMustPass: true}, run)
	if len(violations) != 1 || !strings.Contains(violations[0], "critical") {
		t.Fatalf("expected a critical-case violation, got %v", violations)
	}
}

func TestCostCeilingIsSilentWhenNothingReportedCost(t *testing.T) {
	// An unmeasured run is not a run that came in under budget, but neither is
	// it a violation — habitat never invents a cost on a target's behalf.
	run := runWith(result.Case{ID: "c", Executions: []result.Execution{execution(true)}})
	ceiling := 0.001
	if violations := Evaluate(suite.Policy{MaximumAverageCost: &ceiling}, run); len(violations) != 0 {
		t.Fatalf("expected no violation without reported cost, got %v", violations)
	}
}

func TestCostCeilingFiresOnReportedCost(t *testing.T) {
	cost := 0.5
	run := runWith(result.Case{ID: "c", Executions: []result.Execution{{
		Grades: []result.Grade{{Passed: true}},
		Result: protocol.Result{Usage: &protocol.Usage{CostUSD: &cost}},
	}}})
	ceiling := 0.01
	violations := Evaluate(suite.Policy{MaximumAverageCost: &ceiling}, run)
	if len(violations) != 1 || !strings.Contains(violations[0], "average cost") {
		t.Fatalf("expected a cost violation, got %v", violations)
	}
}

func TestCleanRunHasNoViolations(t *testing.T) {
	run := runWith(result.Case{ID: "c", Executions: []result.Execution{execution(true)}})
	full := 1.0
	if violations := Evaluate(suite.Policy{MinimumPassRate: &full}, run); len(violations) != 0 {
		t.Fatalf("expected no violations, got %v", violations)
	}
}
