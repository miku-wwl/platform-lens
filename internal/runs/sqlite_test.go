package runs

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/miku-wwl/platform-lens/internal/runtime"
)

func TestOpenSQLiteCreatesMissingParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "runs.sqlite3")
	repository, err := OpenSQLite(path, runtime.RealClock{})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	if err := repository.Ready(context.Background()); err != nil {
		t.Fatalf("repository readiness failed after creating its parent: %v", err)
	}
}
