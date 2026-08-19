package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gronnbeck/habitat/internal/result"
	"github.com/gronnbeck/habitat/internal/store"
)

// maxRunBytes bounds an uploaded run. A run carries every execution's output,
// so it is not small, but it is not unbounded either.
const maxRunBytes = 32 << 20 // 32 MiB

// handleIngest accepts a finished, already-graded run from a CLI.
//
// The engine does the grading locally and pushes the verdict — the server
// never re-grades. That keeps one implementation of grading, and means a run
// reads the same in the terminal and in the browser.
func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectFromToken(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var run result.Run
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRunBytes))
	if err := decoder.Decode(&run); err != nil {
		http.Error(w, "invalid run: "+err.Error(), http.StatusBadRequest)
		return
	}
	if run.ID == "" || run.SuiteName == "" {
		http.Error(w, "run is missing id or suite_name", http.StatusBadRequest)
		return
	}

	if err := s.store.SaveRunFor(project.ID, run); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"status": "stored",
		"run_id": run.ID,
		"url":    "/projects/" + project.Slug + "/runs/" + run.ID,
	})
}

// projectFromToken authenticates a machine. The local project is excluded on
// purpose: it exists so single-machine runs have an owner, and nothing should
// be able to write to it over the network.
func (s *Server) projectFromToken(r *http.Request) (store.Project, error) {
	header := r.Header.Get("Authorization")
	token, found := strings.CutPrefix(header, "Bearer ")
	if !found || token == "" {
		return store.Project{}, errors.New("missing bearer token")
	}
	project, err := s.store.ProjectByToken(token)
	if err != nil {
		return store.Project{}, err
	}
	if project.ID == store.LocalProjectID {
		return store.Project{}, errors.New("the local project cannot be written to remotely")
	}
	return project, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
