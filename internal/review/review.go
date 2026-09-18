package review

import (
	"context"

	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/evidence"
	"github.com/miku-wwl/platform-lens/internal/runtime"
)

type Input struct {
	Context evidence.Context `json:"context"`
}
type Reviewer interface {
	Review(context.Context, Input) (domain.ReviewOutput, error)
}

type DeterministicFakeReviewer struct{}

func (DeterministicFakeReviewer) Review(_ context.Context, input Input) (domain.ReviewOutput, error) {
	result := domain.ReviewOutput{SchemaVersion: 1, Summary: "deterministic fake reviewer completed", Findings: []domain.ReviewedFinding{}}
	for _, diagnostic := range input.Context.Diagnostics {
		ids := []string{}
		for _, item := range input.Context.Evidence {
			if item.EvidenceType == domain.EvidenceDiagnostic {
				if id, ok := item.Payload["diagnostic_id"].(string); ok && id == diagnostic.DiagnosticID {
					ids = append(ids, item.EvidenceID)
				}
			}
		}
		result.Findings = append(result.Findings, domain.ReviewedFinding{Finding: domain.Finding{FindingID: runtime.NewID(), Title: diagnostic.Producer + " finding", Observation: diagnostic.Message, Interpretation: "The deterministic producer reported a validation issue.", EvidenceIDs: ids, TargetID: diagnostic.TargetID, Resource: diagnostic.Resource}, Confidence: "HIGH"})
	}
	return result, nil
}

type UnavailableReviewer struct{}

func (UnavailableReviewer) Review(context.Context, Input) (domain.ReviewOutput, error) {
	return domain.ReviewOutput{}, context.Canceled
}
