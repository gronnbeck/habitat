package graders

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// lookupPath walks a dot-separated path into decoded JSON. An empty path
// returns the root, so every path-taking grader can treat "no path" as
// "the whole thing" without a special case.
//
// Numeric segments index into arrays, so "risks.0.title" works.
func lookupPath(root any, path string) (any, bool) {
	if path == "" {
		return root, true
	}
	current := root
	for _, segment := range strings.Split(path, ".") {
		next, ok := step(current, segment)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func step(current any, segment string) (any, bool) {
	switch node := current.(type) {
	case map[string]any:
		value, ok := node[segment]
		return value, ok
	case []any:
		index, err := strconv.Atoi(segment)
		if err != nil || index < 0 || index >= len(node) {
			return nil, false
		}
		return node[index], true
	default:
		return nil, false
	}
}

// toFloat normalises every numeric representation that can reach us. YAML
// decodes whole numbers as int, JSON decodes them as float64, and a target
// may send either — so 1 and 1.0 have to compare equal.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// equalValues compares an expected value from YAML against an actual value
// from JSON. Numbers compare numerically regardless of representation;
// everything else compares structurally.
func equalValues(expected, actual any) bool {
	if expected == nil || actual == nil {
		return expected == nil && actual == nil
	}
	if e, ok := toFloat(expected); ok {
		a, ok := toFloat(actual)
		return ok && e == a
	}
	switch e := expected.(type) {
	case string:
		a, ok := actual.(string)
		return ok && e == a
	case bool:
		a, ok := actual.(bool)
		return ok && e == a
	default:
		return equalComposite(expected, actual)
	}
}

// equalComposite compares maps and slices by their canonical JSON, which
// side-steps YAML's map[any]any versus JSON's map[string]any mismatch.
func equalComposite(expected, actual any) bool {
	left, err := json.Marshal(normalise(expected))
	if err != nil {
		return false
	}
	right, err := json.Marshal(normalise(actual))
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

// normalise rewrites YAML's map keys to strings so a decoded suite value and
// a decoded JSON value have the same shape.
func normalise(v any) any {
	switch node := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(node))
		for key, value := range node {
			out[fmt.Sprint(key)] = normalise(value)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(node))
		for key, value := range node {
			out[key] = normalise(value)
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i, value := range node {
			out[i] = normalise(value)
		}
		return out
	default:
		return v
	}
}

// describe renders a value for a grader's detail line, keeping it short
// enough to stay readable in a terminal report.
func describe(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(truncate(s))
	}
	encoded, err := json.Marshal(normalise(v))
	if err != nil {
		return fmt.Sprint(v)
	}
	return truncate(string(encoded))
}

func truncate(s string) string {
	const limit = 120
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
