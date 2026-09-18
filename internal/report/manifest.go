package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/miku-wwl/platform-lens/internal/domain"
)

type Artifact struct {
	SHA256      string `json:"sha256"`
	Size        int    `json:"size"`
	Sensitivity string `json:"sensitivity"`
}
type Manifest struct {
	SchemaVersion               int                 `json:"schema_version"`
	PlatformLensVersion         string              `json:"platformlens_version"`
	ValidationPlanSchemaVersion int                 `json:"validation_plan_schema_version"`
	Run                         domain.AnalysisRun  `json:"run"`
	Source                      map[string]any      `json:"source"`
	Result                      map[string]any      `json:"result"`
	Toolchain                   map[string]any      `json:"toolchain"`
	Config                      map[string]any      `json:"config"`
	Dependencies                map[string]any      `json:"dependencies"`
	AI                          map[string]any      `json:"ai"`
	Artifacts                   map[string]Artifact `json:"artifacts"`
	Timestamps                  map[string]string   `json:"timestamps"`
}

func CanonicalJSON(value any) ([]byte, error) { return json.Marshal(value) }
func Hash(data []byte) string                 { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func BuildManifest(manifest Manifest, artifacts map[string][]byte) ([]byte, error) {
	manifest.Artifacts = map[string]Artifact{}
	paths := make([]string, 0, len(artifacts))
	for path := range artifacts {
		if path != "manifest.json" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		manifest.Artifacts[path] = Artifact{SHA256: Hash(artifacts[path]), Size: len(artifacts[path]), Sensitivity: "INTERNAL"}
	}
	return CanonicalJSON(manifest)
}

func RenderReport(run domain.AnalysisRun, commit string, outcome domain.AnalysisOutcome, coverage domain.CoverageStatus, results []domain.ValidationResult, diagnostics []domain.Diagnostic, review *domain.ReviewOutput, evaluation *domain.EvaluationOutput) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# PlatformLens Analysis Report\n\n- Run: `%s`\n- Commit: `%s`\n- Analysis outcome: **%s**\n- Coverage: **%s**\n\n", run.RunID, commit, outcome, coverage)
	b.WriteString("## Validation Results\n\n| Step | Producer | Target/resource | Status |\n|---|---|---|---|\n")
	for _, result := range results {
		target := result.TargetID
		if target == "" && result.Resource != nil {
			target = result.Resource.Kind + "/" + result.Resource.Name
		}
		fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | **%s** |\n", result.StepID, result.Producer, target, result.Status)
	}
	b.WriteString("\n## Diagnostics\n\n")
	if len(diagnostics) == 0 {
		b.WriteString("No deterministic diagnostics were produced.\n")
	} else {
		for _, item := range diagnostics {
			fmt.Fprintf(&b, "- **%s**: %s\n", item.Producer, strings.ReplaceAll(item.Message, "\n", " "))
		}
	}
	b.WriteString("\n## Review and Evaluation\n\n")
	if review == nil {
		b.WriteString("Reviewer unavailable.\n")
	} else {
		b.WriteString(review.Summary + "\n")
		for _, finding := range review.Findings {
			fmt.Fprintf(&b, "- %s: %s (evidence: %s)\n", finding.Finding.Title, finding.Finding.Observation, strings.Join(finding.Finding.EvidenceIDs, ", "))
		}
	}
	if evaluation != nil {
		for _, item := range evaluation.Items {
			fmt.Fprintf(&b, "- `%s`: **%s** — %s\n", item.FindingID, item.Verdict, item.Reason)
		}
	}
	return []byte(b.String())
}
