package suite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const valid = `
name: example
target: agent
default_repetitions: 2
cases:
  - id: a case
    input: { message: hi }
    expectations:
      - type: no_error
`

func TestLoadValidSuite(t *testing.T) {
	s, err := Load(write(t, valid))
	if err != nil {
		t.Fatalf("expected the suite to load: %v", err)
	}
	if s.RepetitionsFor(s.Cases[0]) != 2 {
		t.Fatal("case should inherit the suite's default repetitions")
	}
}

func TestCaseWithoutExpectationsIsRejected(t *testing.T) {
	// A case that asserts nothing would pass forever, which is worse than
	// having no case at all.
	_, err := Load(write(t, `
name: example
target: agent
cases:
  - id: asserts nothing
    input: { a: 1 }
    expectations: []
`))
	if err == nil || !strings.Contains(err.Error(), "no expectations") {
		t.Fatalf("expected a no-expectations error, got %v", err)
	}
}

func TestDuplicateCaseIDsAreRejected(t *testing.T) {
	_, err := Load(write(t, `
name: example
target: agent
cases:
  - id: same
    input: { a: 1 }
    expectations: [{ type: no_error }]
  - id: same
    input: { a: 2 }
    expectations: [{ type: no_error }]
`))
	if err == nil || !strings.Contains(err.Error(), "duplicate case id") {
		t.Fatalf("expected a duplicate id error, got %v", err)
	}
}

func TestUnknownFieldIsRejected(t *testing.T) {
	// A misspelled `repetitions` would otherwise silently keep the default.
	_, err := Load(write(t, `
name: example
target: agent
cases:
  - id: a case
    input: { a: 1 }
    repetitons: 5
    expectations: [{ type: no_error }]
`))
	if err == nil {
		t.Fatal("expected a misspelled field to be rejected")
	}
}

func TestRepetitionCeilingIsEnforced(t *testing.T) {
	_, err := Load(write(t, `
name: example
target: agent
cases:
  - id: a case
    input: { a: 1 }
    repetitions: 99
    expectations: [{ type: no_error }]
`))
	if err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("expected the repetition ceiling to be enforced, got %v", err)
	}
}

func TestDigestIgnoresProseButTracksGrading(t *testing.T) {
	base, err := Load(write(t, valid))
	if err != nil {
		t.Fatal(err)
	}
	// Editing a description to explain a case better must never invalidate
	// comparability with earlier runs.
	prose, err := Load(write(t, strings.Replace(valid,
		"  - id: a case", "  - id: a case\n    description: explains the case", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if base.Digest() != prose.Digest() {
		t.Fatal("editing prose must not change the digest")
	}

	graded, err := Load(write(t, strings.Replace(valid,
		"      - type: no_error", "      - type: no_error\n      - type: includes\n        value: hi", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if base.Digest() == graded.Digest() {
		t.Fatal("changing an expectation must change the digest")
	}
}

func TestEffectivePolicyDefaultsToEverythingPassing(t *testing.T) {
	s, err := Load(write(t, valid))
	if err != nil {
		t.Fatal(err)
	}
	p := s.EffectivePolicy()
	if p.MinimumPassRate == nil || *p.MinimumPassRate != 1.0 {
		t.Fatal("the default policy must require every case to pass")
	}
}
