package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/miku-wwl/platform-lens/internal/api"
	"github.com/miku-wwl/platform-lens/internal/app"
	"github.com/miku-wwl/platform-lens/internal/evaluation"
	"github.com/miku-wwl/platform-lens/internal/execution"
	"github.com/miku-wwl/platform-lens/internal/review"
	"github.com/miku-wwl/platform-lens/internal/runs"
	"github.com/miku-wwl/platform-lens/internal/runtime"
	"github.com/miku-wwl/platform-lens/internal/source"
	"github.com/miku-wwl/platform-lens/internal/storage"
	"github.com/miku-wwl/platform-lens/internal/validation"
)

func main() {
	config := runtime.LoadConfig()
	if err := config.Prepare(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := config.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	toolchain, err := runtime.LoadToolchain(config.ToolchainPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	logger := runtime.NewLogger()
	runner := execution.NewCommandRunner()
	clock := runtime.RealClock{}
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	service, err := newService(context.Background(), config, toolchain, runner, clock, logger)
	if err != nil {
		logger.Error("service initialization failed", "error", err)
		os.Exit(1)
	}
	switch command {
	case "serve":
		serve(service, api.Server{Service: service, Toolchain: toolchain})
	case "analyze":
		analyze(service, os.Args[2:])
	case "get":
		get(service, os.Args[2:])
	case "toolchain":
		data, _ := json.MarshalIndent(toolchain.Report(), "", "  ")
		fmt.Println(string(data))
	default:
		fmt.Fprintln(os.Stderr, "usage: platformlens [serve|analyze|get|toolchain]")
		os.Exit(2)
	}
}

func newService(ctx context.Context, config runtime.Config, toolchain runtime.Toolchain, runner *execution.CommandRunner, clock runtime.Clock, logger *slog.Logger) (*app.Service, error) {
	var repo runs.Repository
	var artifacts storage.ArtifactStorage
	var err error
	if config.AWSEndpointURL != "" {
		repo, err = runs.NewDynamoRepository(ctx, config.AWSEndpointURL, config.AWSRegion, config.DynamoTable, config.DynamoGSI, clock)
		if err != nil {
			return nil, err
		}
		artifacts, err = storage.NewS3(ctx, config.AWSEndpointURL, config.AWSRegion, config.S3Bucket)
	} else {
		repo, err = runs.OpenSQLite(config.DatabasePath, clock)
		if err != nil {
			return nil, err
		}
		artifacts, err = storage.NewFileSystem(config.ArtifactDir)
	}
	if err != nil {
		return nil, err
	}
	sourceRuntime, err := source.NewRuntime(config, runner)
	if err != nil {
		return nil, err
	}
	return &app.Service{Config: config, Clock: clock, Repository: repo, Artifacts: artifacts, Source: sourceRuntime, Validation: validation.NewEngine(config, runner, toolchain), Reviewer: review.DeterministicFakeReviewer{}, Evaluator: evaluation.DeterministicFakeEvaluator{}, Logger: logger, Toolchain: toolchain}, nil
}

func serve(service *app.Service, server api.Server) {
	host, port := "127.0.0.1", "8000"
	if value := os.Getenv("PLATFORMLENS_HOST"); value != "" {
		host = value
	}
	if value := os.Getenv("PLATFORMLENS_PORT"); value != "" {
		port = value
	}
	address := host + ":" + port
	service.Logger.Info("server starting", "address", address)
	if err := http.ListenAndServe(address, server.Handler()); err != nil {
		service.Logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func analyze(service *app.Service, args []string) {
	flags := flag.NewFlagSet("analyze", flag.ExitOnError)
	ref := flags.String("ref", "HEAD", "Git reference")
	requestedPath := flags.String("path", "", "repository-relative path")
	_ = flags.Parse(args)
	positional := flags.Args()
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, "usage: platformlens analyze [--ref REF] [--path PATH] REPOSITORY_URL")
		os.Exit(2)
	}
	run, err := service.Submit(context.Background(), app.SubmitRequest{RepositoryURL: positional[0], RequestedRef: *ref, RequestedPath: *requestedPath})
	if err == nil {
		run, err = service.Process(context.Background(), run.RunID)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data, _ := json.MarshalIndent(run, "", "  ")
	fmt.Println(string(data))
}
func get(service *app.Service, args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: platformlens get RUN_ID")
		os.Exit(2)
	}
	run, err := service.Repository.GetRun(context.Background(), args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data, _ := json.MarshalIndent(run, "", "  ")
	fmt.Println(string(data))
}
