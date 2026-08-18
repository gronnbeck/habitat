// Package ingest is the HTTP surface an SDK talks to during a run.
//
// It is deliberately tiny: hand out the plan, collect results, notice when
// the runner is finished. It holds no grading logic, so an SDK can never
// learn what makes a case pass — only what to execute.
package ingest

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/gronnbeck/habitat/pkg/protocol"
)

// Server collects one run's results from a runner process.
type Server struct {
	plan  protocol.Plan
	token string

	mu          sync.Mutex
	submissions map[string]protocol.Result
	completed   bool

	done     chan struct{}
	doneOnce sync.Once
}

// New builds a server for one plan. The token is a per-run shared secret so
// a stray process on the same machine cannot post into this run.
func New(plan protocol.Plan, token string) *Server {
	return &Server{
		plan:        plan,
		token:       token,
		submissions: make(map[string]protocol.Result, len(plan.Executions)),
		done:        make(chan struct{}),
	}
}

// Key identifies one execution within a run.
func Key(caseID string, repetition int) string {
	return fmt.Sprintf("%s#%d", caseID, repetition)
}

// Handler builds the routes. Everything is versioned so a mismatched SDK
// fails loudly rather than silently posting into nothing.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	base := "/" + protocol.Version
	mux.HandleFunc("GET "+base+"/plan", s.authed(s.handlePlan))
	mux.HandleFunc("POST "+base+"/executions", s.authed(s.handleSubmit))
	mux.HandleFunc("POST "+base+"/complete", s.authed(s.handleComplete))
	return mux
}

// Listen starts the server on an ephemeral loopback port and returns its URL.
// Loopback only: a run's ingest surface is never something to expose.
func (s *Server) Listen() (string, func() error, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	server := &http.Server{Handler: s.Handler()} // #nosec G112 -- loopback, run-scoped
	go func() { _ = server.Serve(listener) }()
	url := fmt.Sprintf("http://%s", listener.Addr().String())
	return url, server.Close, nil
}

func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+s.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handlePlan(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.plan)
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	var submission protocol.Submission
	if err := json.NewDecoder(r.Body).Decode(&submission); err != nil {
		http.Error(w, "invalid submission: "+err.Error(), http.StatusBadRequest)
		return
	}
	if submission.CaseID == "" {
		http.Error(w, "submission is missing case_id", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.submissions[Key(submission.CaseID, submission.RepetitionIndex)] = submission.Result
	complete := len(s.submissions) >= len(s.plan.Executions)
	s.mu.Unlock()

	if complete {
		s.finish()
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "recorded"})
}

func (s *Server) handleComplete(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.completed = true
	s.mu.Unlock()
	s.finish()
	writeJSON(w, http.StatusOK, map[string]string{"status": "complete"})
}

func (s *Server) finish() {
	s.doneOnce.Do(func() { close(s.done) })
}

// Done closes once every planned execution has been submitted, or the runner
// has declared itself finished.
func (s *Server) Done() <-chan struct{} { return s.done }

// Submissions returns a copy of what has been received so far.
func (s *Server) Submissions() map[string]protocol.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]protocol.Result, len(s.submissions))
	for key, value := range s.submissions {
		out[key] = value
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
