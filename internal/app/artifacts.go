package app

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/miku-wwl/platform-lens/internal/storage"
)

// attemptArtifacts owns the bytes that are written during one attempt. The
// same byte slice is sent to storage and retained for manifest construction so
// the manifest describes the authoritative pre-manifest artifacts.
type attemptArtifacts struct {
	ctx    context.Context
	store  storage.ArtifactStorage
	prefix string
	values map[string][]byte
}

func newAttemptArtifacts(ctx context.Context, store storage.ArtifactStorage, runID string, attempt int) *attemptArtifacts {
	return &attemptArtifacts{
		ctx:    ctx,
		store:  store,
		prefix: storage.ArtifactURI(runID, attempt, ""),
		values: make(map[string][]byte),
	}
}

func (a *attemptArtifacts) path(name string) string {
	return filepath.ToSlash(filepath.Join(a.prefix, name))
}

func (a *attemptArtifacts) putJSON(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return a.put(name, data)
}

func (a *attemptArtifacts) put(name string, data []byte) error {
	path := a.path(name)
	a.values[path] = data
	_, err := a.store.Put(a.ctx, path, data)
	return err
}

func (a *attemptArtifacts) relative() map[string][]byte {
	return relativeArtifacts(a.prefix, a.values)
}

func relativeArtifacts(prefix string, artifacts map[string][]byte) map[string][]byte {
	result := make(map[string][]byte, len(artifacts))
	for path, data := range artifacts {
		// Preserve the existing manifest key format, including its leading
		// separator after the attempt prefix.
		result[path[len(prefix):]] = data
	}
	return result
}
