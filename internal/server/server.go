// Package server is `habitat serve`: the browsable run history.
//
// It reuses the same store and result types a local run writes, so a run
// looks the same whether it is printed in a terminal or opened in a browser.
package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/gronnbeck/habitat/internal/result"
	"github.com/gronnbeck/habitat/internal/store"
	"github.com/gronnbeck/habitat/pkg/protocol"
)

//go:embed web/*.html
var files embed.FS

// Server serves the dashboard and read APIs over a store.
type Server struct {
	store     *store.Store
	templates *template.Template
}

// New builds a server over an open store.
func New(db *store.Store) (*Server, error) {
	templates, err := template.New("").Funcs(helpers()).ParseFS(files, "web/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{store: db, templates: templates}, nil
}

func helpers() template.FuncMap {
	return template.FuncMap{
		"percent": func(rate float64) string { return fmt.Sprintf("%.0f%%", rate*100) },
		"short": func(s string) string {
			if len(s) > 8 {
				return s[:8]
			}
			return s
		},
		"pretty": func(v any) string {
			encoded, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				return ""
			}
			return string(encoded)
		},
	}
}

// Handler builds the routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /runs/{id}", s.handleRun)

	base := "/" + protocol.Version
	mux.HandleFunc("GET "+base+"/runs", s.handleAPIRuns)
	mux.HandleFunc("GET "+base+"/runs/{id}", s.handleAPIRun)
	mux.HandleFunc("GET "+base+"/suites", s.handleAPISuites)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	suiteName := r.URL.Query().Get("suite")
	runs, err := s.store.ListRuns(suiteName, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	suites, err := s.store.ListSuiteNames()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "index.html", map[string]any{
		"Runs": runs, "Suites": suites, "Selected": suiteName,
	})
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetRun(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}
	s.render(w, "run.html", map[string]any{"Run": run, "Cases": run.Cases})
}

func (s *Server) handleAPIRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.ListRuns(r.URL.Query().Get("suite"), 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, runs)
}

func (s *Server) handleAPIRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetRun(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}
	writeJSON(w, run)
}

func (s *Server) handleAPISuites(w http.ResponseWriter, _ *http.Request) {
	names, err := s.store.ListSuiteNames()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, names)
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func statusFor(err error) int {
	if strings.Contains(err.Error(), store.ErrNotFound.Error()) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// StatusClass maps a run status to a CSS class, so templates do not branch.
func StatusClass(run result.Run) string {
	switch run.Status {
	case result.StatusPassed:
		return "pass"
	case result.StatusFailed:
		return "fail"
	default:
		return "error"
	}
}
