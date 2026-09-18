package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/evaluation"
	"github.com/miku-wwl/platform-lens/internal/evidence"
	"github.com/miku-wwl/platform-lens/internal/report"
	"github.com/miku-wwl/platform-lens/internal/review"
	"github.com/miku-wwl/platform-lens/internal/runs"
	"github.com/miku-wwl/platform-lens/internal/runtime"
	"github.com/miku-wwl/platform-lens/internal/source"
	"github.com/miku-wwl/platform-lens/internal/storage"
	"github.com/miku-wwl/platform-lens/internal/validation"
)

type Service struct {
	Config     runtime.Config
	Clock      runtime.Clock
	Repository runs.Repository
	Artifacts  storage.ArtifactStorage
	Source     *source.Runtime
	Validation *validation.Engine
	Reviewer   review.Reviewer
	Evaluator  evaluation.SemanticEvaluator
	Logger     *slog.Logger
	Toolchain  runtime.Toolchain
}

type SubmitRequest struct {
	RepositoryURL string `json:"repository_url"`
	RequestedRef  string `json:"requested_ref"`
	RequestedPath string `json:"requested_path,omitempty"`
}

func (s *Service) Submit(ctx context.Context, request SubmitRequest) (domain.AnalysisRun, error) {
	if request.RequestedRef == "" {
		request.RequestedRef = "HEAD"
	}
	return s.Repository.CreateRun(ctx, request.RepositoryURL, request.RequestedRef, request.RequestedPath)
}

