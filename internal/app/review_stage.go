package app

import (
	"context"

	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/evaluation"
	"github.com/miku-wwl/platform-lens/internal/review"
)

type reviewStageResult struct {
	status   domain.ReviewStatus
	output   *domain.ReviewOutput
	findings []domain.Finding
}

func (s *Service) reviewAttempt(ctx context.Context, artifacts *attemptArtifacts, input review.Input) (reviewStageResult, error) {
	output, reviewErr := s.Reviewer.Review(ctx, input)
	if reviewErr != nil {
		return reviewStageResult{status: domain.ReviewUnavailable}, nil
	}
	if err := artifacts.putJSON("review-input.json", input.Context); err != nil {
		return reviewStageResult{}, err
	}
	if err := artifacts.putJSON("reviewer.json", output); err != nil {
		return reviewStageResult{}, err
	}
	findings := make([]domain.Finding, 0, len(output.Findings))
	for _, item := range output.Findings {
		findings = append(findings, item.Finding)
	}
	return reviewStageResult{status: domain.ReviewCompleted, output: &output, findings: findings}, nil
}

func (s *Service) evaluateAttempt(ctx context.Context, artifacts *attemptArtifacts, findings []domain.Finding, evidenceItems []domain.EvidenceEnvelope, runID string, attempt int, commitOID string) (*domain.EvaluationOutput, domain.EvaluationStatus, error) {
	if findings == nil {
		return nil, domain.EvaluationNotApplicable, nil
	}
	valid, structural := (evaluation.DeterministicEvaluator{}).Validate(findings, evidenceItems, runID, attempt, commitOID)
	semantic, semanticErr := s.Evaluator.Evaluate(ctx, evaluation.Input{Findings: valid, Evidence: evidenceItems})
	status := domain.EvaluationCompleted
	if semanticErr != nil {
		status = domain.EvaluationUnavailable
		for _, item := range valid {
			structural.Items = append(structural.Items, domain.EvaluationItem{FindingID: item.FindingID, Verdict: domain.VerdictNotEvaluated, Reason: "semantic evaluator unavailable", EvidenceIDs: item.EvidenceIDs})
		}
	} else {
		structural.Items = append(structural.Items, semantic.Items...)
	}
	output := structural
	if err := artifacts.putJSON("evaluation-input.json", evaluation.Input{Findings: valid, Evidence: evidenceItems}); err != nil {
		return nil, status, err
	}
	if err := artifacts.putJSON("evaluation.json", &output); err != nil {
		return nil, status, err
	}
	return &output, status, nil
}
