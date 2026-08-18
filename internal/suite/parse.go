package suite

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/gronnbeck/habitat/internal/graders"
)

// Execution boundaries the engine is designed around. They exist to stop an
// accidental zero added to a repetition count from becoming an expensive run.
const (
	MaxRepetitions = 10
	MaxCases       = 200
)

// UnmarshalYAML splits an expectation into its type and that grader's own
// options, so each grader validates its own keys rather than the format
// needing to know every option that exists.
func (e *Expectation) UnmarshalYAML(node *yaml.Node) error {
	var raw map[string]any
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("expectation must be a mapping: %w", err)
	}
	typeValue, ok := raw["type"]
	if !ok {
		return errors.New("expectation is missing `type`")
	}
	name, ok := typeValue.(string)
	if !ok || name == "" {
		return errors.New("expectation `type` must be a non-empty string")
	}
	options := make(graders.Options, len(raw))
	for key, value := range raw {
		if key != "type" {
			options[key] = value
		}
	}
	e.Type = name
	e.Options = options
	return nil
}

// Load reads and validates one suite file.
func Load(path string) (*Suite, error) {
	file, err := os.Open(path) // #nosec G304 -- suite paths come from the operator
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var s Suite
	decoder := yaml.NewDecoder(file)
	// Unknown top-level or case-level keys are a mistake worth reporting: a
	// misspelled `repetitions` would otherwise silently keep the default.
	decoder.KnownFields(true)
	if err := decoder.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	s.Path = path
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return &s, nil
}

// LoadDir reads every suite in a directory, sorted by filename so runs and
// reports are ordered predictably.
func LoadDir(dir string) ([]*Suite, error) {
	paths, err := Discover(dir)
	if err != nil {
		return nil, err
	}
	suites := make([]*Suite, 0, len(paths))
	for _, path := range paths {
		s, err := Load(path)
		if err != nil {
			return nil, err
		}
		suites = append(suites, s)
	}
	return suites, nil
}

// Discover lists the suite files in a directory.
func Discover(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if ext := filepath.Ext(entry.Name()); ext == ".yml" || ext == ".yaml" {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// Validate checks everything that can be known without executing anything.
func (s *Suite) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("suite is missing `name`")
	}
	if strings.TrimSpace(s.Target) == "" {
		return errors.New("suite is missing `target`")
	}
	if len(s.Cases) == 0 {
		return errors.New("suite has no cases")
	}
	if len(s.Cases) > MaxCases {
		return fmt.Errorf("suite has %d cases, which exceeds the maximum of %d", len(s.Cases), MaxCases)
	}
	if err := s.validateCases(); err != nil {
		return err
	}
	return s.validatePolicy()
}

func (s *Suite) validateCases() error {
	seen := map[string]bool{}
	for i := range s.Cases {
		c := s.Cases[i]
		label := c.ID
		if label == "" {
			return fmt.Errorf("case %d is missing `id`", i+1)
		}
		if seen[label] {
			return fmt.Errorf("duplicate case id %q", label)
		}
		seen[label] = true
		if err := s.validateCase(c); err != nil {
			return fmt.Errorf("case %q: %w", label, err)
		}
	}
	return nil
}

func (s *Suite) validateCase(c Case) error {
	// A case with no expectations asserts nothing and would pass forever.
	if len(c.Expectations) == 0 {
		return errors.New("has no expectations")
	}
	reps := s.RepetitionsFor(c)
	if reps < 1 {
		return fmt.Errorf("repetitions must be at least 1, got %d", reps)
	}
	if reps > MaxRepetitions {
		return fmt.Errorf("repetitions %d exceeds the maximum of %d", reps, MaxRepetitions)
	}
	for i, expectation := range c.Expectations {
		if err := graders.Validate(expectation.Type, expectation.Options); err != nil {
			return fmt.Errorf("expectation %d (%s): %w", i+1, expectation.Type, err)
		}
	}
	return nil
}

func (s *Suite) validatePolicy() error {
	if s.Policy == nil {
		return nil
	}
	fractions := map[string]*float64{
		"minimum_pass_rate":  s.Policy.MinimumPassRate,
		"maximum_error_rate": s.Policy.MaximumErrorRate,
	}
	for name, value := range fractions {
		if value != nil && (*value < 0 || *value > 1) {
			return fmt.Errorf("policy %s must be between 0 and 1, got %v", name, *value)
		}
	}
	return nil
}

// Digest hashes everything that affects grading, deliberately excluding the
// prose fields. Editing a title or description to explain a case better must
// never invalidate its comparability with earlier runs.
func (s *Suite) Digest() string {
	type digestCase struct {
		ID           string         `json:"id"`
		Critical     bool           `json:"critical"`
		Input        map[string]any `json:"input"`
		Repetitions  int            `json:"repetitions"`
		Expectations []Expectation  `json:"expectations"`
	}
	payload := struct {
		Target string       `json:"target"`
		Cases  []digestCase `json:"cases"`
	}{Target: s.Target}

	for _, c := range s.Cases {
		payload.Cases = append(payload.Cases, digestCase{
			ID: c.ID, Critical: c.Critical, Input: c.Input,
			Repetitions: s.RepetitionsFor(c), Expectations: c.Expectations,
		})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// MarshalJSON keeps an expectation's type alongside its options so the digest
// changes when either does.
func (e Expectation) MarshalJSON() ([]byte, error) {
	flat := make(map[string]any, len(e.Options)+1)
	for key, value := range e.Options {
		flat[key] = value
	}
	flat["type"] = e.Type
	return json.Marshal(flat)
}
