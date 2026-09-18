package storage

import (
	"context"
	"os"
	"testing"

	"github.com/miku-wwl/platform-lens/internal/cloudaws"
	"github.com/miku-wwl/platform-lens/internal/runtime"
)

func TestS3ConstructorDoesNotProvisionBucket(t *testing.T) {
	endpoint := os.Getenv("PLATFORMLENS_LOCALSTACK_ENDPOINT")
	if endpoint == "" {
		t.Skip("set PLATFORMLENS_LOCALSTACK_ENDPOINT to run LocalStack constructor acceptance")
	}
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_REGION", "us-east-1")
	bucket := "platformlens-no-provision-" + runtime.NewID()[:8]
	config, err := cloudaws.Load(context.Background(), cloudaws.Options{Region: "us-east-1", Endpoint: endpoint, MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := NewS3(config, bucket)
	if err != nil {
		t.Fatal(err)
	}
	if err := artifacts.Ready(context.Background()); err == nil {
		t.Fatalf("constructor provisioned bucket %q; readiness unexpectedly succeeded", bucket)
	}
}
