package domain

import "time"

type RunState string

const (
	StateQueued     RunState = "QUEUED"
	StateClaimed    RunState = "CLAIMED"
	StateRetrieving RunState = "RETRIEVING"
	StateValidating RunState = "VALIDATING"
	StateReviewing  RunState = "REVIEWING"
	StateEvaluating RunState = "EVALUATING"
	StatePersisting RunState = "PERSISTING"
	StateCompleted  RunState = "COMPLETED"
	StateFailed     RunState = "FAILED"
)

func (s RunState) Active() bool {
	switch s {
	case StateClaimed, StateRetrieving, StateValidating, StateReviewing, StateEvaluating, StatePersisting:
		return true
	default:
		return false
	}
}

type RefType string

const (
	RefCommit RefType = "COMMIT"
	RefHead   RefType = "HEAD"
	RefBranch RefType = "BRANCH"
	RefTag    RefType = "TAG"
)

type AnalysisOutcome string

const (
	OutcomeNoFindings   AnalysisOutcome = "NO_FINDINGS"
	OutcomeFindings     AnalysisOutcome = "FINDINGS"
	OutcomeInconclusive AnalysisOutcome = "INCONCLUSIVE"
)

type CoverageStatus string

const (
	CoverageNone     CoverageStatus = "NONE"
	CoveragePartial  CoverageStatus = "PARTIAL"
	CoverageComplete CoverageStatus = "COMPLETE"
)

type ReviewStatus string

const (
	ReviewNotApplicable ReviewStatus = "NOT_APPLICABLE"
	ReviewCompleted     ReviewStatus = "COMPLETED"
	ReviewUnavailable   ReviewStatus = "UNAVAILABLE"
)

type EvaluationStatus string

const (
	EvaluationNotApplicable EvaluationStatus = "NOT_APPLICABLE"
	EvaluationCompleted     EvaluationStatus = "COMPLETED"
	EvaluationUnavailable   EvaluationStatus = "UNAVAILABLE"
)

type ValidationStatus string

const (
	ValidationPass                 ValidationStatus = "PASS"
	ValidationFail                 ValidationStatus = "FAIL"
	ValidationError                ValidationStatus = "ERROR"
	ValidationSkipped              ValidationStatus = "SKIPPED"
	ValidationSkippedUnsupported   ValidationStatus = "SKIPPED_UNSUPPORTED"
	ValidationSkippedSchemaMissing ValidationStatus = "SKIPPED_SCHEMA_MISSING"
)

type EvidenceType string

const (
	EvidenceDiagnostic EvidenceType = "DiagnosticEvidence"
	EvidenceSource     EvidenceType = "SourceExcerptEvidence"
	EvidenceTool       EvidenceType = "ToolExecutionEvidence"
)

type EvaluationVerdict string

const (
	VerdictSupported    EvaluationVerdict = "SUPPORTED"
	VerdictPartial      EvaluationVerdict = "PARTIAL"
	VerdictUnsupported  EvaluationVerdict = "UNSUPPORTED"
	VerdictNotEvaluated EvaluationVerdict = "NOT_EVALUATED"
)

