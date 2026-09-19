import type {
  AnalysisAccepted,
  AnalysisRequest,
  ArtifactListResponse,
  ArtifactPayload,
  HealthResponse,
  Run,
  RunListResponse,
  RunState,
  VersionInfo,
} from "./types";

const apiBase = (import.meta.env.VITE_API_BASE_URL ?? "").replace(/\/$/, "");

export class ApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

async function request(path: string, init?: RequestInit): Promise<Response> {
  let response: Response;
  try {
    response = await fetch(`${apiBase}${path}`, init);
  } catch {
    throw new ApiError("PlatformLens backend is unavailable.", 0);
  }
  if (!response.ok) {
    let message = `Request failed (${response.status}).`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      // Keep the bounded generic message when the server did not return JSON.
    }
    throw new ApiError(message, response.status);
  }
  return response;
}

async function json<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await request(path, init);
  return (await response.json()) as T;
}

export function getHealth(): Promise<HealthResponse> {
  return json<HealthResponse>("/healthz");
}

export function getReady(): Promise<HealthResponse> {
  return json<HealthResponse>("/readyz");
}

export function getVersion(): Promise<VersionInfo> {
  return json<VersionInfo>("/version");
}

export function listRuns(options: { limit?: number; offset?: number; state?: RunState; repository?: string } = {}): Promise<RunListResponse> {
  const query = new URLSearchParams();
  if (options.limit !== undefined) query.set("limit", String(options.limit));
  if (options.offset !== undefined) query.set("offset", String(options.offset));
  if (options.state) query.set("state", options.state);
  if (options.repository) query.set("repository", options.repository);
  const suffix = query.toString() ? `?${query.toString()}` : "";
  return json<RunListResponse>(`/analysis${suffix}`);
}

export function getRun(runID: string): Promise<Run> {
  return json<Run>(`/analysis/${encodeURIComponent(runID)}`);
}

export function createAnalysis(input: AnalysisRequest): Promise<AnalysisAccepted> {
  return json<AnalysisAccepted>("/analysis", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function listArtifacts(runID: string): Promise<ArtifactListResponse> {
  return json<ArtifactListResponse>(`/analysis/${encodeURIComponent(runID)}/artifacts`);
}

function artifactPath(runID: string, name: string): string {
  const encodedName = name.split("/").map((part) => encodeURIComponent(part)).join("/");
  return `/analysis/${encodeURIComponent(runID)}/artifacts/${encodedName}`;
}

export function getArtifact(runID: string, name: string): Promise<ArtifactPayload> {
  return json<ArtifactPayload>(artifactPath(runID, name));
}

export async function getReport(runID: string): Promise<string> {
  const response = await request(`/analysis/${encodeURIComponent(runID)}/report`);
  return response.text();
}

export function getManifest(runID: string): Promise<ArtifactPayload> {
  return json<ArtifactPayload>(`/analysis/${encodeURIComponent(runID)}/manifest`);
}
