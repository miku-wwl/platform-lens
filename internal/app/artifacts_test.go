package app

import (
	"context"
	"testing"

	"github.com/miku-wwl/platform-lens/internal/storage"
)

type recordingArtifactStorage struct {
	values map[string][]byte
}

func (s *recordingArtifactStorage) Put(_ context.Context, uri string, data []byte) (string, error) {
	if s.values == nil {
		s.values = make(map[string][]byte)
	}
	s.values[uri] = append([]byte(nil), data...)
	return uri, nil
}

func (*recordingArtifactStorage) Get(context.Context, string) ([]byte, error)  { return nil, nil }
func (*recordingArtifactStorage) Exists(context.Context, string) (bool, error) { return false, nil }
func (*recordingArtifactStorage) Ready(context.Context) error                  { return nil }

var _ storage.ArtifactStorage = (*recordingArtifactStorage)(nil)

func TestAttemptArtifactsUsesOnePathForStorageAndManifest(t *testing.T) {
	store := &recordingArtifactStorage{}
	artifacts := newAttemptArtifacts(context.Background(), store, "run-1", 2)

	if err := artifacts.putJSON("source.json", map[string]string{"commit": "abc"}); err != nil {
		t.Fatal(err)
	}
	if got, want := artifacts.path("source.json"), "runs/run-1/attempts/2/source.json"; got != want {
		t.Fatalf("artifact path=%q, want %q", got, want)
	}
	if _, ok := store.values["runs/run-1/attempts/2/source.json"]; !ok {
		t.Fatalf("storage did not receive the authoritative artifact path: %v", store.values)
	}
	if _, ok := artifacts.relative()["source.json"]; !ok {
		t.Fatalf("manifest-relative artifact key changed: %v", artifacts.relative())
	}
}
