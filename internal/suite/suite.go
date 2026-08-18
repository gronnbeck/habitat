// Package suite defines the evaluation suite format and its validation.
//
// This is the only place the suite format is implemented. No SDK, in any
// language, parses a suite file — SDKs receive already-expanded executions
// over the wire, so the format can change here without touching them.
package suite

// Suite is one named evaluation: a target bound to a list of cases.
type Suite struct {
	Name               string  `yaml:"name"`
	Target             string  `yaml:"target"`
	DefaultRepetitions int     `yaml:"default_repetitions"`
	Policy             *Policy `yaml:"policy"`
	Cases              []Case  `yaml:"cases"`

	// Path is where this suite was loaded from. Not part of the format.
	Path string `yaml:"-"`
}

// Case is one input paired with the expectations its result must satisfy.
type Case struct {
	ID          string         `yaml:"id"`
	Title       string         `yaml:"title"`
	Description string         `yaml:"description"`
	Critical    bool           `yaml:"critical"`
	Tags        []string       `yaml:"tags"`
	Input       map[string]any `yaml:"input"`
	Repetitions *int           `yaml:"repetitions"`
	Expectations []Expectation `yaml:"expectations"`
}

// Policy is the suite-level pass/fail rules layered on top of case results.
// Left unset, the default already requires every case to pass every
// repetition, so a policy block only ever adds constraints.
type Policy struct {
	MinimumPassRate          *float64 `yaml:"minimum_pass_rate"`
	MaximumErrorRate         *float64 `yaml:"maximum_error_rate"`
	CriticalCasesMustPass    bool     `yaml:"critical_cases_must_pass"`
	MaximumAverageCost       *float64 `yaml:"maximum_average_cost"`
	MaximumAverageDurationMS *float64 `yaml:"maximum_average_duration_ms"`
}

// Expectation is one grader config: a type plus that grader's own options.
// The options are kept as a raw map so each grader validates its own keys.
type Expectation struct {
	Type    string
	Options map[string]any
}

// RepetitionsFor returns how many attempts a case gets, applying the suite
// default and then the engine's fallback of one.
func (s *Suite) RepetitionsFor(c Case) int {
	if c.Repetitions != nil {
		return *c.Repetitions
	}
	if s.DefaultRepetitions > 0 {
		return s.DefaultRepetitions
	}
	return 1
}

// EffectivePolicy returns the suite's policy, or the implicit default that
// requires every case to pass every repetition.
func (s *Suite) EffectivePolicy() Policy {
	if s.Policy != nil {
		return *s.Policy
	}
	full := 1.0
	return Policy{MinimumPassRate: &full}
}

// Case looks up a case by id.
func (s *Suite) Case(id string) (Case, bool) {
	for _, c := range s.Cases {
		if c.ID == id {
			return c, true
		}
	}
	return Case{}, false
}
