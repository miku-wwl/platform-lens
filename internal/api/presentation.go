package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/report"
	"github.com/miku-wwl/platform-lens/internal/runs"
)

const maxPresentationArtifactBytes = 4 * 1024 * 1024

type RunView struct {
	RunID            string                   `json:"run_id"`
	RepositoryURL    string                   `json:"repository_url"`
	RequestedRef     string                   `json:"requested_ref"`
	ResolvedRef      string                   `json:"resolved_ref,omitempty"`
	RefType          domain.RefType           `json:"ref_type,omitempty"`
	CommitOID        string                   `json:"commit_oid,omitempty"`
	RequestedPath    string                   `json:"requested_path,omitempty"`
	State            domain.RunState          `json:"state"`
	AttemptNo        int                      `json:"attempt_no"`
	LeaseActive      bool                     `json:"lease_active"`
	AnalysisOutcome  *domain.AnalysisOutcome  `json:"analysis_outcome,omitempty"`
	CoverageStatus   *domain.CoverageStatus   `json:"coverage_status,omitempty"`
	ReviewStatus     *domain.ReviewStatus     `json:"review_status,omitempty"`
	EvaluationStatus *domain.EvaluationStatus `json:"evaluation_status,omitempty"`
	ManifestURI      string                   `json:"manifest_uri,omitempty"`
	ManifestHash     string                   `json:"manifest_hash,omitempty"`
	WinningAttempt   *int                     `json:"winning_attempt,omitempty"`
	FailureCode      string                   `json:"failure_code,omitempty"`
	FailureMessage   string                   `json:"failure_message,omitempty"`
	CreatedAt        string                   `json:"created_at"`
	UpdatedAt        string                   `json:"updated_at"`
}

type RunListResponse struct {
	Runs    []RunView `json:"runs"`
	Limit   int       `json:"limit"`
	Offset  int       `json:"offset"`
	HasMore bool      `json:"has_more"`
}

type ArtifactSummary struct {
	Name        string `json:"name"`
	SHA256      string `json:"sha256"`
	Size        int    `json:"size"`
	Sensitivity string `json:"sensitivity"`
}

type ArtifactListResponse struct {
	RunID     string            `json:"run_id"`
	AttemptNo int               `json:"attempt_no"`
	Artifacts []ArtifactSummary `json:"artifacts"`
}

type artifactCatalog struct {
	ManifestURI string
	Manifest    report.Manifest
	Bytes       []byte
	Entries     map[string]report.Artifact
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	lister, ok := s.Service.Repository.(runs.Lister)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "run listing is unavailable"})
		return
	}
	options, err := listOptions(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	result, err := lister.ListRuns(r.Context(), options)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "run listing failed"})
		return
	}
	views := make([]RunView, 0, len(result.Runs))
	for _, run := range result.Runs {
		views = append(views, viewOf(run))
	}
	writeJSON(w, http.StatusOK, RunListResponse{Runs: views, Limit: options.Limit, Offset: options.Offset, HasMore: result.HasMore})
}

func listOptions(r *http.Request) (runs.ListOptions, error) {
	options := runs.ListOptions{Limit: runs.DefaultListLimit}
	query := r.URL.Query()
	if value := query.Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return runs.ListOptions{}, errors.New("limit must be an integer")
		}
		options.Limit = parsed
	}
	if value := query.Get("offset"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return runs.ListOptions{}, errors.New("offset must be an integer")
		}
		options.Offset = parsed
	}
	if value := strings.TrimSpace(query.Get("state")); value != "" {
		options.State = domain.RunState(value)
		switch options.State {
		case domain.StateQueued, domain.StateClaimed, domain.StateRetrieving, domain.StateValidating, domain.StateReviewing, domain.StateEvaluating, domain.StatePersisting, domain.StateCompleted, domain.StateFailed:
		default:
			return runs.ListOptions{}, fmt.Errorf("unsupported state %q", value)
		}
	}
	options.Repository = strings.TrimSpace(query.Get("repository"))
	return runs.NormalizeListOptions(options), nil
}

