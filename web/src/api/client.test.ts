import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, createAnalysis, getArtifact, listRuns } from "./client";

afterEach(() => vi.restoreAllMocks());

describe("API client", () => {
  it("encodes bounded list filters and decodes the backend response", async () => {
    const response = new Response(JSON.stringify({ runs: [], limit: 10, offset: 0, has_more: false }), { status: 200, headers: { "Content-Type": "application/json" } });
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(response);
    await listRuns({ limit: 10, offset: 0, state: "COMPLETED", repository: "https://example.test/repo.git" });
    expect(fetchMock).toHaveBeenCalledWith("/analysis?limit=10&offset=0&state=COMPLETED&repository=https%3A%2F%2Fexample.test%2Frepo.git", undefined);
  });

  it("posts the actual analysis request schema", async () => {
    const response = new Response(JSON.stringify({ run_id: "run-1", state: "QUEUED" }), { status: 202 });
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(response);
    await createAnalysis({ repository_url: "https://example.test/repo.git", requested_ref: "main" });
    expect(fetchMock).toHaveBeenCalledWith("/analysis", expect.objectContaining({ method: "POST", body: JSON.stringify({ repository_url: "https://example.test/repo.git", requested_ref: "main" }) }));
  });

  it("reports bounded server errors without exposing a stack trace", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ error: "not found" }), { status: 404 }));
    await expect(getArtifact("run-1", "../secret")).rejects.toBeInstanceOf(ApiError);
  });
});
