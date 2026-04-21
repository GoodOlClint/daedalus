package core

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/GoodOlClint/daedalus/minos/storage"
	"github.com/GoodOlClint/daedalus/pkg/audit"
)

// routes builds the HTTP handler for Minos's Slice A API.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("POST /tasks", s.requireAdmin(http.HandlerFunc(s.handleCreateTask)))
	mux.Handle("GET /tasks", s.requireAdmin(http.HandlerFunc(s.handleListTasks)))
	mux.Handle("GET /tasks/{id}", s.requireAdmin(http.HandlerFunc(s.handleGetTask)))
	return s.auditMiddleware(mux)
}

// auditMiddleware emits one event per request outcome.
func (s *Server) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.audit.Emit(audit.Event{
			Category: "http",
			Outcome:  outcomeFor(rec.status),
			Fields: map[string]string{
				"method": r.Method,
				"path":   r.URL.Path,
				"status": strconv.Itoa(rec.status),
			},
		})
	})
}

// requireAdmin gates operator-only endpoints behind a bearer token resolved
// via the configured secret provider. Phase 1 posture per
// architecture.md §6 MCP Broker Authentication: shared-secret bearer over
// the trusted Crete bridge; Phase 2 swaps to JWT.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == "" || got == r.Header.Get("Authorization") {
			writeError(w, http.StatusUnauthorized, "missing or malformed bearer")
			return
		}
		want, err := s.provider.Resolve(r.Context(), s.cfg.AdminTokenRef)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "resolve admin token")
			return
		}
		if subtle.ConstantTimeCompare([]byte(got), want.Data) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid bearer")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req CommissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}
	task, err := s.Commission(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, taskResponse(task))
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	var states []storage.State
	if raw := r.URL.Query().Get("state"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			states = append(states, storage.State(strings.TrimSpace(part)))
		}
	}
	limit := 0
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
	out := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, taskResponse(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	task, err := s.store.GetTask(r.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, taskResponse(task))
}

// taskResponse is the JSON shape the API returns for task records.
func taskResponse(t *storage.Task) map[string]any {
	out := map[string]any{
		"id":         t.ID,
		"project_id": t.ProjectID,
		"task_type":  t.TaskType,
		"backend":    t.Backend,
		"state":      t.State,
		"created_at": t.CreatedAt,
	}
	if t.ParentID != nil {
		out["parent_id"] = *t.ParentID
	}
	if t.StartedAt != nil {
		out["started_at"] = *t.StartedAt
	}
	if t.FinishedAt != nil {
		out["finished_at"] = *t.FinishedAt
	}
	if t.RunID != nil {
		out["run_id"] = *t.RunID
	}
	if t.PodName != nil {
		out["pod_name"] = *t.PodName
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func outcomeFor(status int) string {
	switch {
	case status >= 500:
		return "server-error"
	case status >= 400:
		return "client-error"
	default:
		return "ok"
	}
}

// statusRecorder wraps an http.ResponseWriter to capture the status code
// for audit logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