func viewOf(run domain.AnalysisRun) RunView {
	failureMessage := strings.TrimSpace(run.FailureMessage)
	if len(failureMessage) > 1024 {
		failureMessage = failureMessage[:1024]
	}
	return RunView{
		RunID: run.RunID, RepositoryURL: run.RepositoryURL, RequestedRef: run.RequestedRef,
		ResolvedRef: run.ResolvedRef, RefType: run.RefType, CommitOID: run.CommitOID,
		RequestedPath: run.RequestedPath, State: run.State, AttemptNo: run.AttemptNo,
		LeaseActive: run.State.Active() && run.LeaseOwner != "", AnalysisOutcome: run.AnalysisOutcome,
		CoverageStatus: run.CoverageStatus, ReviewStatus: run.ReviewStatus,
		EvaluationStatus: run.EvaluationStatus, ManifestURI: run.ManifestURI,
		ManifestHash: run.ManifestHash, WinningAttempt: run.WinningAttempt,
		FailureCode: run.FailureCode, FailureMessage: failureMessage,
		CreatedAt: run.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		UpdatedAt: run.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
}

func (s *Server) loadRun(ctx context.Context, runID string) (domain.AnalysisRun, error) {
	run, err := s.Service.Repository.GetRun(ctx, runID)
	if errors.Is(err, runs.ErrNotFound) {
		return domain.AnalysisRun{}, runs.ErrNotFound
	}
	return run, err
}

func (s *Server) loadCatalog(ctx context.Context, run domain.AnalysisRun) (artifactCatalog, error) {
	if run.ManifestURI == "" {
		return artifactCatalog{}, runs.ErrNotFound
	}
	data, err := s.Service.Artifacts.Get(ctx, run.ManifestURI)
	if err != nil {
		return artifactCatalog{}, err
	}
	if len(data) > maxPresentationArtifactBytes {
		return artifactCatalog{}, errors.New("manifest exceeds presentation limit")
	}
	if run.ManifestHash != "" && run.ManifestHash != report.Hash(data) {
		return artifactCatalog{}, errors.New("manifest hash mismatch")
	}
	var manifest report.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return artifactCatalog{}, fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.Run.RunID != "" && manifest.Run.RunID != run.RunID {
		return artifactCatalog{}, errors.New("manifest run identity mismatch")
	}
	entries := make(map[string]report.Artifact, len(manifest.Artifacts)+1)
	for name, entry := range manifest.Artifacts {
		if !safeArtifactName(name) {
			continue
		}
		entries[name] = entry
	}
	entries["manifest.json"] = report.Artifact{SHA256: report.Hash(data), Size: len(data), Sensitivity: "INTERNAL"}
	return artifactCatalog{ManifestURI: run.ManifestURI, Manifest: manifest, Bytes: data, Entries: entries}, nil
}

func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	run, err := s.loadRun(r.Context(), r.PathValue("run_id"))
	if err != nil {
		s.writeReadError(w, err)
		return
	}
	catalog, err := s.loadCatalog(r.Context(), run)
	if err != nil {
		s.writeReadError(w, err)
		return
	}
	names := make([]string, 0, len(catalog.Entries))
	for name := range catalog.Entries {
		names = append(names, name)
	}
	sort.Strings(names)
	artifacts := make([]ArtifactSummary, 0, len(names))
	for _, name := range names {
		entry := catalog.Entries[name]
		artifacts = append(artifacts, ArtifactSummary{Name: name, SHA256: entry.SHA256, Size: entry.Size, Sensitivity: entry.Sensitivity})
	}
	writeJSON(w, http.StatusOK, ArtifactListResponse{RunID: run.RunID, AttemptNo: run.AttemptNo, Artifacts: artifacts})
}

func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	s.serveNamedArtifact(w, r, "report.md", "text/markdown; charset=utf-8")
}

func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	s.serveNamedArtifact(w, r, "manifest.json", "application/json; charset=utf-8")
}

func (s *Server) artifact(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	contentType := "application/json; charset=utf-8"
	if strings.HasSuffix(name, ".md") {
		contentType = "text/markdown; charset=utf-8"
	}
	s.serveNamedArtifact(w, r, name, contentType)
}

func (s *Server) serveNamedArtifact(w http.ResponseWriter, r *http.Request, name, contentType string) {
	if !safeArtifactName(name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid artifact name"})
		return
	}
	run, err := s.loadRun(r.Context(), r.PathValue("run_id"))
	if err != nil {
		s.writeReadError(w, err)
		return
	}
	catalog, err := s.loadCatalog(r.Context(), run)
	if err != nil {
		s.writeReadError(w, err)
		return
	}
	if _, ok := catalog.Entries[name]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artifact not found"})
		return
	}
	uri := catalog.ManifestURI
	if name != "manifest.json" {
		uri = strings.TrimSuffix(catalog.ManifestURI, "manifest.json") + name
	}
	data, err := s.Service.Artifacts.Get(r.Context(), uri)
	if err != nil {
		s.writeReadError(w, err)
		return
	}
	if len(data) > maxPresentationArtifactBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "artifact exceeds presentation limit"})
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) writeReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, runs.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "presentation data unavailable"})
}

func safeArtifactName(name string) bool {
	if name == "" || strings.ContainsRune(name, 0) || path.IsAbs(name) || path.Clean(name) != name || strings.HasPrefix(name, "../") || name == ".." {
		return false
	}
	return name == "manifest.json" || strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".md")
}
