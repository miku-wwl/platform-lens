package report

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/miku-wwl/platform-lens/internal/domain"
)

func TestManifestIsDeterministicAndSelfExcluding(t *testing.T) {
	base := Manifest{SchemaVersion: 1, PlatformLensVersion: "0.1.0", ValidationPlanSchemaVersion: 1, Run: domain.AnalysisRun{RunID: "run", State: domain.StateCompleted}}
	one, err := BuildManifest(base, map[string][]byte{"z.json": []byte("z"), "a.json": []byte("a"), "manifest.json": []byte("old")})
	if err != nil {
		t.Fatal(err)
	}
	two, err := BuildManifest(base, map[string][]byte{"manifest.json": []byte("different"), "a.json": []byte("a"), "z.json": []byte("z")})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(one, two) {
		t.Fatalf("manifest bytes are not deterministic:\n%s\n%s", one, two)
	}
	var decoded Manifest
	if err := json.Unmarshal(one, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded.Artifacts["manifest.json"]; ok || len(decoded.Artifacts) != 2 {
		t.Fatalf("manifest self-exclusion failed: %+v", decoded.Artifacts)
	}
}
