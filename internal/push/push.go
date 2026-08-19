// Package push uploads a finished run to a habitat server.
//
// The run is already graded and its verdict already decided locally — the
// server stores what it is told. That is deliberate: grading exists in one
// place, so a run reads the same in the terminal and in the browser, and a
// server outage can never change whether a suite passed.
package push

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gronnbeck/habitat/internal/result"
	"github.com/gronnbeck/habitat/pkg/protocol"
)

// Result describes where an uploaded run can be read.
type Result struct {
	RunID string `json:"run_id"`
	URL   string `json:"url"`
}

// Run uploads a finished run. Token authenticates the project it belongs to.
func Run(server, token string, run result.Run) (Result, error) {
	if token == "" {
		return Result{}, fmt.Errorf("no token: set HABITAT_TOKEN to push to %s", server)
	}
	body, err := json.Marshal(run)
	if err != nil {
		return Result{}, err
	}
	endpoint := strings.TrimRight(server, "/") + "/" + protocol.Version + "/runs"

	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("reaching %s: %w", endpoint, err)
	}
	defer func() { _ = response.Body.Close() }()

	return decode(response, server)
}

func decode(response *http.Response, server string) (Result, error) {
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	if response.StatusCode == http.StatusUnauthorized {
		return Result{}, fmt.Errorf("%s rejected the token", server)
	}
	if response.StatusCode >= 300 {
		return Result{}, fmt.Errorf("%s returned %d: %s", server, response.StatusCode, strings.TrimSpace(string(payload)))
	}
	var stored Result
	if err := json.Unmarshal(payload, &stored); err != nil {
		return Result{}, nil // stored fine; we just cannot name the URL
	}
	if stored.URL != "" && strings.HasPrefix(stored.URL, "/") {
		stored.URL = strings.TrimRight(server, "/") + stored.URL
	}
	return stored, nil
}
