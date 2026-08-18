package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gronnbeck/habitat/internal/result"
)

// ErrNotFound is returned when a run id matches nothing.
var ErrNotFound = errors.New("run not found")

// GetRun reads one persisted run back into the same shape the engine
// produced, so `show` and a fresh run render identically.
func (s *Store) GetRun(id string) (result.Run, error) {
	run, err := s.scanRun(id)
	if err != nil {
		return result.Run{}, err
	}
	cases, err := s.readCases(id)
	if err != nil {
		return result.Run{}, err
	}
	executions, err := s.readExecutions(id)
	if err != nil {
		return result.Run{}, err
	}
	for i := range cases {
		cases[i].Executions = executions[cases[i].ID]
	}
	run.Cases = cases
	return run, nil
}

func (s *Store) scanRun(id string) (result.Run, error) {
	row := s.db.QueryRow(
		`SELECT id, suite_name, COALESCE(suite_path,''), target, COALESCE(digest,''),
		        COALESCE(git_sha,''), started_at, finished_at, status, COALESCE(policy_violations,'')
		 FROM runs WHERE id = ?`, id)

	var run result.Run
	var started, finished, violations string
	err := row.Scan(&run.ID, &run.SuiteName, &run.SuitePath, &run.Target, &run.Digest,
		&run.GitSHA, &started, &finished, &run.Status, &violations)
	if errors.Is(err, sql.ErrNoRows) {
		return result.Run{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return result.Run{}, err
	}
	run.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
	run.FinishedAt, _ = time.Parse(time.RFC3339Nano, finished)
	decode(violations, &run.PolicyViolations)
	return run, nil
}

func (s *Store) readCases(runID string) ([]result.Case, error) {
	rows, err := s.db.Query(
		`SELECT case_id, COALESCE(title,''), critical, COALESCE(tags,'')
		 FROM run_cases WHERE run_id = ? ORDER BY rowid`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var cases []result.Case
	for rows.Next() {
		var c result.Case
		var tags string
		if err := rows.Scan(&c.ID, &c.Title, &c.Critical, &tags); err != nil {
			return nil, err
		}
		decode(tags, &c.Tags)
		cases = append(cases, c)
	}
	return cases, rows.Err()
}

func (s *Store) readExecutions(runID string) (map[string][]result.Execution, error) {
	rows, err := s.db.Query(
		`SELECT case_id, repetition_index, COALESCE(output,''), COALESCE(final_state,''),
		        COALESCE(events,''), COALESCE(usage,''), duration_ms, COALESCE(error,''),
		        COALESCE(grades,'')
		 FROM executions WHERE run_id = ? ORDER BY case_id, repetition_index`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	byCase := map[string][]result.Execution{}
	for rows.Next() {
		e, caseID, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		byCase[caseID] = append(byCase[caseID], e)
	}
	return byCase, rows.Err()
}

func scanExecution(rows *sql.Rows) (result.Execution, string, error) {
	var e result.Execution
	var caseID, output, finalState, events, usage, execError, grades string
	err := rows.Scan(&caseID, &e.RepetitionIndex, &output, &finalState,
		&events, &usage, &e.Result.DurationMS, &execError, &grades)
	if err != nil {
		return e, "", err
	}
	e.CaseID = caseID
	decode(output, &e.Result.Output)
	decode(finalState, &e.Result.FinalState)
	decode(events, &e.Result.Events)
	decode(usage, &e.Result.Usage)
	decode(execError, &e.Result.Error)
	decode(grades, &e.Grades)
	return e, caseID, nil
}

// ListSuiteNames returns every suite that has at least one persisted run.
func (s *Store) ListSuiteNames() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT suite_name FROM runs ORDER BY suite_name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func decode(encoded string, target any) {
	if encoded == "" || encoded == "null" {
		return
	}
	_ = json.Unmarshal([]byte(encoded), target)
}
