package tests

import (
	"context"
	"os"
	"path/filepath"
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

func TestSQLiteFilesystemLocalE2E(t *testing.T) {
	fixture := t.TempDir()
	git(t, fixture, "init", "-b", "main")
	git(t, fixture, "config", "user.email", "platformlens@example.invalid")
	git(t, fixture, "config", "user.name", "PlatformLens Test")
	if err := os.WriteFile(filepath.Join(fixture, "main.tf"), []byte("terraform {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "deployment.yaml"), []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: sample\nspec: {}\n"), 0o600); err != nil {
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
	config.Limits.CommandTimeoutSeconds = 45
	if err := config.Prepare(); err != nil {
		t.Fatal(err)
	}
	toolchain, err := runtime.LoadToolchain(config.ToolchainPath)
	if err != nil {
		t.Fatal(err)
	}
	clock := runtime.RealClock{}
	repository, err := runs.OpenSQLite(config.DatabasePath, clock)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	artifacts, err := storage.NewFileSystem(config.ArtifactDir)
	if err != nil {
		t.Fatal(err)
	}
	sourceRuntime, err := source.NewRuntime(config, execution.NewCommandRunner())
	if err != nil {
		t.Fatal(err)
	}
	service := &app.Service{Config: config, Clock: clock, Repository: repository, Artifacts: artifacts, Source: sourceRuntime, Validation: validation.NewEngine(config, execution.NewCommandRunner(), toolchain), Reviewer: review.DeterministicFakeReviewer{}, Evaluator: evaluation.DeterministicFakeEvaluator{}, Toolchain: toolchain}
	run, err := service.Submit(context.Background(), app.SubmitRequest{RepositoryURL: fixture, RequestedRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.Process(context.Background(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != domain.StateCompleted {
		t.Fatalf("run did not complete: %+v", completed)
	}
	if completed.CommitOID == "" || completed.ManifestURI == "" || completed.ManifestHash == "" || completed.LeaseOwner != "" || completed.LeaseExpiresAt != nil {
		t.Fatalf("completion invariant missing: %+v", completed)
	}
	manifest, err := artifacts.Get(context.Background(), completed.ManifestURI)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest) == 0 {
		t.Fatal("manifest is empty")
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(cwd)
}
