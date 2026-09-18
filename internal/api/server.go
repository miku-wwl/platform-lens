package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/miku-wwl/platform-lens/internal/app"
	"github.com/miku-wwl/platform-lens/internal/runs"
	"github.com/miku-wwl/platform-lens/internal/runtime"
)

type Server struct {
	Service   *app.Service
	Toolchain runtime.Toolchain
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /version", s.version)
	mux.HandleFunc("POST /analysis", s.create)
	mux.HandleFunc("GET /analysis/", s.get)
	return loggingMiddleware(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.Service.Artifacts.Ready(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
func (s *Server) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, runtime.BuildInfo(s.Toolchain.Hash))
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var request app.SubmitRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&request); err != nil || strings.TrimSpace(request.RepositoryURL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid analysis request"})
		return
	}
	run, err := s.Service.Submit(r.Context(), request)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	go func() { _, _ = s.Service.Process(context.Background(), run.RunID) }()
	writeJSON(w, http.StatusAccepted, map[string]any{"run_id": run.RunID, "state": run.State})
}
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimPrefix(r.URL.Path, "/analysis/")
	if runID == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	run, err := s.Service.Repository.GetRun(r.Context(), runID)
	if errors.Is(err, runs.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, run)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.ServeHTTP(w, r) })
}
