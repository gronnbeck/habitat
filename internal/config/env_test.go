package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnv(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, EnvFilename), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// unset clears a variable for the duration of one test and puts back whatever
// was there, so these tests cannot leak state into each other.
func unset(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		previous, had := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(key, previous)
				return
			}
			_ = os.Unsetenv(key)
		})
	}
}

func TestLoadEnvFileReadsAssignments(t *testing.T) {
	dir := writeEnv(t, `
# the project's token
HABITAT_TOKEN=hbt_abc123

export HABITAT_QUOTED="quoted value"
HABITAT_SINGLE='single'
HABITAT_SPACED  =  spaced
not an assignment
`)
	unset(t, "HABITAT_TOKEN", "HABITAT_QUOTED", "HABITAT_SINGLE", "HABITAT_SPACED")

	if err := loadEnvFile(dir); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"HABITAT_TOKEN":  "hbt_abc123",
		"HABITAT_QUOTED": "quoted value",
		"HABITAT_SINGLE": "single",
		"HABITAT_SPACED": "spaced",
	} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestExistingEnvironmentWins(t *testing.T) {
	// Overriding for a one-off run must work: `HABITAT_TOKEN=x habitat run`
	// should beat the file, not be silently discarded by it.
	dir := writeEnv(t, "HABITAT_TOKEN=from_file\n")
	t.Setenv("HABITAT_TOKEN", "from_environment")

	if err := loadEnvFile(dir); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("HABITAT_TOKEN"); got != "from_environment" {
		t.Fatalf("the environment must win, got %q", got)
	}
}

func TestMissingEnvFileIsFine(t *testing.T) {
	// Most projects will not have one, so its absence cannot be an error.
	if err := loadEnvFile(t.TempDir()); err != nil {
		t.Fatalf("a missing file should be silently accepted: %v", err)
	}
}

func TestMalformedLineIsReported(t *testing.T) {
	dir := writeEnv(t, "=novalue\n")
	if err := loadEnvFile(dir); err == nil {
		t.Fatal("a line with no variable name should be reported, not skipped")
	}
}

func TestLoadPicksUpTheEnvFile(t *testing.T) {
	dir := writeEnv(t, "HABITAT_TOKEN=via_load\n")
	unset(t, "HABITAT_TOKEN")

	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("HABITAT_TOKEN"); got != "via_load" {
		t.Fatalf("config.Load should load the env file, got %q", got)
	}
}
