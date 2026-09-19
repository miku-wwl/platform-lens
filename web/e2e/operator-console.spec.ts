import { expect, test } from "@playwright/test";
import path from "node:path";

test("operator console completes a LocalStack-backed analysis journey", async ({ page }) => {
  const fixturePath = path.resolve(process.cwd(), "../fixtures/web-console-demo");
  const observedStates = new Set<string>();
  let detailReads = 0;
  page.on("request", (request) => {
    if (request.method() === "GET" && /\/analysis\/[a-f0-9]{32}$/.test(new URL(request.url()).pathname)) {
      detailReads += 1;
    }
  });

  await page.goto("/#/", { waitUntil: "networkidle" });
  await expect(page.getByRole("heading", { name: "PlatformLens Console" })).toBeVisible();
  await expect(page.getByText("Process health")).toBeVisible();
  await expect(page.getByText("Dependency readiness")).toBeVisible();
  await page.screenshot({ path: "test-results/stage2.5-dashboard.png", fullPage: true });

  await page.getByRole("button", { name: "New Analysis", exact: true }).click();
  await expect(page.getByRole("heading", { name: "New Analysis" })).toBeVisible();
  await page.getByLabel("Repository URL or approved local fixture path").fill(fixturePath);
  await page.getByLabel("Requested ref").fill("main");
  await page.getByRole("button", { name: "Submit analysis", exact: true }).click();

  await page.waitForFunction(() => /^#\/?runs\/[a-f0-9]{32}$/.test(location.hash));
  const runID = new URL(page.url()).hash.replace(/^#\/?runs\//, "");
  expect(runID).toMatch(/^[a-f0-9]{32}$/);

  await expect.poll(async () => {
    const state = (await page.locator(".run-hero strong").first().innerText()).trim();
    if (state) observedStates.add(state);
    return state;
  }, { timeout: 90000, intervals: [250, 1000, 3000] }).toBe("COMPLETED");
  expect(detailReads).toBeGreaterThan(0);

  await page.getByRole("button", { name: "Runs", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Runs" })).toBeVisible();
  await page.locator("select").selectOption("COMPLETED");
  await expect(page.getByText(runID.slice(0, 12), { exact: false }).first()).toBeVisible();
  await expect(page.locator("tbody .status-badge").filter({ hasText: "COMPLETED" }).first()).toBeVisible();

  await page.getByText(runID.slice(0, 12), { exact: false }).first().click();
  await expect(page.locator(".run-hero")).toBeVisible();
  for (const label of [
    "COMPLETED", "Attempt", "Winning attempt", "Requested ref", "Commit",
    "Analysis outcome", "Coverage", "Review", "Evaluation", "Validation results",
    "Diagnostics", "Evidence artifacts", "Terraform provenance", "Kubernetes results",
  ]) {
    await expect(page.getByText(label, { exact: true }).first()).toBeVisible();
  }
  await expect(page.locator(".report-viewer")).toContainText("PlatformLens Analysis Report");
  await expect(page.locator(".json-viewer").last()).toContainText("schema_version");
  await expect(page.locator(".json-viewer").last()).toContainText(runID);
  await page.screenshot({ path: "test-results/stage2.5-run-detail.png", fullPage: true });

  await page.reload({ waitUntil: "networkidle" });
  await expect(page.locator(".run-hero")).toBeVisible();
  await expect(page.getByText("COMPLETED", { exact: true }).first()).toBeVisible();

  await page.goto("/#runs/does-not-exist", { waitUntil: "networkidle" });
  await expect(page.locator(".alert.alert-bad")).toContainText("not found");
  const errorText = await page.locator("body").innerText();
  expect(errorText).not.toContain("goroutine");
  expect(errorText).not.toContain("panic:");

  expect(observedStates.has("COMPLETED")).toBe(true);
});
