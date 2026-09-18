package tests

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miku-wwl/platform-lens/internal/app"
	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/evaluation"
	"github.com/miku-wwl/platform-lens/internal/evidence"
	"github.com/miku-wwl/platform-lens/internal/execution"
	"github.com/miku-wwl/platform-lens/internal/review"
	"github.com/miku-wwl/platform-lens/internal/runs"
	"github.com/miku-wwl/platform-lens/internal/runtime"
	"github.com/miku-wwl/platform-lens/internal/source"
	"github.com/miku-wwl/platform-lens/internal/storage"
	"github.com/miku-wwl/platform-lens/internal/validation"
)

func TestSourceBoundaryAndWorkerIdentity(t *testing.T) {
	accepted, err := source.CanonicalURL("https://EXAMPLE.com/repo.git?ref=main", false)
	if err != nil || accepted != "https://example.com/repo.git" {
		t.Fatalf("HTTPS canonicalization failed: %q %v", accepted, err)
	}
	for _, value := range []string{"http://example.com/repo.git", "file:///tmp/repo", "ssh://git@example.com/repo.git", "git@example.com:repo.git", "https://user:password@example.com/repo.git", "https://example.com/repo.git?token=secret"} {
		if _, err := source.CanonicalURL(value, false); err == nil {
			t.Fatalf("insecure source should be rejected: %s", value)
		}
	}
	fixture := t.TempDir()
	if _, err := source.CanonicalURL(fixture, false); err == nil {
		t.Fatal("local fixture must require explicit test/dev mode")
	}
	if _, err := source.CanonicalURL(fixture, true); err != nil {
		t.Fatalf("explicit local mode rejected fixture: %v", err)
	}
	first := runtime.DefaultConfig()
	second := runtime.DefaultConfig()
	if first.WorkerID == second.WorkerID || first.WorkerID == "local-worker" {
		t.Fatalf("worker identity is not process-instance unique: %q %q", first.WorkerID, second.WorkerID)
	}
}

func TestServiceRejectsBeforePersistence(t *testing.T) {
	config := runtime.DefaultConfig()
	config.DataDir = t.TempDir()
	config.DatabasePath = filepath.Join(config.DataDir, "runs.db")
	repository, err := runs.OpenSQLite(config.DatabasePath, runtime.RealClock{})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	service := &app.Service{Config: config, Repository: repository}
	for _, value := range []string{"http://example.com/repo.git", "https://user:secret@example.com/repo.git", "file:///tmp/repo"} {
		if _, err := service.Submit(context.Background(), app.SubmitRequest{RepositoryURL: value, RequestedRef: "HEAD"}); err == nil {
			t.Fatalf("submission should reject %s", value)
		}
	}
	queued, err := repository.FindQueuedCandidates(context.Background(), 10)
	if err != nil || len(queued) != 0 {
		t.Fatalf("rejected URL was persisted: %v %+v", err, queued)
	}
	accepted, err := service.Submit(context.Background(), app.SubmitRequest{RepositoryURL: "https://EXAMPLE.com/repo.git?ref=main", RequestedRef: "HEAD"})
	if err != nil || accepted.RepositoryURL != "https://example.com/repo.git" {
		t.Fatalf("HTTPS submission was not accepted/canonicalized: %+v %v", accepted, err)
	}
}

