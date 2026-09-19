export type RunState =
  | "QUEUED"
  | "CLAIMED"
  | "RETRIEVING"
  | "VALIDATING"
  | "REVIEWING"
  | "EVALUATING"
  | "PERSISTING"
  | "COMPLETED"
  | "FAILED";

export const lifecycleStates: RunState[] = [
  "QUEUED",
  "CLAIMED",
  "RETRIEVING",
  "VALIDATING",
  "REVIEWING",
  "EVALUATING",
  "PERSISTING",
  "COMPLETED",
];

export type Run = {
  run_id: string;
  repository_url: string;
  requested_ref: string;
  resolved_ref?: string;
  ref_type?: string;
  commit_oid?: string;
  requested_path?: string;
  state: RunState;
  attempt_no: number;
  lease_active?: boolean;
  analysis_outcome?: string;
  coverage_status?: string;
  review_status?: string;
  evaluation_status?: string;
  manifest_uri?: string;
  manifest_hash?: string;
  winning_attempt?: number;
  failure_code?: string;
  failure_message?: string;
  created_at: string;
  updated_at: string;
};

export type RunListResponse = {
  runs: Run[];
  limit: number;
  offset: number;
  has_more: boolean;
};

export type ArtifactSummary = {
  name: string;
  sha256: string;
  size: number;
  sensitivity: string;
};

export type ArtifactListResponse = {
  run_id: string;
  attempt_no: number;
  artifacts: ArtifactSummary[];
};

export type VersionInfo = Record<string, string>;

export type HealthResponse = { status: string; error?: string };

export type AnalysisRequest = {
  repository_url: string;
  requested_ref: string;
  requested_path?: string;
};

export type AnalysisAccepted = { run_id: string; state: RunState };

export type ArtifactPayload = unknown;
