// Package report renders a finished run.
//
// Terminal output is the default; JSON is the full structured payload and the
// shape a baseline comparison reads back in.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/gronnbeck/habitat/internal/result"
)

// JSON writes the complete structured payload.
func JSON(w io.Writer, run result.Run) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(run)
}

// Terminal writes the human-readable report: one line per case, then the
// suite's totals.
func Terminal(w io.Writer, run result.Run) error {
	if _, err := fmt.Fprintf(w, "\n%s  (target: %s)\n\n", run.SuiteName, run.Target); err != nil {
		return err
	}
	if err := writeIncompleteNotice(w, run); err != nil {
		return err
	}
	width := longestCaseID(run)
	for _, c := range run.Cases {
		if err := writeCase(w, c, width); err != nil {
			return err
		}
	}
	return writeSummary(w, run)
}

func writeCase(w io.Writer, c result.Case, width int) error {
	status := "FAIL"
	if c.Passed() {
		status = "PASS"
	}
	// The X/Y fraction stays visible even when the case fails outright, so a
	// consistently wrong case reads differently from a merely flaky one.
	_, err := fmt.Fprintf(w, "  %s  %-*s  %d/%d repetitions passed\n",
		status, width, c.ID, c.PassedCount(), len(c.Executions))
	if err != nil || c.Passed() {
		return err
	}
	return writeFailureDetail(w, c)
}

// writeIncompleteNotice explains a run the runner never finished, before any
// case lines. Without it the table below reads as "the agent got everything
// wrong", when the truth is that nothing was ever asked of it.
func writeIncompleteNotice(w io.Writer, run result.Run) error {
	missing := run.MissingCount()
	if missing == 0 {
		return nil
	}
	_, err := fmt.Fprintf(w,
		"  INCOMPLETE  the runner reported no result for %d of %d executions.\n"+
			"              Those count as failures below, but they are evidence about the\n"+
			"              runner, not about the target. Check the runner output above.\n\n",
		missing, len(run.Executions()))
	return err
}

// writeFailureDetail explains the first failing repetition. One is enough to
// act on, and printing every one buries the signal.
func writeFailureDetail(w io.Writer, c result.Case) error {
	for _, e := range c.Executions {
		if e.Missing() {
			if _, err := fmt.Fprintf(w, "          not reported by the runner\n"); err != nil {
				return err
			}
			return nil
		}
		failed := e.FailedGrades()
		if len(failed) == 0 {
			continue
		}
		for _, g := range failed {
			if _, err := fmt.Fprintf(w, "          %s: %s\n", g.Type, g.Detail); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

func writeSummary(w io.Writer, run result.Run) error {
	executions := len(run.Executions())
	lines := []string{
		fmt.Sprintf("\n  %d cases, %d executions, %.0f%% pass rate",
			len(run.Cases), executions, run.PassRate()*100),
	}
	if average, reported := run.AverageCost(); reported {
		lines = append(lines, fmt.Sprintf("  average cost $%.4f per execution, $%.4f total",
			average, average*float64(executions)))
	}
	lines = append(lines, fmt.Sprintf("  average duration %.0fms per execution", run.AverageDurationMS()))
	if _, err := fmt.Fprintln(w, strings.Join(lines, "\n")); err != nil {
		return err
	}
	return writeVerdict(w, run)
}

func writeVerdict(w io.Writer, run result.Run) error {
	// A policy verdict computed partly from executions that never happened is
	// not a verdict worth printing: it would blame the target for the runner.
	if run.Status == result.StatusErrored {
		_, err := fmt.Fprintf(w, "\n  %s  (run %s) — the run did not complete\n\n",
			strings.ToUpper(run.Status), run.ID)
		return err
	}
	for _, violation := range run.PolicyViolations {
		if _, err := fmt.Fprintf(w, "\n  POLICY  %s", violation); err != nil {
			return err
		}
	}
	if len(run.PolicyViolations) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "\n  %s  (run %s)\n\n", strings.ToUpper(run.Status), run.ID)
	return err
}

func longestCaseID(run result.Run) int {
	width := 0
	for _, c := range run.Cases {
		if len(c.ID) > width {
			width = len(c.ID)
		}
	}
	return width
}
