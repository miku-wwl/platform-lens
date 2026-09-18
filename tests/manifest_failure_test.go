package tests

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miku-wwl/platform-lens/internal/app"
	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/evaluation"
	"github.com/miku-wwl/platform-lens/internal/execution"
	"github.com/miku-wwl/platform-lens/internal/review"
	"github.com/miku-wwl/platform-lens/internal/runs"
	"github.com/miku-wwl/platform-lens/internal/runtime"
	"github.com/miku-wwl/platform-lens/internal/source"
	"github.com/miku-wwl/platform-lens/internal/storage"
	"github.com/miku-wwl/platform-lens/internal/validation"
)

func TestManifestPutFailureCannotCompleteRun(t *testing.T) {
	fixture := t.TempDir()
	git(t, fixture, "init", "-b", "main")
	git(t, fixture, "config", "user.email", "platformlens@example.invalid")
	git(t, fixture, "config", "user.name", "PlatformLens Test")
	if err := os.WriteFile(filepath.Join(fixture, "main.tf"), []byte("terraform {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, fixture, "add", ".")
	git(t, fixture, "commit", "-m", "fixture")
	config := runtime.DefaultConfig()
	config.AllowLocalGit = true
	config.DataDir = t.TempDir()
	config.DatabasePath = filepath.Join(config.DataDir, "runs.db")
	config.ArtifactDir = filepath.Join(config.DataDir, "artifacts")
	config.WorkspaceDir = filepath.Join(config.DataDir, "workspaces")
	config.SourceCacheDir = filepath.Join(config.DataDir, "cache")
	config.ToolchainPath = filepath.Join(repositoryRoot(t), "toolchain.lock")
	if err := config.Prepare(); err != nil {
		t.Fatal(err)
	}
	toolchain, err := runtime.LoadToolchain(config.ToolchainPath)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := runs.OpenSQLite(config.DatabasePath, runtime.RealClock{})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	delegate, err := storage.NewFileSystem(config.ArtifactDir)
	if err != nil {
		t.Fatal(err)
	}
	failing := &manifestFailStorage{delegate: delegate}
	sourceRuntime, err := source.NewRuntime(config, execution.NewCommandRunner())
	if err != nil {
		t.Fatal(err)
	}
	service := &app.Service{Config: config, Clock: runtime.RealClock{}, Repository: repository, Artifacts: failing, Source: sourceRuntime, Validation: validation.NewEngine(config, execution.NewCommandRunner(), toolchain), Reviewer: review.DeterministicFakeReviewer{}, Evaluator: evaluation.DeterministicFakeEvaluator{}, Toolchain: toolchain}
	run, err := service.Submit(context.Background(), app.SubmitRequest{RepositoryURL: fixture, RequestedRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	final, err := service.Process(context.Background(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if final.State != domain.StateFailed || final.FailureCode != "PERSISTENCE_ERROR" || failing.manifestPuts != 1 {
		t.Fatalf("manifest failure was not fenced before completion: %+v puts=%d", final, failing.manifestPuts)
	}
	if final.ManifestURI != "" || final.WinningAttempt != nil {
		t.Fatalf("failed run acquired authoritative completion fields: %+v", final)
	}
}

type manifestFailStorage struct {
	delegate     *storage.FileSystem
	manifestPuts int
}

func (s *manifestFailStorage) Put(ctx context.Context, uri string, data []byte) (string, error) {
	if strings.HasSuffix(uri, "/manifest.json") {
		s.manifestPuts++
		return "", errors.New("injected manifest put failure")
	}
	return s.delegate.Put(ctx, uri, data)
}
func (s *manifestFailStorage) Get(ctx context.Context, uri string) ([]byte, error) {
	return s.delegate.Get(ctx, uri)
}
func (s *manifestFailStorage) Exists(ctx context.Context, uri string) (bool, error) {
	return s.delegate.Exists(ctx, uri)
}
func (s *manifestFailStorage) Ready(ctx context.Context) error { return s.delegate.Ready(ctx) }