func (s *Service) Process(ctx context.Context, runID string) (domain.AnalysisRun, error) {
	run, err := s.Repository.GetRun(ctx, runID)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	if run.State == domain.StateQueued {
		run, err = s.Repository.ClaimRun(ctx, runID, s.Config.WorkerID, time.Duration(s.Config.LeaseSeconds)*time.Second)
		if err != nil {
			return domain.AnalysisRun{}, err
		}
	}
	if run.State != domain.StateClaimed {
		return run, nil
	}
	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	heartbeat := s.startHeartbeat(attemptCtx, cancel, run)
	defer heartbeat()
	workspace := ""
	cache := ""
	defer func() {
		if workspace != "" && cache != "" {
			_ = s.Source.CleanupWorkspace(context.Background(), cache, workspace)
		}
	}()
	fail := func(code string, cause error) (domain.AnalysisRun, error) {
		current, getErr := s.Repository.GetRun(context.Background(), runID)
		if getErr == nil && current.State.Active() && current.LeaseOwner == s.Config.WorkerID {
			failed, failErr := s.Repository.FailRun(context.Background(), runID, current.AttemptNo, s.Config.WorkerID, code, cause.Error())
			if failErr == nil {
				return failed, nil
			}
			return current, failErr
		}
		return current, cause
	}
	if _, err = s.Repository.UpdatePhase(attemptCtx, runID, run.AttemptNo, s.Config.WorkerID, domain.StateClaimed, domain.StateRetrieving); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	acquired, err := s.Source.Acquire(attemptCtx, run.RepositoryURL, run.RequestedRef)
	if err != nil {
		return fail("SOURCE_ERROR", err)
	}
	cache = acquired.CachePath
	run, err = s.Repository.PinSourceIfAbsent(attemptCtx, runID, run.AttemptNo, s.Config.WorkerID, acquired.CommitOID, acquired.ResolvedRef, acquired.RefType)
	if err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	workspace, err = s.Source.CreateWorkspace(attemptCtx, cache, acquired.CommitOID, run.AttemptNo)
	if err != nil {
		return fail("SOURCE_ERROR", err)
	}
	if _, err = s.Repository.UpdatePhase(attemptCtx, runID, run.AttemptNo, s.Config.WorkerID, domain.StateRetrieving, domain.StateValidating); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	plan, err := s.Validation.BuildPlan(workspace, run.RequestedPath)
	if err != nil {
		return fail("EXECUTION_ERROR", err)
	}
	output, err := s.Validation.Execute(attemptCtx, workspace, plan)
	if err != nil {
		return fail("EXECUTION_ERROR", err)
	}
	outcome, coverage := Outcome(output.Results, s.Config.Limits)
	prefix := storage.ArtifactURI(runID, run.AttemptNo, "")
	artifacts := map[string][]byte{}
	addJSON := func(name string, value any) error {
		name = filepath.ToSlash(name)
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		artifacts[name] = data
		_, err = s.Artifacts.Put(attemptCtx, name, data)
		return err
	}
	if err := addJSON(filepath.Join(prefix, "source.json"), map[string]any{"repository_url": run.RepositoryURL, "requested_ref": run.RequestedRef, "resolved_ref": acquired.ResolvedRef, "ref_type": acquired.RefType, "commit_oid": acquired.CommitOID, "requested_path": run.RequestedPath}); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	if err := addJSON(filepath.Join(prefix, "validation-plan.json"), plan.Plan); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	if err := addJSON(filepath.Join(prefix, "diagnostics.json"), output.Diagnostics); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	if err := addJSON(filepath.Join(prefix, "source-excerpts.json"), []any{}); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	if err := addJSON(filepath.Join(prefix, "tool-executions.json"), output.Executions); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	if len(output.Dependencies) > 0 {
		if err := addJSON(filepath.Join(prefix, "terraform-dependencies.json"), output.Dependencies); err != nil {
			return fail("PERSISTENCE_ERROR", err)
		}
	}
	if err := addJSON(filepath.Join(prefix, "discovery.json"), map[string]any{"terraform_targets": plan.TerraformTargets, "kubernetes_resources": plan.KubernetesResources, "skipped": plan.Skipped}); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	if _, err = s.Repository.UpdatePhase(attemptCtx, runID, run.AttemptNo, s.Config.WorkerID, domain.StateValidating, domain.StateReviewing); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	evidenceItems := []domain.EvidenceEnvelope{}
	for _, item := range output.Diagnostics {
		evidenceItems = append(evidenceItems, evidence.Diagnostic(runID, run.AttemptNo, acquired.CommitOID, item, s.Clock))
	}
	for _, item := range output.Executions {
		evidenceItems = append(evidenceItems, evidence.Tool(runID, run.AttemptNo, acquired.CommitOID, item, s.Clock))
	}
	contextInput := evidence.BuildContext(output.Diagnostics, evidenceItems, s.Config.Limits.MaxAgentContextBytes)
	reviewerOutput, reviewerErr := s.Reviewer.Review(attemptCtx, review.Input{Context: contextInput})
	reviewStatus := domain.ReviewCompleted
	evaluationStatus := domain.EvaluationNotApplicable
	if reviewerErr != nil {
		reviewStatus = domain.ReviewUnavailable
	} else {
		if err := addJSON(filepath.Join(prefix, "review-input.json"), contextInput); err != nil {
			return fail("PERSISTENCE_ERROR", err)
		}
		if err := addJSON(filepath.Join(prefix, "reviewer.json"), reviewerOutput); err != nil {
			return fail("PERSISTENCE_ERROR", err)
		}
		for _, item := range evidenceItems {
			if err := addJSON(filepath.Join(prefix, "evidence", item.EvidenceID+".json"), item); err != nil {
				return fail("PERSISTENCE_ERROR", err)
			}
		}
	}
	if _, err = s.Repository.UpdatePhase(attemptCtx, runID, run.AttemptNo, s.Config.WorkerID, domain.StateReviewing, domain.StateEvaluating); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	evaluationOutput := (*domain.EvaluationOutput)(nil)
	if reviewerErr == nil {
		findings := make([]domain.Finding, 0, len(reviewerOutput.Findings))
		for _, item := range reviewerOutput.Findings {
			findings = append(findings, item.Finding)
		}
		valid, structural := (evaluation.DeterministicEvaluator{}).Validate(findings, evidenceItems, runID, run.AttemptNo, acquired.CommitOID)
		semantic, semanticErr := s.Evaluator.Evaluate(attemptCtx, evaluation.Input{Findings: valid, Evidence: evidenceItems})
		if semanticErr != nil {
			evaluationStatus = domain.EvaluationUnavailable
			for _, item := range valid {
				structural.Items = append(structural.Items, domain.EvaluationItem{FindingID: item.FindingID, Verdict: domain.VerdictNotEvaluated, Reason: "semantic evaluator unavailable", EvidenceIDs: item.EvidenceIDs})
			}
			evaluationOutput = &structural
		} else {
			evaluationStatus = domain.EvaluationCompleted
			structural.Items = append(structural.Items, semantic.Items...)
			evaluationOutput = &structural
		}
		if err := addJSON(filepath.Join(prefix, "evaluation-input.json"), evaluation.Input{Findings: valid, Evidence: evidenceItems}); err != nil {
			return fail("PERSISTENCE_ERROR", err)
		}
		if err := addJSON(filepath.Join(prefix, "evaluation.json"), evaluationOutput); err != nil {
			return fail("PERSISTENCE_ERROR", err)
		}
	}
	if _, err = s.Repository.UpdatePhase(attemptCtx, runID, run.AttemptNo, s.Config.WorkerID, domain.StateEvaluating, domain.StatePersisting); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	reportBytes := report.RenderReport(run, acquired.CommitOID, outcome, coverage, output.Results, output.Diagnostics, optionalReview(reviewerErr, reviewerOutput), evaluationOutput)
	reportPath := filepath.ToSlash(filepath.Join(prefix, "report.md"))
	artifacts[reportPath] = reportBytes
	if _, err = s.Artifacts.Put(attemptCtx, reportPath, reportBytes); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	manifest := report.Manifest{SchemaVersion: 1, PlatformLensVersion: s.Config.Version, ValidationPlanSchemaVersion: 1, Run: run, Source: map[string]any{"repository_url": run.RepositoryURL, "requested_ref": run.RequestedRef, "resolved_ref": acquired.ResolvedRef, "ref_type": acquired.RefType, "commit_oid": acquired.CommitOID}, Result: map[string]any{"analysis_outcome": outcome, "coverage_status": coverage}, Toolchain: map[string]any{"go_build_version": s.Toolchain.Lock.Go.Version, "expected_actual": s.Toolchain.Report(), "toolchain_lock_hash": s.Toolchain.Hash}, Config: map[string]any{"redaction_rules_version": evidence.RedactionRulesVersion, "context_builder_version": "1"}, Dependencies: map[string]any{"terraform_dependencies": output.Dependencies, "kubernetes_version": s.Toolchain.Lock.Kubeconform.KubernetesVersion, "schema_repository": s.Toolchain.Lock.Kubeconform.SchemaRepository, "schema_repository_commit": s.Toolchain.Lock.Kubeconform.SchemaRepositoryCommit}, AI: map[string]any{"reviewer": "DeterministicFakeReviewer", "evaluator": "DeterministicFakeEvaluator"}, Timestamps: map[string]string{"created_at": run.CreatedAt.UTC().Format(time.RFC3339Nano), "updated_at": s.Clock.Now().UTC().Format(time.RFC3339Nano)}}
	manifestBytes, err := report.BuildManifest(manifest, relativeArtifacts(prefix, artifacts))
	if err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	manifestPath := filepath.ToSlash(filepath.Join(prefix, "manifest.json"))
	artifacts[manifestPath] = manifestBytes
	if _, err = s.Artifacts.Put(attemptCtx, manifestPath, manifestBytes); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	manifestHash := report.Hash(manifestBytes)
	final, err := s.Repository.CompleteRun(attemptCtx, runID, run.AttemptNo, s.Config.WorkerID, outcome, coverage, reviewStatus, evaluationStatus, manifestPath, manifestHash)
	if err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	return final, nil
}

