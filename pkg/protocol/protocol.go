// Package protocol defines the JSON wire types shared between the habitat
// engine and every language SDK. It is the whole contract an SDK implements:
// fetch a list of executions, run each one, post back a Result.
//
// Nothing here knows what a grader or a policy is. An SDK reports what
// happened; the engine decides what it means.
package protocol

// Version is the wire protocol version. It prefixes every route ("/v1/...")
// so an SDK built against an older engine fails loudly rather than subtly.
const Version = "v1"

// Execution is one attempt at one case: the unit of work an SDK performs.
// RepetitionIndex is 0-based and distinguishes repeated attempts at the same
// case, which a non-deterministic target may not answer identically.
type Execution struct {
	CaseID          string         `json:"case_id"`
	RepetitionIndex int            `json:"repetition_index"`
	Input           map[string]any `json:"input"`
}

// Plan is what an SDK receives when it asks what to run. Target names the
// key the SDK should look up in its own registry.
type Plan struct {
	RunID      string      `json:"run_id"`
	Suite      string      `json:"suite"`
	Target     string      `json:"target"`
	Executions []Execution `json:"executions"`
}

// Result is what a target reports for one execution. Every field is optional:
// a target that only produces text fills Output and nothing else.
//
// Output is what content-shaped graders read. FinalState is a flat snapshot
// of structured fields, and is what state_match reads — targets should expose
// the fields they intend to grade there rather than relying on prose.
type Result struct {
	Output     any            `json:"output"`
	FinalState map[string]any `json:"final_state"`
	Events     []Event        `json:"events"`
	Usage      *Usage         `json:"usage"`
	DurationMS int64          `json:"duration_ms"`
	Error      *Error         `json:"error"`
}

// Event is one entry in a target's execution trace, for tool-calling targets.
type Event struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Input  any    `json:"input,omitempty"`
	Output any    `json:"output,omitempty"`
}

// Usage carries what an execution consumed. Every field is a pointer so that
// "no cost was reported" stays distinguishable from "the cost was zero" —
// reports omit cost entirely rather than printing a misleading $0.00.
type Usage struct {
	CostUSD      *float64 `json:"cost_usd,omitempty"`
	InputTokens  *int     `json:"input_tokens,omitempty"`
	OutputTokens *int     `json:"output_tokens,omitempty"`
}

// Error is a target failure. It is not a grading verdict: a case may well
// expect an error, so only the no_error grader treats this as a failure.
type Error struct {
	Type    string `json:"type"`
	Class   string `json:"class"`
	Message string `json:"message"`
}

// Failed reports whether the execution errored.
func (r *Result) Failed() bool { return r != nil && r.Error != nil }

// Cost returns the reported cost and whether one was reported at all.
func (r *Result) Cost() (float64, bool) {
	if r == nil || r.Usage == nil || r.Usage.CostUSD == nil {
		return 0, false
	}
	return *r.Usage.CostUSD, true
}

// Submission is the body an SDK posts back for one execution.
type Submission struct {
	CaseID          string `json:"case_id"`
	RepetitionIndex int    `json:"repetition_index"`
	Result          Result `json:"result"`
}
