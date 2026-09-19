package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/miku-wwl/platform-lens/internal/app"
	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/report"
	"github.com/miku-wwl/platform-lens/internal/runs"
	"github.com/miku-wwl/platform-lens/internal/runtime"
	"github.com/miku-wwl/platform-lens/internal/storage"
)

func TestPresentationEndpointsUseManifestAllowlist(t *testing.T) {
	root := t.TempDir()
	clock := runtime.NewFakeClock(time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC))
	repository, err := runs.OpenSQLite(filepath.Join(root, "runs.sqlite3"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	artifacts, err := storage.NewFileSystem(filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	run, err := repository.CreateRun(context.Background(), "https://example.test/repo.git", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	run, err = repository.ClaimRun(context.Background(), run.RunID, "worker-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, transition := range [][2]domain.RunState{{domain.StateClaimed, domain.StateRetrieving}, {domain.StateRetrieving, domain.StateValidating}, {domain.StateValidating, domain.StateReviewing}, {domain.StateReviewing, domain.StateEvaluating}, {domain.StateEvaluating, domain.StatePersisting}} {
		run, err = repository.UpdatePhase(context.Background(), run.RunID, run.AttemptNo, "worker-1", transition[0], transition[1])
		if err != nil {
			t.Fatal(err)
		}
	}
	reportBytes := []byte("# safe report\n")
	reportURI := storage.ArtifactURI(run.RunID, run.AttemptNo, "report.md")
	if _, err := artifacts.Put(context.Background(), reportURI, reportBytes); err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := report.BuildManifest(report.Manifest{SchemaVersion: 1, Run: run}, map[string][]byte{"report.md": reportBytes})
	if err != nil {
		t.Fatal(err)
	}
	manifestURI := storage.ArtifactURI(run.RunID, run.AttemptNo, "manifest.json")
	if _, err := artifacts.Put(context.Background(), manifestURI, manifestBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CompleteRun(context.Background(), run.RunID, run.AttemptNo, "worker-1", domain.OutcomeNoFindings, domain.CoverageComplete, domain.ReviewCompleted, domain.EvaluationCompleted, manifestURI, storage.Hash(manifestBytes)); err != nil {
		t.Fatal(err)
	}

	server := (&Server{Service: &app.Service{Repository: repository, Artifacts: artifacts}}).Handler()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest("GET", "/analysis?limit=10", nil))
	if response.Code != 200 {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	var listed RunListResponse
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil || len(listed.Runs) != 1 || listed.Runs[0].State != domain.StateCompleted {
		t.Fatalf("unexpected list response: %+v %v", listed, err)
	}

	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest("GET", "/analysis/"+run.RunID+"/artifacts", nil))
	if response.Code != 200 || !containsArtifact(response.Body.String(), "report.md") || !containsArtifact(response.Body.String(), "manifest.json") {
		t.Fatalf("artifact list status=%d body=%s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest("GET", "/analysis/"+run.RunID+"/report", nil))
	if response.Code != 200 || response.Body.String() != string(reportBytes) {
		t.Fatalf("report status=%d body=%s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest("GET", "/analysis/"+run.RunID+"/artifacts/%2e%2e/secret", nil))
	if response.Code != 400 {
		t.Fatalf("unsafe artifact status=%d body=%s", response.Code, response.Body.String())
	}
}

func containsArtifact(body, name string) bool {
	return len(body) > 0 && stringContains(body, `"name":"`+name+`"`)
}

func stringContains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