type AnalysisRun struct {
	RunID            string            `json:"run_id"`
	RepositoryURL    string            `json:"repository_url"`
	RequestedRef     string            `json:"requested_ref"`
	ResolvedRef      string            `json:"resolved_ref,omitempty"`
	RefType          RefType           `json:"ref_type,omitempty"`
	CommitOID        string            `json:"commit_oid,omitempty"`
	RequestedPath    string            `json:"requested_path,omitempty"`
	State            RunState          `json:"state"`
	AttemptNo        int               `json:"attempt_no"`
	LeaseOwner       string            `json:"lease_owner,omitempty"`
	LeaseAcquiredAt  *time.Time        `json:"lease_acquired_at,omitempty"`
	LeaseExpiresAt   *time.Time        `json:"lease_expires_at,omitempty"`
	AnalysisOutcome  *AnalysisOutcome  `json:"analysis_outcome,omitempty"`
	CoverageStatus   *CoverageStatus   `json:"coverage_status,omitempty"`
	ReviewStatus     *ReviewStatus     `json:"review_status,omitempty"`
	EvaluationStatus *EvaluationStatus `json:"evaluation_status,omitempty"`
	ManifestURI      string            `json:"manifest_uri,omitempty"`
	ManifestHash     string            `json:"manifest_hash,omitempty"`
	WinningAttempt   *int              `json:"winning_attempt,omitempty"`
	FailureCode      string            `json:"failure_code,omitempty"`
	FailureMessage   string            `json:"failure_message,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type ResourceIdentity struct {
	APIVersion    string `json:"api_version"`
	Kind          string `json:"kind"`
	Namespace     string `json:"namespace,omitempty"`
	Name          string `json:"name,omitempty"`
	File          string `json:"file"`
	DocumentIndex int    `json:"document_index"`
}

type Diagnostic struct {
	DiagnosticID string            `json:"diagnostic_id"`
	Producer     string            `json:"producer"`
	TargetID     string            `json:"target_id,omitempty"`
	Resource     *ResourceIdentity `json:"resource_identity,omitempty"`
	Severity     string            `json:"severity,omitempty"`
	RuleCode     string            `json:"rule_code,omitempty"`
	Message      string            `json:"message"`
	File         string            `json:"file,omitempty"`
	StartLine    int               `json:"start_line,omitempty"`
	EndLine      int               `json:"end_line,omitempty"`
	RawOutputRef string            `json:"raw_output_ref,omitempty"`
}

type ValidationStep struct {
	StepID   string `json:"step_id"`
	Kind     string `json:"kind"`
	TargetID string `json:"target_id,omitempty"`
	Required bool   `json:"required"`
	Producer string `json:"producer"`
}

type ValidationPlan struct {
	SchemaVersion int              `json:"schema_version"`
	ScopeChecks   []ValidationStep `json:"scope_checks"`
	Preparations  []ValidationStep `json:"preparations"`
	Validators    []ValidationStep `json:"validators"`
}

type ValidationResult struct {
	StepID          string            `json:"step_id"`
	StepKind        string            `json:"step_kind"`
	Producer        string            `json:"producer"`
	ProducerVersion string            `json:"producer_version,omitempty"`
	TargetID        string            `json:"target_id,omitempty"`
	Resource        *ResourceIdentity `json:"resource_identity,omitempty"`
	Required        bool              `json:"required"`
	Status          ValidationStatus  `json:"status"`
	DurationMS      int64             `json:"duration_ms"`
	ExitCode        *int              `json:"exit_code,omitempty"`
	Diagnostics     []Diagnostic      `json:"diagnostics,omitempty"`
	RawOutputRef    string            `json:"raw_output_ref,omitempty"`
}

type ToolExecution struct {
	ExecutionID     string   `json:"execution_id"`
	Producer        string   `json:"producer"`
	ProducerVersion string   `json:"producer_version,omitempty"`
	Argv            []string `json:"argv"`
	ExitCode        *int     `json:"exit_code,omitempty"`
	DurationMS      int64    `json:"duration_ms"`
	StdoutRef       string   `json:"stdout_ref,omitempty"`
	StderrRef       string   `json:"stderr_ref,omitempty"`
	OutputTruncated bool     `json:"output_truncated"`
	TimedOut        bool     `json:"timed_out"`
	Cancelled       bool     `json:"cancelled"`
}

type TerraformTarget struct {
	TargetID           string   `json:"target_id"`
	RootPath           string   `json:"root_path"`
	DiscoveryMethod    string   `json:"discovery_method"`
	Files              []string `json:"files"`
	LockfilePresent    bool     `json:"lockfile_present"`
	SourceLockfileHash string   `json:"source_lockfile_hash,omitempty"`
}

type ProviderSelection struct {
	SourceAddress   string   `json:"source_address"`
	SelectedVersion string   `json:"selected_version,omitempty"`
	Constraints     string   `json:"constraints,omitempty"`
	PackageHashes   []string `json:"package_hashes,omitempty"`
}

type ModuleProvenance struct {
	TargetID             string `json:"target_id"`
	ModuleKey            string `json:"module_key"`
	DeclaredSource       string `json:"declared_source,omitempty"`
	DeclaredVersionOrRef string `json:"declared_version_or_ref,omitempty"`
	ResolvedVersion      string `json:"resolved_version,omitempty"`
	ResolvedLocalPath    string `json:"resolved_local_path,omitempty"`
	ResolvedVCSRevision  string `json:"resolved_vcs_revision,omitempty"`
	ContentTreeHash      string `json:"content_tree_hash,omitempty"`
	HashVersion          int    `json:"module_tree_hash_version"`
}

type TerraformDependencyProvenance struct {
	TargetID                 string              `json:"target_id"`
	SourceLockfilePresent    bool                `json:"source_lockfile_present"`
	SourceLockfileHash       string              `json:"source_lockfile_hash,omitempty"`
	EffectiveLockfileHash    string              `json:"effective_lockfile_hash,omitempty"`
	LockfileOrigin           string              `json:"lockfile_origin"`
	ProviderProvenanceStatus string              `json:"provider_provenance_status"`
	Providers                []ProviderSelection `json:"providers"`
	ModuleProvenanceStatus   string              `json:"module_provenance_status"`
	Modules                  []ModuleProvenance  `json:"modules"`
}

type EvidenceEnvelope struct {
	SchemaVersion   int            `json:"schema_version"`
	EvidenceID      string         `json:"evidence_id"`
	RunID           string         `json:"run_id"`
	AttemptNo       int            `json:"attempt_no"`
	CommitOID       string         `json:"commit_oid"`
	EvidenceType    EvidenceType   `json:"evidence_type"`
	Producer        string         `json:"producer"`
	ProducerVersion string         `json:"producer_version,omitempty"`
	ContentHash     string         `json:"content_hash"`
	CreatedAt       time.Time      `json:"created_at"`
	Payload         map[string]any `json:"payload"`
}

type Finding struct {
	FindingID      string            `json:"finding_id"`
	Title          string            `json:"title"`
	Observation    string            `json:"observation"`
	Interpretation string            `json:"interpretation,omitempty"`
	EvidenceIDs    []string          `json:"evidence_ids"`
	TargetID       string            `json:"target_id,omitempty"`
	Resource       *ResourceIdentity `json:"resource_identity,omitempty"`
}

type ReviewedFinding struct {
	Finding    Finding `json:"finding"`
	Confidence string  `json:"confidence"`
}

type ReviewOutput struct {
	SchemaVersion int               `json:"schema_version"`
	Findings      []ReviewedFinding `json:"findings"`
	Summary       string            `json:"summary"`
}

type EvaluationItem struct {
	FindingID   string            `json:"finding_id"`
	Verdict     EvaluationVerdict `json:"verdict"`
	Reason      string            `json:"reason"`
	EvidenceIDs []string          `json:"evidence_ids"`
}

type EvaluationOutput struct {
	SchemaVersion int              `json:"schema_version"`
	Items         []EvaluationItem `json:"items"`
}
