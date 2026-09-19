import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import * as api from "../api/client";
import { RunsPage } from "./RunsPage";

vi.mock("../api/client", () => ({ listRuns: vi.fn() }));

describe("RunsPage", () => {
  it("shows a safe empty state from an empty bounded response", async () => {
    vi.mocked(api.listRuns).mockResolvedValue({ runs: [], limit: 25, offset: 0, has_more: false });
    render(<RunsPage onSelectRun={vi.fn()} />);
    await waitFor(() => expect(screen.getByText("No runs match the current view.")).toBeInTheDocument());
  });
});
