package graders

import (
	"fmt"
	"strings"

	"github.com/gronnbeck/habitat/pkg/protocol"
)

func init() {
	Register(noError())
	Register(exactMatch())
	Register(includes())
	Register(stateMatch())
	Register(maximumDuration())
	Register(maximumCost())
}

func noError() Grader {
	return Grader{
		Name:     "no_error",
		Validate: func(o Options) error { return o.allow() },
		Grade: func(res *protocol.Result, _ Options) Outcome {
			if res.Failed() {
				return Outcome{Detail: fmt.Sprintf("target errored: %s", res.Error.Message)}
			}
			return Outcome{Passed: true, Detail: "no error"}
		},
	}
}

func exactMatch() Grader {
	return Grader{
		Name: "exact_match",
		Validate: func(o Options) error {
			if err := o.allow("value", "path"); err != nil {
				return err
			}
			return o.requirePresent("value")
		},
		Grade: func(res *protocol.Result, o Options) Outcome {
			path, _ := o.str("path")
			actual, found := lookupPath(res.Output, path)
			if !found {
				return Outcome{Detail: fmt.Sprintf("no value at output path %q", path)}
			}
			expected := o["value"]
			if equalValues(expected, actual) {
				return Outcome{Passed: true, Detail: fmt.Sprintf("%s == %s", pathLabel(path), describe(actual))}
			}
			return Outcome{Detail: fmt.Sprintf("%s: expected %s, got %s",
				pathLabel(path), describe(expected), describe(actual))}
		},
	}
}

func stateMatch() Grader {
	return Grader{
		Name: "state_match",
		Validate: func(o Options) error {
			if err := o.allow("value", "path"); err != nil {
				return err
			}
			if err := o.requireString("path"); err != nil {
				return err
			}
			return o.requirePresent("value")
		},
		Grade: func(res *protocol.Result, o Options) Outcome {
			path, _ := o.str("path")
			actual, found := lookupPath(anyMap(res.FinalState), path)
			if !found {
				return Outcome{Detail: fmt.Sprintf("final_state has no value at %q", path)}
			}
			expected := o["value"]
			if equalValues(expected, actual) {
				return Outcome{Passed: true, Detail: fmt.Sprintf("%s == %s", path, describe(actual))}
			}
			return Outcome{Detail: fmt.Sprintf("%s: expected %s, got %s", path, describe(expected), describe(actual))}
		},
	}
}

func includes() Grader {
	return Grader{
		Name: "includes",
		Validate: func(o Options) error {
			if err := o.allow("value", "path"); err != nil {
				return err
			}
			return o.requirePresent("value")
		},
		Grade: func(res *protocol.Result, o Options) Outcome {
			path, _ := o.str("path")
			actual, found := lookupPath(res.Output, path)
			if !found {
				return Outcome{Detail: fmt.Sprintf("no value at output path %q", path)}
			}
			return includesOutcome(actual, o["value"], path)
		},
	}
}

// includesOutcome is substring containment for strings and membership for
// arrays — the two things "includes" can sensibly mean.
func includesOutcome(actual, expected any, path string) Outcome {
	switch container := actual.(type) {
	case string:
		needle, ok := expected.(string)
		if !ok {
			return Outcome{Detail: "value must be a string to search a string"}
		}
		if strings.Contains(container, needle) {
			return Outcome{Passed: true, Detail: fmt.Sprintf("%s contains %s", pathLabel(path), describe(needle))}
		}
		return Outcome{Detail: fmt.Sprintf("%s does not contain %s", pathLabel(path), describe(needle))}
	case []any:
		for _, item := range container {
			if equalValues(expected, item) {
				return Outcome{Passed: true, Detail: fmt.Sprintf("%s includes %s", pathLabel(path), describe(expected))}
			}
		}
		return Outcome{Detail: fmt.Sprintf("%s does not include %s", pathLabel(path), describe(expected))}
	default:
		return Outcome{Detail: fmt.Sprintf("%s is neither a string nor an array", pathLabel(path))}
	}
}

func maximumDuration() Grader {
	return Grader{
		Name: "maximum_duration",
		Validate: func(o Options) error {
			if err := o.allow("maximum_ms"); err != nil {
				return err
			}
			return o.requireNumber("maximum_ms")
		},
		Grade: func(res *protocol.Result, o Options) Outcome {
			limit, _ := o.number("maximum_ms")
			actual := float64(res.DurationMS)
			if actual <= limit {
				return Outcome{Passed: true, Detail: fmt.Sprintf("%.0fms <= %.0fms", actual, limit)}
			}
			return Outcome{Detail: fmt.Sprintf("took %.0fms, limit %.0fms", actual, limit)}
		},
	}
}

func maximumCost() Grader {
	return Grader{
		Name: "maximum_cost",
		Validate: func(o Options) error {
			if err := o.allow("maximum_usd"); err != nil {
				return err
			}
			return o.requireNumber("maximum_usd")
		},
		Grade: func(res *protocol.Result, o Options) Outcome {
			limit, _ := o.number("maximum_usd")
			cost, reported := res.Cost()
			// A target that reports no cost cannot be over budget. Failing here
			// would grade the target's instrumentation, not its behaviour.
			if !reported {
				return Outcome{Passed: true, Detail: "no cost reported"}
			}
			if cost <= limit {
				return Outcome{Passed: true, Detail: fmt.Sprintf("$%.4f <= $%.4f", cost, limit)}
			}
			return Outcome{Detail: fmt.Sprintf("cost $%.4f, limit $%.4f", cost, limit)}
		},
	}
}

func pathLabel(path string) string {
	if path == "" {
		return "output"
	}
	return "output." + path
}

func anyMap(m map[string]any) any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