func TestRepositoryLockAndWorkspaceSweeperAreConservative(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "repo.lock")
	first, err := source.AcquireFileLock(lockPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.AcquireFileLock(lockPath, 75*time.Millisecond); err == nil {
		t.Fatal("live repository lock was not respected")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(`{"pid":2147483647,"hostname":"`+localHostname()+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := source.AcquireFileLock(lockPath, time.Second)
	if err != nil {
		t.Fatalf("stale repository lock was not recovered: %v", err)
	}
	_ = second.Close()

	root := t.TempDir()
	for _, name := range []string{"active", "stale"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		marker, _ := json.Marshal(source.WorkspaceMarker{RunID: name, AttemptNo: 1, CommitOID: strings.Repeat("a", 40), CreatedAt: time.Now().Add(-time.Hour)})
		if err := os.WriteFile(filepath.Join(path, ".platformlens-workspace.json"), marker, 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := source.SweepWorkspaces(root, time.Hour, func(marker source.WorkspaceMarker) bool { return marker.RunID == "active" })
	if err != nil || len(removed) != 1 || !strings.HasSuffix(removed[0], "stale") {
		t.Fatalf("unexpected sweep result: %v %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(root, "active")); err != nil {
		t.Fatalf("active workspace was removed: %v", err)
	}
}

func TestRedactionHappensBeforeReviewerContext(t *testing.T) {
	clock := runtime.NewFakeClock(time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC))
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "main.tf"), []byte("token=SUPER_SECRET_VALUE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	diagnostic := domain.Diagnostic{DiagnosticID: "diagnostic-1", Producer: "terraform", Message: "token=SUPER_SECRET_VALUE", File: "main.tf", StartLine: 1, EndLine: 1}
	envelope := evidence.Diagnostic("run", 1, strings.Repeat("a", 40), diagnostic, clock)
	excerpt, err := evidence.SourceExcerpt(workspace, "main.tf", 1, 1, "run", 1, strings.Repeat("a", 40), 1024, clock)
	if err != nil {
		t.Fatal(err)
	}
	contextInput := evidence.BuildContext([]domain.Diagnostic{diagnostic}, []domain.EvidenceEnvelope{envelope, excerpt}, 64*1024)
	encoded := mustJSON(t, contextInput)
	if strings.Contains(encoded, "SUPER_SECRET_VALUE") {
		t.Fatalf("secret crossed into reviewer context: %s", encoded)
	}
	reviewed, err := (review.DeterministicFakeReviewer{}).Review(context.Background(), review.Input{Context: contextInput})
	if err != nil || len(reviewed.Findings) != 1 || strings.Contains(reviewed.Findings[0].Finding.Observation, "SUPER_SECRET_VALUE") {
		t.Fatalf("reviewer received unredacted context: %+v %v", reviewed, err)
	}
	if !strings.Contains(string(mustJSON(t, envelope)), "[REDACTED]") || !strings.Contains(string(mustJSON(t, excerpt)), "[REDACTED]") {
		t.Fatal("redacted evidence representation missing")
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func localHostname() string {
	value, _ := os.Hostname()
	return value
}

func TestReclaimReplayUsesPinnedCommit(t *testing.T) {
	fixture := t.TempDir()
	git(t, fixture, "init", "-b", "main")
	git(t, fixture, "config", "user.email", "platformlens@example.invalid")
	git(t, fixture, "config", "user.name", "PlatformLens Test")
	if err := os.WriteFile(filepath.Join(fixture, "note.txt"), []byte("commit A\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, fixture, "add", ".")
	git(t, fixture, "commit", "-m", "A")
	commitA := stringOutput(t, fixture, "rev-parse", "HEAD")

	clock := runtime.NewFakeClock(time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC))
	config := runtime.DefaultConfig()
	config.AllowLocalGit = true
	config.WorkerID = "worker-a"
	config.DataDir = t.TempDir()
	config.DatabasePath = filepath.Join(config.DataDir, "runs.db")
	config.ArtifactDir = filepath.Join(config.DataDir, "artifacts")
	config.WorkspaceDir = filepath.Join(config.DataDir, "workspaces")
	config.SourceCacheDir = filepath.Join(config.DataDir, "cache")
	config.ToolchainPath = filepath.Join(repositoryRoot(t), "toolchain.lock")
	if err := config.Prepare(); err != nil {
		t.Fatal(err)
	}
	repository, err := runs.OpenSQLite(config.DatabasePath, clock)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	runner := execution.NewCommandRunner()
	sourceRuntime, err := source.NewRuntime(config, runner)
	if err != nil {
		t.Fatal(err)
	}
	run, err := repository.CreateRun(context.Background(), fixture, "HEAD", "")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.ClaimRun(context.Background(), run.RunID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpdatePhase(context.Background(), run.RunID, claimed.AttemptNo, claimed.LeaseOwner, domain.StateClaimed, domain.StateRetrieving); err != nil {
		t.Fatal(err)
	}
	acquired, err := sourceRuntime.Acquire(context.Background(), fixture, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if acquired.CommitOID != commitA {
		t.Fatalf("initial pin mismatch: %s %s", acquired.CommitOID, commitA)
	}
	if _, err := repository.PinSourceIfAbsent(context.Background(), run.RunID, 1, "worker-a", acquired.CommitOID, acquired.ResolvedRef, acquired.RefType); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(fixture, "note.txt"), []byte("commit B\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, fixture, "add", ".")
	git(t, fixture, "commit", "-m", "B")
	commitB := stringOutput(t, fixture, "rev-parse", "HEAD")
	if commitA == commitB {
		t.Fatal("fixture branch did not move")
	}

	config.WorkerID = "worker-b"
	service := newLocalService(t, config, clock, repository)
	clock.Advance(2 * time.Minute)
	completed, err := service.RecoverExpired(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(completed) != 1 {
		t.Fatalf("expected one replayed run: %+v", completed)
	}
	if completed[0].CommitOID != commitA || completed[0].WinningAttempt == nil || *completed[0].WinningAttempt != 2 || !strings.Contains(completed[0].ManifestURI, "/attempts/2/") {
		t.Fatalf("replay did not preserve pinned commit/fresh attempt: %+v", completed[0])
	}
	if completed[0].CommitOID == commitB {
		t.Fatal("replay substituted moved branch commit")
	}
	if _, err := repository.CompleteRun(context.Background(), run.RunID, 1, "worker-a", domain.OutcomeNoFindings, domain.CoverageComplete, domain.ReviewCompleted, domain.EvaluationCompleted, "stale", "stale"); !errors.Is(err, runs.ErrConditional) {
		t.Fatalf("old attempt should not complete after reclaim: %v", err)
	}
}

func newLocalService(t *testing.T, config runtime.Config, clock runtime.Clock, repository runs.Repository) *app.Service {
	t.Helper()
	toolchain, err := runtime.LoadToolchain(config.ToolchainPath)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := storage.NewFileSystem(config.ArtifactDir)
	if err != nil {
		t.Fatal(err)
	}
	runner := execution.NewCommandRunner()
	sourceRuntime, err := source.NewRuntime(config, runner)
	if err != nil {
		t.Fatal(err)
	}
	return &app.Service{Config: config, Clock: clock, Repository: repository, Artifacts: artifacts, Source: sourceRuntime, Validation: validation.NewEngine(config, runner, toolchain), Reviewer: review.DeterministicFakeReviewer{}, Evaluator: evaluation.DeterministicFakeEvaluator{}, Toolchain: toolchain}
}
