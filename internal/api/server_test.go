package api

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/miku-wwl/platform-lens/internal/app"
	"github.com/miku-wwl/platform-lens/internal/runs"
	"github.com/miku-wwl/platform-lens/internal/runtime"
	"github.com/miku-wwl/platform-lens/internal/storage"
)

func TestReadyRequiresDependenciesAndAcceptingWorker(t *testing.T) {
	root := t.TempDir()
	repository, err := runs.OpenSQLite(filepath.Join(root, "runs.sqlite3"), runtime.RealClock{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	artifacts, err := storage.NewFileSystem(filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	config := runtime.DefaultConfig()
	config.WorkerPollSeconds = 1
	service := &app.Service{Config: config, Clock: runtime.RealClock{}, Repository: repository, Artifacts: artifacts}
	server := (&Server{Service: service}).Handler()

	request := httptest.NewRequest("GET", "/readyz", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != 503 {
		t.Fatalf("ready before worker acceptance = %d, want 503", response.Code)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		service.WorkerLoop(ctx)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		request = httptest.NewRequest("GET", "/readyz", nil)
		response = httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code == 200 {
			cancel()
			<-workerDone
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-workerDone
	t.Fatalf("ready did not become 200 after worker acceptance; last status=%d", response.Code)
}
