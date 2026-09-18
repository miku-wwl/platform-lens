package evaluation

import (
	"context"

	"github.com/miku-wwl/platform-lens/internal/domain"
)

type Input struct {
	Findings []domain.Finding          `json:"findings"`
	Evidence []domain.EvidenceEnvelope `json:"evidence"`
}
type SemanticEvaluator interface {
	Evaluate(context.Context, Input) (domain.EvaluationOutput, error)
}

type DeterministicEvaluator struct{}

func (DeterministicEvaluator) Validate(findings []domain.Finding, evidenceItems []domain.EvidenceEnvelope, runID string, attempt int, commitOID string) ([]domain.Finding, domain.EvaluationOutput) {
	byID := map[string]domain.EvidenceEnvelope{}
	for _, item := range evidenceItems {
		byID[item.EvidenceID] = item
	}
	valid := []domain.Finding{}
	output := domain.EvaluationOutput{SchemaVersion: 1, Items: []domain.EvaluationItem{}}
	for _, finding := range findings {
		reasons := []string{}
		if len(finding.EvidenceIDs) == 0 {
			reasons = append(reasons, "evidence missing")
		}
		for _, id := range finding.EvidenceIDs {
			item, ok := byID[id]
			if !ok {
				reasons = append(reasons, "evidence identity missing")
				continue
			}
			if item.RunID != runID {
				reasons = append(reasons, "run_id mismatch")
			}
			if item.AttemptNo != attempt {
				reasons = append(reasons, "attempt_no mismatch")
			}
			if item.CommitOID != commitOID {
				reasons = append(reasons, "commit_oid mismatch")
			}
			if item.Producer == "" {
				reasons = append(reasons, "producer missing")
			}
		}
		if len(reasons) > 0 {
			output.Items = append(output.Items, domain.EvaluationItem{FindingID: finding.FindingID, Verdict: domain.VerdictUnsupported, Reason: join(reasons), EvidenceIDs: finding.EvidenceIDs})
		} else {
			valid = append(valid, finding)
		}
	}
	return valid, output
}

type DeterministicFakeEvaluator struct{}

func (DeterministicFakeEvaluator) Evaluate(_ context.Context, input Input) (domain.EvaluationOutput, error) {
	output := domain.EvaluationOutput{SchemaVersion: 1, Items: []domain.EvaluationItem{}}
	for _, finding := range input.Findings {
		verdict := domain.VerdictSupported
		reason := "evidence identity is present"
		if len(finding.EvidenceIDs) == 0 {
			verdict = domain.VerdictUnsupported
			reason = "finding has no evidence"
		}
		output.Items = append(output.Items, domain.EvaluationItem{FindingID: finding.FindingID, Verdict: verdict, Reason: reason, EvidenceIDs: finding.EvidenceIDs})
	}
	return output, nil
}

type UnavailableEvaluator struct{}

func (UnavailableEvaluator) Evaluate(context.Context, Input) (domain.EvaluationOutput, error) {
	return domain.EvaluationOutput{}, context.Canceled
}
func join(values []string) string {
	result := ""
	for i, value := range values {
		if i > 0 {
			result += "; "
		}
		result += value
	}
	return result
}
