// Package store persists finished runs to SQLite.
//
// Persistence is not optional here: a shared, navigable history is the whole
// reason there is a server component, so every run lands in the store rather
// than only being printed and lost.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver: no cgo, so the binary stays static

	"github.com/gronnbeck/habitat/internal/result"
)

// DefaultPath is where a run persists when nothing else is configured.
const DefaultPath = ".habitat/habitat.db"

const schema = `
CREATE TABLE IF NOT EXISTS runs (
  id                TEXT PRIMARY KEY,
  suite_name        TEXT NOT NULL,
  suite_path        TEXT,
  target            TEXT NOT NULL,
  digest            TEXT,
  git_sha           TEXT,
  started_at        TEXT NOT NULL,
  finished_at       TEXT NOT NULL,
  pass_rate         REAL NOT NULL,
  status            TEXT NOT NULL,
  policy_violations TEXT
);
CREATE INDEX IF NOT EXISTS runs_suite_idx ON runs (suite_name, started_at DESC);

CREATE TABLE IF NOT EXISTS run_cases (
  run_id       TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  case_id      TEXT NOT NULL,
  title        TEXT,
  critical     INTEGER NOT NULL DEFAULT 0,
  tags         TEXT,
  passed       INTEGER NOT NULL,
  passed_count INTEGER NOT NULL,
  total        INTEGER NOT NULL,
  PRIMARY KEY (run_id, case_id)
);

-- Tenancy. A project owns its runs; its token is what a CLI authenticates with.
-- Local runs land in an implicit "local" project, so single-machine use needs
-- no setup and the schema has one shape everywhere.
CREATE TABLE IF NOT EXISTS projects (
  id         TEXT PRIMARY KEY,
  slug       TEXT NOT NULL UNIQUE,
  name       TEXT NOT NULL,
  token_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
  id            TEXT PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at    TEXT NOT NULL
);

-- Only the hash is stored, so a stolen database yields no usable session.
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS executions (
  run_id           TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  case_id          TEXT NOT NULL,
  repetition_index INTEGER NOT NULL,
  passed           INTEGER NOT NULL,
  output           TEXT,
  final_state      TEXT,
  events           TEXT,
  usage            TEXT,
  duration_ms      INTEGER NOT NULL DEFAULT 0,
  error            TEXT,
  grades           TEXT,
  PRIMARY KEY (run_id, case_id, repetition_index)
);
`

// Store is a SQLite-backed run history.
type Store struct{ db *sql.DB }

// Open connects to the store, creating the file and schema if needed.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// migrate applies changes that CREATE TABLE IF NOT EXISTS cannot, for stores
// written before a column existed. Each is idempotent: SQLite reports a
// duplicate column rather than a generic error, which is the signal that this
// migration has already run.
func migrate(db *sql.DB) error {
	for _, statement := range []string{
		`ALTER TABLE runs ADD COLUMN project_id TEXT`,
	} {
		if _, err := db.Exec(statement); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("migrating: %w", err)
		}
	}
	return nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// SaveRun writes a finished run and everything under it in one transaction,
// so a partially written run can never be browsed as if it were complete.
func (s *Store) SaveRun(run result.Run) error { return s.SaveRunFor(LocalProjectID, run) }

// SaveRunFor writes a finished run under a project.
func (s *Store) SaveRunFor(projectID string, run result.Run) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := insertRun(tx, run, projectID); err != nil {
		return err
	}
	for _, c := range run.Cases {
		if err := insertCase(tx, run.ID, c); err != nil {
			return err
		}
		for _, e := range c.Executions {
			if err := insertExecution(tx, run.ID, e); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func insertRun(tx *sql.Tx, run result.Run, projectID string) error {
	_, err := tx.Exec(
		`INSERT OR REPLACE INTO runs
		 (id, suite_name, suite_path, target, digest, git_sha, started_at, finished_at,
		  pass_rate, status, policy_violations, project_id)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		run.ID, run.SuiteName, run.SuitePath, run.Target, run.Digest, run.GitSHA,
		run.StartedAt.Format(time.RFC3339Nano), run.FinishedAt.Format(time.RFC3339Nano),
		run.PassRate(), run.Status, encode(run.PolicyViolations), projectID,
	)
	return err
}

func insertCase(tx *sql.Tx, runID string, c result.Case) error {
	_, err := tx.Exec(
		`INSERT OR REPLACE INTO run_cases
		 (run_id, case_id, title, critical, tags, passed, passed_count, total)
		 VALUES (?,?,?,?,?,?,?,?)`,
		runID, c.ID, c.Title, c.Critical, encode(c.Tags),
		c.Passed(), c.PassedCount(), len(c.Executions),
	)
	return err
}

func insertExecution(tx *sql.Tx, runID string, e result.Execution) error {
	_, err := tx.Exec(
		`INSERT OR REPLACE INTO executions
		 (run_id, case_id, repetition_index, passed, output, final_state, events,
		  usage, duration_ms, error, grades)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		runID, e.CaseID, e.RepetitionIndex, e.Passed(),
		encode(e.Result.Output), encode(e.Result.FinalState), encode(e.Result.Events),
		encode(e.Result.Usage), e.Result.DurationMS, encode(e.Result.Error), encode(e.Grades),
	)
	return err
}

// Summary is one row in a run listing.
type Summary struct {
	ID         string    `json:"id"`
	SuiteName  string    `json:"suite_name"`
	Target     string    `json:"target"`
	Status     string    `json:"status"`
	PassRate   float64   `json:"pass_rate"`
	GitSHA     string    `json:"git_sha,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// ListRuns returns the most recent runs, newest first. An empty suite name
// lists across every suite.
func (s *Store) ListRuns(suiteName string, limit int) ([]Summary, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT id, suite_name, target, status, pass_rate, COALESCE(git_sha,''), started_at, finished_at
	          FROM runs`
	args := []any{}
	if suiteName != "" {
		query += ` WHERE suite_name = ?`
		args = append(args, suiteName)
	}
	query += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanSummaries(rows)
}

func scanSummaries(rows *sql.Rows) ([]Summary, error) {
	var out []Summary
	for rows.Next() {
		var item Summary
		var started, finished string
		if err := rows.Scan(&item.ID, &item.SuiteName, &item.Target, &item.Status,
			&item.PassRate, &item.GitSHA, &started, &finished); err != nil {
			return nil, err
		}
		item.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
		item.FinishedAt, _ = time.Parse(time.RFC3339Nano, finished)
		out = append(out, item)
	}
	return out, rows.Err()
}

func encode(v any) string {
	if v == nil {
		return ""
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(encoded)
}
