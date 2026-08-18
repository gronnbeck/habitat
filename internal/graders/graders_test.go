package graders

import (
	"testing"

	"github.com/gronnbeck/habitat/pkg/protocol"
)

func TestStateMatchComparesNumbersAcrossRepresentations(t *testing.T) {
	// YAML gives int, JSON gives float64. A suite author writing `value: 1`
	// must match a target reporting 1.0, or every numeric check is a coin flip.
	res := &protocol.Result{FinalState: map[string]any{"count": float64(1)}}
	got := Grade("state_match", res, Options{"path": "count", "value": 1})
	if !got.Passed {
		t.Fatalf("expected int 1 to match float64 1.0, got %q", got.Detail)
	}
}

func TestStateMatchFailsOnMissingPath(t *testing.T) {
	res := &protocol.Result{FinalState: map[string]any{"other": 1}}
	got := Grade("state_match", res, Options{"path": "count", "value": 1})
	if got.Passed {
		t.Fatal("a missing path must fail rather than pass vacuously")
	}
}

func TestStateMatchReadsNestedPaths(t *testing.T) {
	res := &protocol.Result{FinalState: map[string]any{
		"risks": []any{map[string]any{"title": "fraud"}},
	}}
	got := Grade("state_match", res, Options{"path": "risks.0.title", "value": "fraud"})
	if !got.Passed {
		t.Fatalf("expected nested path lookup to work, got %q", got.Detail)
	}
}

func TestNoErrorReflectsTargetFailure(t *testing.T) {
	failed := &protocol.Result{Error: &protocol.Error{Message: "boom"}}
	if Grade("no_error", failed, Options{}).Passed {
		t.Fatal("no_error must fail when the target errored")
	}
	if !Grade("no_error", &protocol.Result{}, Options{}).Passed {
		t.Fatal("no_error must pass when the target did not error")
	}
}

func TestIncludesHandlesStringsAndArrays(t *testing.T) {
	text := &protocol.Result{Output: "the quick brown fox"}
	if !Grade("includes", text, Options{"value": "quick"}).Passed {
		t.Fatal("substring containment should pass")
	}
	list := &protocol.Result{Output: []any{"a", "b"}}
	if !Grade("includes", list, Options{"value": "b"}).Passed {
		t.Fatal("array membership should pass")
	}
	if Grade("includes", list, Options{"value": "z"}).Passed {
		t.Fatal("absent member should fail")
	}
}

func TestMaximumCostIgnoresUnreportedCost(t *testing.T) {
	// A target that reports no cost cannot be over budget. Failing here would
	// grade the target's instrumentation rather than its behaviour.
	res := &protocol.Result{}
	if !Grade("maximum_cost", res, Options{"maximum_usd": 0.01}).Passed {
		t.Fatal("an unreported cost must not fail a cost ceiling")
	}
	cost := 0.5
	over := &protocol.Result{Usage: &protocol.Usage{CostUSD: &cost}}
	if Grade("maximum_cost", over, Options{"maximum_usd": 0.01}).Passed {
		t.Fatal("a reported cost over the ceiling must fail")
	}
}

func TestValidateRejectsUnknownTypeAndOptions(t *testing.T) {
	if err := Validate("state_matches", Options{}); err == nil {
		t.Fatal("a misspelled grader type must be rejected at load time")
	}
	if err := Validate("state_match", Options{"pth": "x", "value": 1}); err == nil {
		t.Fatal("a misspelled option must be rejected at load time")
	}
	if err := Validate("state_match", Options{"path": "x", "value": 1}); err != nil {
		t.Fatalf("a well-formed expectation must validate: %v", err)
	}
}

func TestJSONSchemaContainsAndNot(t *testing.T) {
	res := &protocol.Result{Output: []any{
		map[string]any{"title": "hvitvasking risk"},
		map[string]any{"title": "something else"},
	}}
	present := Options{"schema": map[string]any{
		"type":     "array",
		"contains": map[string]any{"type": "object", "properties": map[string]any{
			"title": map[string]any{"type": "string", "pattern": "(?i)hvitvasking"},
		}},
	}}
	if !Grade("json_schema", res, present).Passed {
		t.Fatal("contains must pass when at least one item matches")
	}

	absent := Options{"schema": map[string]any{
		"type": "array",
		"not": map[string]any{"contains": map[string]any{
			"type": "object", "properties": map[string]any{
				"title": map[string]any{"type": "string", "pattern": "(?i)nowhere"},
			},
		}},
	}}
	if !Grade("json_schema", res, absent).Passed {
		t.Fatal("not+contains must pass when no item matches")
	}
}

func TestSourceSelectsFinalState(t *testing.T) {
	// An agent whose answer is prose keeps its structured fields in
	// final_state. Without `source`, a schema check could only read the prose,
	// forcing every such target to duplicate its state into the output.
	res := &protocol.Result{
		Output:     "I've proposed adding face pulls.",
		FinalState: map[string]any{"action_types": []any{"add_exercise", "add_set"}},
	}
	opts := Options{
		"source": "final_state",
		"path":   "action_types",
		"schema": map[string]any{
			"type":     "array",
			"contains": map[string]any{"type": "string", "const": "add_exercise"},
		},
	}
	if got := Grade("json_schema", res, opts); !got.Passed {
		t.Fatalf("expected final_state to be readable by schema, got %q", got.Detail)
	}
	// The default must stay the output, so existing suites keep their meaning.
	delete(opts, "source")
	if Grade("json_schema", res, opts).Passed {
		t.Fatal("without source, the grader must read output, not final_state")
	}
}

func TestSourceIsValidatedAtLoadTime(t *testing.T) {
	err := Validate("includes", Options{"value": "x", "source": "finalstate"})
	if err == nil {
		t.Fatal("a misspelled source must be rejected at load time")
	}
	if err := Validate("includes", Options{"value": "x", "source": "final_state"}); err != nil {
		t.Fatalf("a valid source must be accepted: %v", err)
	}
}

func TestJSONSchemaRejectsUnsupportedKeyword(t *testing.T) {
	// A keyword nobody implements would silently check nothing, which is the
	// exact failure this guards against.
	err := Validate("json_schema", Options{"schema": map[string]any{"patern": "^x"}})
	if err == nil {
		t.Fatal("an unsupported schema keyword must be rejected at load time")
	}
}

func TestJSONSchemaRequiredAndTypes(t *testing.T) {
	res := &protocol.Result{Output: map[string]any{"name": "x"}}
	opts := Options{"schema": map[string]any{
		"type":     "object",
		"required": []any{"name", "missing"},
	}}
	if Grade("json_schema", res, opts).Passed {
		t.Fatal("a missing required property must fail")
	}
}
