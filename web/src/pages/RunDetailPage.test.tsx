import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import * as api from "../api/client";
import type { Run } from "../api/types";
import { RunDetailPage } from "./RunDetailPage";

vi.mock("../api/client", () => ({
  ApiError: class ApiError extends Error { status = 0; },
  getRun: vi.fn(),
  listArtifacts: vi.fn(),
  getArtifact: vi.fn(),
  getReport: vi.fn(),
  getManifest: vi.fn(),
}));

const completed: Run = {
  run_id: "run-completed", repository_url: "https://example.test/repo.git", requested_ref: "main", resolved_ref: "refs/heads/main", ref_type: "BRANCH", commit_oid: "a".repeat(40), state: "COMPLETED", attempt_no: 2, winning_attempt: 2, review_status: "UNAVAILABLE", evaluation_status: "NOT_APPLICABLE", analysis_outcome: "NO_FINDINGS", coverage_status: "COMPLETE", created_at: "2026-09-19T00:00:00Z", updated_at: "2026-09-19T00:01:00Z",
};

function mockCompleted() {
  vi.mocked(api.getRun).mockResolvedValue(completed);
  vi.mocked(api.listArtifacts).mockResolvedValue({ run_id: completed.run_id, attempt_no: 2, artifacts: [
    { name: "validation-results.json", sha256: "x", size: 2, sensitivity: "INTERNAL" },
    { name: "report.md", sha256: "x", size: 2, sensitivity: "INTERNAL" },
    { name: "manifest.json", sha256: "x", size: 2, sensitivity: "INTERNAL" },
  ] });
  vi.mocked(api.getArtifact).mockResolvedValue([]);
  vi.mocked(api.getReport).mockResolvedValue("# report");
  vi.mocked(api.getManifest).mockResolvedValue({ schema_version: 1 });
}

describe("RunDetailPage", () => {
  it("renders completed degraded AI state and conservative recovery wording", async () => {
    mockCompleted();
    render(<RunDetailPage runID={completed.run_id} />);
    await waitFor(() => expect(screen.getByLabelText("Current lifecycle state: COMPLETED")).toBeInTheDocument());
    expect(screen.getAllByText("UNAVAILABLE").length).toBeGreaterThan(0);
    expect(screen.getByText(/indicating at least one prior attempt/)).toBeInTheDocument();
    expect(screen.getByText("VALIDATING")).toBeInTheDocument();
  });

  it("renders failed runs distinctly", async () => {
    const failed: Run = { ...completed, run_id: "run-failed", state: "FAILED", failure_code: "SOURCE_ERROR", failure_message: "source could not be retrieved", winning_attempt: undefined };
    vi.mocked(api.getRun).mockResolvedValue(failed);
    vi.mocked(api.listArtifacts).mockRejectedValue(new Error("not found"));
    render(<RunDetailPage runID={failed.run_id} />);
    await waitFor(() => expect(screen.getByText("SOURCE_ERROR")).toBeInTheDocument());
    expect(screen.getByText("source could not be retrieved")).toBeInTheDocument();
    expect(screen.getByLabelText("Current lifecycle state: FAILED")).toBeInTheDocument();
  });
});
