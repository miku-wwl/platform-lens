package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
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
	Config      runtime.Config
	Clock       runtime.Clock
	Repository  runs.Repository
	Artifacts   storage.ArtifactStorage
	Source      *source.Runtime
	Validation  *validation.Engine
	Reviewer    review.Reviewer
	Evaluator   evaluation.SemanticEvaluator
	Logger      *slog.Logger
	Toolchain   runtime.Toolchain
	workerReady atomic.Bool
}

type SubmitRequest struct {
	RepositoryURL string `json:"repository_url"`
	RequestedRef  string `json:"requested_ref"`
	RequestedPath string `json:"requested_path,omitempty"`
}

func (s *Service) CheckDependencies(ctx context.Context) error {
	if err := s.Repository.Ready(ctx); err != nil {
		return fmt.Errorf("run repository is not ready: %w", err)
	}
	if err := s.Artifacts.Ready(ctx); err != nil {
		return fmt.Errorf("artifact storage is not ready: %w", err)
	}
	return nil
}

func (s *Service) Ready(ctx context.Context) error {
	if err := s.CheckDependencies(ctx); err != nil {
		return err
	}
	if !s.workerReady.Load() {
		return errors.New("worker is not accepting work")
	}
	return nil
}

func (s *Service) WorkerAccepting() bool { return s.workerReady.Load() }

