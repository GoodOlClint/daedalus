package core

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/zakros-hq/zakros/minos/storage"
)

// handleStateTasks returns all tasks, optionally filtered by ?state=<csv>
// and capped by ?limit=<n>. Default limit when unset is 50; pass limit=0
// for unbounded. Mirrors handleListTasks's shape so Iris's tool layer can
// consume it the same way the operator CLI does.
func (s *Server) handleStateTasks(w http.ResponseWriter, r *http.Request) {
	var states []storage.State
	if raw := r.URL.Query().Get("state"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			states = append(states, storage.State(p))
		}
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = n
	}
	tasks, err := s.store.ListTasks(r.Context(), states, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, taskListResponse(tasks))
}

// handleStateQueue returns tasks in StateQueued, newest first. Convenience
// over /state/tasks?state=queued for Iris's "what's queued?" intent.
func (s *Server) handleStateQueue(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.store.ListTasks(r.Context(), []storage.State{storage.StateQueued}, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, taskListResponse(tasks))
}

// handleStateRecent returns recently terminal (completed | failed) tasks,
// newest first. Default 20; ?limit=<n> overrides. Convenience over
// /state/tasks?state=completed,failed for Iris's "what just finished?".
func (s *Server) handleStateRecent(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = n
	}
	tasks, err := s.store.ListTasks(r.Context(),
		[]storage.State{storage.StateCompleted, storage.StateFailed}, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, taskListResponse(tasks))
}

// taskListResponse renders a slice of tasks into the same JSON shape as
// handleListTasks. Lifted out so all three state endpoints share it.
func taskListResponse(tasks []*storage.Task) []map[string]any {
	out := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		entry := taskResponse(t)
		// Surface the brief summary so Iris doesn't need a follow-up GET to
		// render "what's running?" — the brief is the natural answer.
		if t.Envelope != nil && t.Envelope.Brief.Summary != "" {
			entry["summary"] = t.Envelope.Brief.Summary
		}
		out = append(out, entry)
	}
	return out
}