func (s *Service) startHeartbeat(ctx context.Context, cancel context.CancelFunc, run domain.AnalysisRun) func() {
	heartbeatCtx, stop := context.WithCancel(ctx)
	var mu sync.Mutex
	expected := *run.LeaseExpiresAt
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Duration(s.Config.HeartbeatSeconds) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				current := expected
				mu.Unlock()
				next := current.Add(time.Duration(s.Config.HeartbeatSeconds*2) * time.Second)
				updated, err := s.Repository.RenewLease(heartbeatCtx, run.RunID, run.AttemptNo, s.Config.WorkerID, current, next)
				if err != nil {
					cancel()
					return
				}
				if updated.LeaseExpiresAt != nil {
					mu.Lock()
					expected = *updated.LeaseExpiresAt
					mu.Unlock()
				}
			}
		}
	}()
	return func() { stop(); <-done }
}

func Outcome(results []domain.ValidationResult, limits runtime.Limits) (domain.AnalysisOutcome, domain.CoverageStatus) {
	effective, findings, requiredErrors, partial := 0, 0, 0, false
	for _, result := range results {
		if result.StepKind == "VALIDATOR" && (result.Status == domain.ValidationPass || result.Status == domain.ValidationFail) {
			effective++
		}
		if result.Status == domain.ValidationFail {
			findings++
		}
		if result.Required && result.Status == domain.ValidationError {
			requiredErrors++
		}
		if result.Status == domain.ValidationError || result.Status == domain.ValidationSkippedSchemaMissing || result.Status == domain.ValidationSkippedUnsupported {
			partial = true
		}
	}
	coverage := domain.CoverageComplete
	if len(results) == 0 {
		coverage = domain.CoverageNone
	} else if partial {
		coverage = domain.CoveragePartial
	}
	if findings > 0 {
		return domain.OutcomeFindings, coverage
	}
	if requiredErrors > 0 || effective == 0 || len(results) > limits.MaxTargets*100 {
		return domain.OutcomeInconclusive, coverage
	}
	return domain.OutcomeNoFindings, coverage
}
func optionalReview(err error, output domain.ReviewOutput) *domain.ReviewOutput {
	if err != nil {
		return nil
	}
	return &output
}
func relativeArtifacts(prefix string, artifacts map[string][]byte) map[string][]byte {
	result := map[string][]byte{}
	for path, data := range artifacts {
		result[path[len(prefix):]] = data
	}
	return result
}