func (s *Service) Submit(ctx context.Context, request SubmitRequest) (domain.AnalysisRun, error) {
	if request.RequestedRef == "" {
		request.RequestedRef = "HEAD"
	}
	canonical, err := source.CanonicalURL(request.RepositoryURL, s.Config.AllowLocalGit)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	request.RepositoryURL = canonical
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
	if run.LeaseOwner != s.Config.WorkerID {
		return run, nil
	}
	if run.State != domain.StateClaimed {
		return run, nil
	}
	attemptNo, workerID := run.AttemptNo, s.Config.WorkerID
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
		failed, failErr := s.Repository.FailRun(context.Background(), runID, attemptNo, workerID, code, cause.Error())
		if failErr == nil {
			return failed, nil
		}
		current, getErr := s.Repository.GetRun(context.Background(), runID)
		if getErr == nil {
			return current, cause
		}
		return domain.AnalysisRun{}, cause
	}
	if _, err = s.Repository.UpdatePhase(attemptCtx, runID, run.AttemptNo, s.Config.WorkerID, domain.StateClaimed, domain.StateRetrieving); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	var acquired source.AcquiredSource
	if run.CommitOID != "" {
		acquired, err = s.Source.AcquirePinned(attemptCtx, run.RepositoryURL, run.CommitOID, run.ResolvedRef, run.RefType)
	} else {
		acquired, err = s.Source.Acquire(attemptCtx, run.RepositoryURL, run.RequestedRef)
	}
	if err != nil {
		return fail("SOURCE_ERROR", err)
	}
	cache = acquired.CachePath
	if run.CommitOID == "" {
		run, err = s.Repository.PinSourceIfAbsent(attemptCtx, runID, attemptNo, workerID, acquired.CommitOID, acquired.ResolvedRef, acquired.RefType)
		if err != nil {
			return fail("PERSISTENCE_ERROR", err)
		}
	}
	workspace, err = s.Source.CreateWorkspace(attemptCtx, cache, acquired.CommitOID, run.RunID, run.AttemptNo)
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
	artifacts := newAttemptArtifacts(attemptCtx, s.Artifacts, runID, run.AttemptNo)
	if err := artifacts.putJSON("source.json", map[string]any{"repository_url": run.RepositoryURL, "requested_ref": run.RequestedRef, "resolved_ref": acquired.ResolvedRef, "ref_type": acquired.RefType, "commit_oid": acquired.CommitOID, "requested_path": run.RequestedPath}); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	if err := artifacts.putJSON("validation-plan.json", plan.Plan); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	if err := artifacts.putJSON("diagnostics.json", output.Diagnostics); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	if err := artifacts.putJSON("tool-executions.json", output.Executions); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	if len(output.Dependencies) > 0 {
		if err := artifacts.putJSON("terraform-dependencies.json", output.Dependencies); err != nil {
			return fail("PERSISTENCE_ERROR", err)
		}
	}
	if err := artifacts.putJSON("discovery.json", map[string]any{"terraform_targets": plan.TerraformTargets, "kubernetes_resources": plan.KubernetesResources, "skipped": plan.Skipped}); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	if _, err = s.Repository.UpdatePhase(attemptCtx, runID, run.AttemptNo, s.Config.WorkerID, domain.StateValidating, domain.StateReviewing); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	evidenceItems := []domain.EvidenceEnvelope{}
	for _, item := range output.Diagnostics {
		evidenceItems = append(evidenceItems, evidence.Diagnostic(runID, run.AttemptNo, acquired.CommitOID, item, s.Clock))
		if item.File != "" && item.StartLine > 0 {
			endLine := item.EndLine
			if endLine < item.StartLine {
				endLine = item.StartLine
			}
			if excerpt, excerptErr := evidence.SourceExcerpt(workspace, item.File, item.StartLine, endLine, runID, run.AttemptNo, acquired.CommitOID, s.Config.Limits.MaxSourceExcerptBytes, s.Clock); excerptErr == nil {
				evidenceItems = append(evidenceItems, excerpt)
			}
		}
	}
	for _, item := range output.Executions {
		evidenceItems = append(evidenceItems, evidence.Tool(runID, run.AttemptNo, acquired.CommitOID, item, s.Clock))
	}
	sourceExcerpts := []domain.EvidenceEnvelope{}
	for _, item := range evidenceItems {
		if item.EvidenceType == domain.EvidenceSource {
			sourceExcerpts = append(sourceExcerpts, item)
		}
		if err := artifacts.putJSON("evidence/"+item.EvidenceID+".json", item); err != nil {
			return fail("PERSISTENCE_ERROR", err)
		}
	}
	if err := artifacts.putJSON("source-excerpts.json", sourceExcerpts); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	contextInput := evidence.BuildContext(output.Diagnostics, evidenceItems, s.Config.Limits.MaxAgentContextBytes)
	reviewed, err := s.reviewAttempt(attemptCtx, artifacts, review.Input{Context: contextInput})
	if err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	if _, err = s.Repository.UpdatePhase(attemptCtx, runID, run.AttemptNo, s.Config.WorkerID, domain.StateReviewing, domain.StateEvaluating); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	evaluationOutput, evaluationStatus, err := s.evaluateAttempt(attemptCtx, artifacts, reviewed.findings, evidenceItems, runID, run.AttemptNo, acquired.CommitOID)
	if err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	if _, err = s.Repository.UpdatePhase(attemptCtx, runID, run.AttemptNo, s.Config.WorkerID, domain.StateEvaluating, domain.StatePersisting); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	reportBytes := report.RenderReport(run, acquired.CommitOID, outcome, coverage, output.Results, output.Diagnostics, reviewed.output, evaluationOutput)
	if err = artifacts.put("report.md", reportBytes); err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	manifest := report.Manifest{SchemaVersion: 1, PlatformLensVersion: s.Config.Version, ValidationPlanSchemaVersion: 1, Run: run, Source: map[string]any{"repository_url": run.RepositoryURL, "requested_ref": run.RequestedRef, "resolved_ref": acquired.ResolvedRef, "ref_type": acquired.RefType, "commit_oid": acquired.CommitOID}, Result: map[string]any{"analysis_outcome": outcome, "coverage_status": coverage}, Toolchain: map[string]any{"go_build_version": s.Toolchain.Lock.Go.Version, "expected_actual": s.Toolchain.Report(), "toolchain_lock_hash": s.Toolchain.Hash}, Config: map[string]any{"redaction_rules_version": evidence.RedactionRulesVersion, "context_builder_version": "1"}, Dependencies: map[string]any{"terraform_dependencies": output.Dependencies, "kubernetes_version": s.Toolchain.Lock.Kubeconform.KubernetesVersion, "schema_repository": s.Toolchain.Lock.Kubeconform.SchemaRepository, "schema_repository_commit": s.Toolchain.Lock.Kubeconform.SchemaRepositoryCommit}, AI: map[string]any{"reviewer": "DeterministicFakeReviewer", "evaluator": "DeterministicFakeEvaluator"}, Timestamps: map[string]string{"created_at": run.CreatedAt.UTC().Format(time.RFC3339Nano), "updated_at": s.Clock.Now().UTC().Format(time.RFC3339Nano)}}
	manifestBytes, err := report.BuildManifest(manifest, artifacts.relative())
	if err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	manifestURI, err := s.Artifacts.Put(attemptCtx, artifacts.path("manifest.json"), manifestBytes)
	if err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	readBack, err := s.Artifacts.Get(attemptCtx, manifestURI)
	if err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	if !bytes.Equal(readBack, manifestBytes) {
		return fail("PERSISTENCE_ERROR", errors.New("manifest read-back bytes differ from authoritative write"))
	}
	manifestHash := report.Hash(readBack)
	final, err := s.Repository.CompleteRun(attemptCtx, runID, run.AttemptNo, s.Config.WorkerID, outcome, coverage, reviewed.status, evaluationStatus, manifestURI, manifestHash)
	if err != nil {
		return fail("PERSISTENCE_ERROR", err)
	}
	return final, nil
}

