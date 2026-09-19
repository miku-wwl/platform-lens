import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NewAnalysisPage } from "./NewAnalysisPage";

describe("NewAnalysisPage", () => {
  it("validates required submission fields before calling the API", () => {
    render(<NewAnalysisPage onCreated={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Submit analysis" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Repository URL is required.");
  });
});
