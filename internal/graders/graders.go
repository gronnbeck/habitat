// Package graders implements the expectation types a case can carry.
//
// Every grader is checked independently against one execution's result, and
// reports only pass/fail plus a human-readable detail line. Graders never see
// the suite, the policy, or the other repetitions of their case — combining
// verdicts is the engine's job, not theirs.
package graders

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gronnbeck/habitat/pkg/protocol"
)

// Options are a grader's own keys from an expectation, minus "type".
type Options map[string]any

// Outcome is one grader's verdict on one execution.
type Outcome struct {
	Passed bool
	Detail string
}

// Grader is one expectation type. Validate runs at suite-load time so that a
// misspelled option fails before anything is executed, rather than silently
// checking less than the author believes it does.
type Grader struct {
	Name     string
	Validate func(Options) error
	Grade    func(*protocol.Result, Options) Outcome
}

var registry = map[string]Grader{}

// Register adds a grader. Panics on a duplicate name, which can only be a
// programming error at init time.
func Register(g Grader) {
	if _, exists := registry[g.Name]; exists {
		panic("habitat: grader registered twice: " + g.Name)
	}
	registry[g.Name] = g
}

// Lookup returns a registered grader by name.
func Lookup(name string) (Grader, bool) {
	g, ok := registry[name]
	return g, ok
}

// Names lists every registered grader, sorted, for error messages.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Validate checks one expectation's type and options. This is what makes a
// typo in a grader name or option a load-time error rather than a check that
// quietly never runs.
func Validate(name string, opts Options) error {
	g, ok := Lookup(name)
	if !ok {
		return fmt.Errorf("unknown expectation type %q (known types: %s)", name, strings.Join(Names(), ", "))
	}
	if g.Validate == nil {
		return nil
	}
	return g.Validate(opts)
}

// Grade runs one expectation against one result. An unknown type here would
// mean validation was skipped, so it fails rather than passing vacuously.
func Grade(name string, res *protocol.Result, opts Options) Outcome {
	g, ok := Lookup(name)
	if !ok {
		return Outcome{Passed: false, Detail: "unknown expectation type " + name}
	}
	return g.Grade(res, opts)
}

// allow reports an error for any option key the grader does not understand.
func (o Options) allow(keys ...string) error {
	permitted := map[string]bool{}
	for _, k := range keys {
		permitted[k] = true
	}
	var unknown []string
	for k := range o {
		if !permitted[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("unknown option(s) %s (accepted: %s)",
		strings.Join(unknown, ", "), strings.Join(keys, ", "))
}

// requirePresent checks that an option was supplied at all. It deliberately
// accepts a nil value: `value: null` is a legitimate thing to assert.
func (o Options) requirePresent(key string) error {
	if _, ok := o[key]; !ok {
		return fmt.Errorf("option %q is required", key)
	}
	return nil
}

func (o Options) str(key string) (string, bool) {
	s, ok := o[key].(string)
	return s, ok
}

func (o Options) number(key string) (float64, bool) {
	v, ok := o[key]
	if !ok {
		return 0, false
	}
	return toFloat(v)
}

// requireString demands a non-empty string option.
func (o Options) requireString(key string) error {
	s, ok := o.str(key)
	if !ok || s == "" {
		return fmt.Errorf("option %q must be a non-empty string", key)
	}
	return nil
}

// requireNumber demands a numeric option.
func (o Options) requireNumber(key string) error {
	if _, ok := o.number(key); !ok {
		return fmt.Errorf("option %q must be a number", key)
	}
	return nil
}
