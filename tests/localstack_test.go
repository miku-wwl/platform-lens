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

func TestLocalStackDynamoS3E2E(t *testing.T) {
	endpoint := os.Getenv("PLATFORMLENS_LOCALSTACK_ENDPOINT")
	if endpoint == "" {
		t.Skip("set PLATFORMLENS_LOCALSTACK_ENDPOINT to run LocalStack E2E")
	}
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
	config.DatabasePath = filepath.Join(config.DataDir, "unused.db")
	config.ArtifactDir = filepath.Join(config.DataDir, "unused-artifacts")
	config.WorkspaceDir = filepath.Join(config.DataDir, "workspaces")
	config.SourceCacheDir = filepath.Join(config.DataDir, "cache")
	config.ToolchainPath = filepath.Join(repositoryRoot(t), "toolchain.lock")
	config.AWSEndpointURL = endpoint
	config.DynamoTable = "platformlens-runs"
	config.S3Bucket = "platformlens-artifacts"
	if err := config.Prepare(); err != nil {
		t.Fatal(err)
	}
	toolchain, err := runtime.LoadToolchain(config.ToolchainPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := execution.NewCommandRunner()
	repository, err := runs.NewDynamoRepository(context.Background(), endpoint, config.AWSRegion, config.DynamoTable, config.DynamoGSI, runtime.RealClock{})
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := storage.NewS3(context.Background(), endpoint, config.AWSRegion, config.S3Bucket)
	if err != nil {
		t.Fatal(err)
	}
	sourceRuntime, err := source.NewRuntime(config, runner)
	if err != nil {
		t.Fatal(err)
	}
	service := &app.Service{Config: config, Clock: runtime.RealClock{}, Repository: repository, Artifacts: artifacts, Source: sourceRuntime, Validation: validation.NewEngine(config, runner, toolchain), Reviewer: review.DeterministicFakeReviewer{}, Evaluator: evaluation.DeterministicFakeEvaluator{}, Toolchain: toolchain}
	run, err := service.Submit(context.Background(), app.SubmitRequest{RepositoryURL: fixture, RequestedRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.Process(context.Background(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != domain.StateCompleted || completed.WinningAttempt == nil || completed.ManifestURI == "" || completed.ManifestHash == "" || completed.LeaseOwner != "" {
		t.Fatalf("LocalStack completion invariant failed: %+v", completed)
	}
	manifest, err := artifacts.Get(context.Background(), completed.ManifestURI)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest) == 0 {
		t.Fatal("S3 manifest is empty")
	}
}