// ProcessQueuedAndReclaim is the smallest Stage 1 worker poll: it claims new
// work, atomically reclaims expired attempts, and replays each claimed attempt.
func (s *Service) ProcessQueuedAndReclaim(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 16
	}
	queued, err := s.Repository.FindQueuedCandidates(ctx, limit)
	if err != nil {
		return err
	}
	for _, candidate := range queued {
		claimed, claimErr := s.Repository.ClaimRun(ctx, candidate.RunID, s.Config.WorkerID, time.Duration(s.Config.LeaseSeconds)*time.Second)
		if errors.Is(claimErr, runs.ErrConditional) {
			continue
		}
		if claimErr != nil {
			return claimErr
		}
		if _, processErr := s.Process(ctx, claimed.RunID); processErr != nil {
			s.logRunError("run processing failed", claimed, processErr)
		}
	}
	_, err = s.RecoverExpired(ctx, limit)
	return err
}

// RecoverExpired reclaims and replays expired attempts. Conditional failures
// are expected when another worker wins the race and are ignored.
func (s *Service) RecoverExpired(ctx context.Context, limit int) ([]domain.AnalysisRun, error) {
	if limit <= 0 {
		limit = 16
	}
	candidates, err := s.Repository.FindReclaimCandidates(ctx, s.Clock.Now(), limit)
	if err != nil {
		return nil, err
	}
	completed := make([]domain.AnalysisRun, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.LeaseExpiresAt == nil {
			continue
		}
		reclaimed, reclaimErr := s.Repository.ReclaimExpiredRun(ctx, candidate.RunID, candidate.AttemptNo, candidate.LeaseOwner, *candidate.LeaseExpiresAt, candidate.State, s.Config.WorkerID, s.Clock.Now(), time.Duration(s.Config.LeaseSeconds)*time.Second)
		if errors.Is(reclaimErr, runs.ErrConditional) {
			continue
		}
		if reclaimErr != nil {
			return completed, reclaimErr
		}
		processed, processErr := s.Process(ctx, reclaimed.RunID)
		if processErr != nil {
			s.logRunError("reclaimed run processing failed", reclaimed, processErr)
			return completed, processErr
		}
		completed = append(completed, processed)
	}
	return completed, nil
}

func (s *Service) logRunError(message string, run domain.AnalysisRun, err error) {
	if s.Logger == nil {
		return
	}
	attrs := []any{
		"run_id", run.RunID,
		"attempt_no", run.AttemptNo,
		"worker_id", s.Config.WorkerID,
		"state", run.State,
		"error", err,
	}
	if run.CommitOID != "" {
		attrs = append(attrs, "commit_oid", run.CommitOID)
	}
	s.Logger.Error(message, attrs...)
}

func (s *Service) WorkerLoop(ctx context.Context) {
	s.workerReady.Store(true)
	defer s.workerReady.Store(false)
	interval := time.Duration(s.Config.WorkerPollSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.ProcessQueuedAndReclaim(ctx, 16); err != nil && s.Logger != nil {
			s.Logger.Error("worker poll failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
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
