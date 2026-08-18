// Package policy evaluates a suite's pass/fail rules against a finished run.
//
// A policy is layered on top of individual case results: a run can fail its
// policy with every case green (say, on average cost), and the default policy
// already requires every case to pass every repetition, so a policy block in
// a suite only ever adds constraints.
package policy

import (
	"fmt"

	"github.com/gronnbeck/habitat/internal/result"
	"github.com/gronnbeck/habitat/internal/suite"
)

// Evaluate returns the violated rules, in a stable order. An empty slice
// means the run satisfied its policy.
func Evaluate(p suite.Policy, run result.Run) []string {
	var violations []string
	violations = appendIf(violations, passRateViolation(p, run))
	violations = appendIf(violations, errorRateViolation(p, run))
	violations = appendIf(violations, criticalViolation(p, run))
	violations = appendIf(violations, costViolation(p, run))
	violations = appendIf(violations, durationViolation(p, run))
	return violations
}

func appendIf(violations []string, message string) []string {
	if message == "" {
		return violations
	}
	return append(violations, message)
}

func passRateViolation(p suite.Policy, run result.Run) string {
	if p.MinimumPassRate == nil {
		return ""
	}
	actual := run.PassRate()
	if actual >= *p.MinimumPassRate {
		return ""
	}
	return fmt.Sprintf("pass rate %.1f%% is below the minimum of %.1f%%", actual*100, *p.MinimumPassRate*100)
}

func errorRateViolation(p suite.Policy, run result.Run) string {
	if p.MaximumErrorRate == nil {
		return ""
	}
	actual := run.ErrorRate()
	if actual <= *p.MaximumErrorRate {
		return ""
	}
	return fmt.Sprintf("error rate %.1f%% exceeds the maximum of %.1f%%", actual*100, *p.MaximumErrorRate*100)
}

func criticalViolation(p suite.Policy, run result.Run) string {
	if !p.CriticalCasesMustPass {
		return ""
	}
	var failed []string
	for _, c := range run.Cases {
		if c.Critical && !c.Passed() {
			failed = append(failed, c.ID)
		}
	}
	if len(failed) == 0 {
		return ""
	}
	if len(failed) == 1 {
		return fmt.Sprintf("critical case %q failed", failed[0])
	}
	return fmt.Sprintf("%d critical cases failed: %v", len(failed), failed)
}

// costViolation is silent when nothing reported a cost — an unmeasured run is
// not a run that came in under budget, but neither is it a violation.
func costViolation(p suite.Policy, run result.Run) string {
	if p.MaximumAverageCost == nil {
		return ""
	}
	average, reported := run.AverageCost()
	if !reported || average <= *p.MaximumAverageCost {
		return ""
	}
	return fmt.Sprintf("average cost $%.4f exceeds the maximum of $%.4f", average, *p.MaximumAverageCost)
}

func durationViolation(p suite.Policy, run result.Run) string {
	if p.MaximumAverageDurationMS == nil {
		return ""
	}
	average := run.AverageDurationMS()
	if average <= *p.MaximumAverageDurationMS {
		return ""
	}
	return fmt.Sprintf("average duration %.0fms exceeds the maximum of %.0fms", average, *p.MaximumAverageDurationMS)
}
