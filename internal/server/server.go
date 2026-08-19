// Package server is habitat's web side: the browsable, multi-project run
// history and the HTML report for a single run.
//
// It serves two audiences over one store. A CLI authenticates with a project
// token and pushes finished runs; a person authenticates with a session cookie
// and reads them. Neither can do the other's job.
package server

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strings"

	"github.com/gronnbeck/habitat/internal/result"
	"github.com/gronnbeck/habitat/internal/store"
	"github.com/gronnbeck/habitat/pkg/protocol"
)

//go:embed web/*.html
var files embed.FS

// Config controls how the server presents itself.
type Config struct {
	// RequireAuth gates every human-facing page behind a login.
	RequireAuth bool
	// SecureCookies marks the session cookie Secure. On behind TLS.
	SecureCookies bool
}

// Server serves the dashboard, the reports, and the ingest API.
type Server struct {
	store     *store.Store
	templates *template.Template
	cfg       Config
}

// New builds a server over an open store.
func New(db *store.Store, cfg Config) (*Server, error) {
	templates, err := template.New("").Funcs(helpers()).ParseFS(files, "web/*.html")
	if err != nil {
		return nil, err
	}
	if err := db.EnsureLocalProject(); err != nil {
		return nil, fmt.Errorf("preparing local project: %w", err)
	}
	return &Server{store: db, templates: templates, cfg: cfg}, nil
}

// IsLoopback reports whether an address is local-only. A server bound
// anywhere else is reachable by other people, which is what makes running it
// without authentication a mistake rather than a preference.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return false
	case "localhost":
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
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

	// Unauthenticated: the proxy's health check and the login form itself.
	mux.HandleFunc("GET /up", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)

	// Machine ingest: a project token, never a session.
	mux.HandleFunc("POST /"+protocol.Version+"/runs", s.handleIngest)

	// Human pages.
	mux.HandleFunc("GET /{$}", s.guard(s.handleIndex))
	mux.HandleFunc("GET /projects/{slug}", s.guard(s.handleProject))
	mux.HandleFunc("GET /projects/{slug}/runs/{id}", s.guard(s.handleRun))

	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	projects, err := s.store.ListProjects()
	if err != nil {
		s.fail(w, err)
		return
	}
	type row struct {
		store.Project
		Latest *store.Summary
	}
	rows := make([]row, 0, len(projects))
	for _, p := range projects {
		runs, err := s.store.ListRunsFor(p.ID, "", 1)
		if err != nil {
			s.fail(w, err)
			return
		}
		item := row{Project: p}
		if len(runs) > 0 {
			item.Latest = &runs[0]
		}
		rows = append(rows, item)
	}
	s.render(w, r, "projects.html", map[string]any{"Projects": rows})
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.ProjectBySlug(r.PathValue("slug"))
	if errors.Is(err, store.ErrNoProject) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	suite := r.URL.Query().Get("suite")
	runs, err := s.store.ListRunsFor(project.ID, suite, 100)
	if err != nil {
		s.fail(w, err)
		return
	}
	suites, err := s.store.SuiteNamesFor(project.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, "project.html", map[string]any{
		"Project": project, "Runs": runs, "Suites": suites, "Selected": suite,
	})
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.ProjectBySlug(r.PathValue("slug"))
	if errors.Is(err, store.ErrNoProject) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	runID := r.PathValue("id")

	// Check ownership before reading: a run id from another project must be a
	// 404 here, not a readable report.
	owned, err := s.store.RunBelongsTo(project.ID, runID)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !owned {
		http.NotFound(w, r)
		return
	}
	run, err := s.store.GetRun(runID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, "run.html", map[string]any{
		"Project": project, "Run": run, "Cases": run.Cases,
	})
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["User"] = userFrom(r.Context())
	data["RequireAuth"] = s.cfg.RequireAuth
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		s.fail(w, err)
	}
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
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
