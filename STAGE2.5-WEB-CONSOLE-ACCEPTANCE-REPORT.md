# PlatformLens Stage 2.5 Web Console Acceptance Report

## Final Status

`PASS / FREEZE`

The React/TypeScript/Vite console and the minimum presentation API are
implemented and pass their automated gates. The final closeout now includes a
new real Microsoft Edge/Playwright browser run against the LocalStack-backed Go
API. No real AWS was used. The current closeout does not change the frozen
backend semantics or begin Stage 3.

## Frontend Stack

- React `19.3.0`
- TypeScript `7.0.2`
- Vite `8.3.0`
- Vitest `5.0.1`
- React Testing Library `16.3.3`
- jsdom `30.1.0`
- `@vitejs/plugin-react` `6.1.1`
- No AWS SDK, database client, or LocalStack credential is present in `web/`.

## Pages

- Dashboard: implemented; health, readiness, version, bounded recent runs,
  state summary, completed/failed counts, and conservative recovery indicators.
- New Analysis: implemented; submits the existing
  `repository_url`, `requested_ref`, and optional `requested_path` schema.
- Runs: implemented; bounded backend list with state/repository filters and
  offset navigation.
- Run Detail: implemented; bounded polling, source identity, current-state
  lifecycle, attempt/recovery wording, result/AI status, diagnostics, evidence,
  validation, Terraform provenance, Kubernetes discovery, artifacts, report,
  and manifest sections.

## Backend API Changes

Added only read/presentation capabilities:

- `GET /analysis` — bounded list with `limit`, `offset`, `state`, and exact
  `repository` filters. SQLite orders by updated time; DynamoDB returns a
  bounded scan sorted in memory for the page.
- `GET /analysis/{run_id}/artifacts` — lists only artifacts named by the
  authoritative manifest.
- `GET /analysis/{run_id}/artifacts/{name...}` — reads only a manifest-
  allowlisted JSON/Markdown artifact name; traversal and arbitrary paths are
  rejected.
- `GET /analysis/{run_id}/report` — safe report artifact boundary.
- `GET /analysis/{run_id}/manifest` — verified manifest read-back boundary.

`GET /analysis/{run_id}` retains its existing response contract, including
active lease fields needed by the process acceptance harness. The worker now
persists `validation-results.json` as a presentation artifact. No claim,
renew, reclaim, fencing, pin, evidence, manifest authority, validation, AI,
Terraform provenance, or Kubernetes semantics changed.

## UI Features

- submission: PASS in component/API tests; direct HTTP submission to the
  LocalStack-backed Go API completed a Run.
- runs list: PASS in component tests and API presentation tests.
- lifecycle: PASS in component tests; only the persisted current state is
  highlighted, not fabricated historical transitions.
- diagnostics, evidence, AI findings, evaluation, Terraform provenance,
  Kubernetes results, artifacts, report, and manifest: implemented with safe
  empty/unavailable states and JSON/Markdown viewers.
- AI degraded mode: `COMPLETED` remains distinct from `UNAVAILABLE` and
  `NOT_APPLICABLE` review/evaluation statuses.

## Security Boundary

- Browser code calls only the Go HTTP API.
- No direct DynamoDB, S3, LocalStack, SQLite, filesystem, or workspace access
  exists in the frontend.
- Artifact reads are run-scoped, manifest-allowlisted, bounded, and reject
  traversal/arbitrary paths.
- Existing canonical source URL validation and evidence redaction remain in
  the backend.
- Repository-derived content is rendered as escaped text/JSON; raw HTML is
  never injected or executed.
- No AWS credentials, Git credentials, tokens, or secret environment values
  are put in frontend configuration.

## Frontend Tests

- `npm ci`: PASS.
- `npm run typecheck`: PASS.
- `npm test`: PASS — 4 files, 7 tests.
- `npm run build`: PASS — production Vite bundle generated.

## Backend Regression

- `go fmt ./...`: PASS.
- `git diff --check`: PASS; only normal Windows LF/CRLF conversion warnings.
- `go vet ./...`: PASS.
- `go test ./...`: PASS.
- `go test -race ./...`: PASS in the current closeout with the repository's
  LLVM/MinGW CGO setup.
- SQLite/API presentation tests: PASS.
- Terraform `fmt -check -recursive infra/localstack`: PASS.
- Terraform `-chdir=infra/localstack validate`: PASS.
- Live TFLint `0.55.1`: PASS.
- Live Kubeconform `0.6.7`: PASS.
- LocalStack DynamoDB/S3 E2E: PASS.
- LocalStack restricted IAM and negative checks: PASS.
- DynamoDB CAS/concurrency: PASS.
- Manifest verification/read-back: PASS.
- Targeted process-kill/multi-process acceptance after the API compatibility
  fix: PASS — 20/20 completed and process-kill attempt 1→2 replayed the pinned
  commit.
- One later full-suite Windows run had a single transient `source-cache/*.lock`
  `Access is denied` during the 20-run batch (19/20); no test was weakened and
  the targeted acceptance rerun was 20/20 PASS.

## Closeout Compatibility Fixes

- The LocalStack restricted worker policy now includes the minimum read-only
  `dynamodb:Scan` permission required by the existing presentation list
  endpoint. No write, lifecycle, fencing, or authority permission was added.
- The Dynamo presentation list now follows all DynamoDB `Scan` pages before
  sorting by authoritative `updated_at`/`run_id`, then returns the existing
  bounded limit/offset page. This fixes the first-scan-page omission without
  changing run semantics.
- The browser acceptance harness hash assertion was corrected to accept the
  existing application's `#runs/<run_id>` hash route. This is test plumbing,
  not product routing.
- After the permission and pagination correction, targeted LocalStack IAM
  negative checks and DynamoDB/S3 E2E both passed.

## LocalStack Demo Evidence

The direct local integration path was verified with LocalStack Ultimate,
Terraform-provisioned DynamoDB/S3, the Go API, and Vite serving the frontend:

- Vite `/`: HTTP 200.
- Go `/healthz`: `ok`.
- Go `/readyz`: HTTP 200.
- UI-shaped `POST /analysis`: accepted `QUEUED`.
- Final Run: `COMPLETED`, attempt 1, winning attempt 1.
- Artifact catalog: 23 artifacts including `validation-results.json`.
- Report endpoint: HTTP 200.
- Manifest endpoint: schema version 1.

This proves the browser-facing API path, but not a browser interaction.

## UI E2E (Historical Exploratory Evidence)

- Submit Run from actual browser UI: `PASS`.
- Observe lifecycle in actual browser UI: `PASS`; observed `CLAIMED` then
  `COMPLETED` without slowing production code.
- View completed result/report/manifest in actual browser UI: `PASS`.
- Runs state filtering and attempt/winning-attempt display: `PASS`.
- Reload/direct Run Detail navigation: `PASS`.
- Safe nonexistent-run error state: `PASS`; rendered `not found` without a
  raw stack trace.
- Backend process-kill recovery: `PASS`; the browser closeout itself used the
  normal deterministic run, while the existing process-kill regression also
  passed independently.

## Previous Browser Surface Investigation

- Requested VS Code browser plugin: `BLOCKED` — no callable VS Code browser
  connector or browser surface was available in this environment.
- VS Code installed: `1.135.0`.
- Browser-like VS Code extensions detected: none.
- Computer Use application/browser inventory: empty.
- Browser engine, headed/headless mode, and screenshots for the requested VS
  Code plugin flow: not available.
- This historical VS Code-plugin investigation is superseded by the actual
  browser closeout below. It is retained so the earlier `BLOCKED` evidence is
  not rewritten.

## Previous Exploratory Chrome Evidence (Historical; Not Final Acceptance)

- Browser: Google Chrome `153.0.8010.52`.
- Automation: `playwright-core 1.55.0`, headless Chromium-compatible Chrome on
  Windows.
- URL: `http://127.0.0.1:5173` with Vite proxying to the Go API on
  `127.0.0.1:8000`.
- Dashboard: `PASS` — identity, health, readiness, version, and recent-run
  surface rendered.
- New Analysis: `PASS` — submission was performed through the form, not a
  direct `POST /analysis` call; run ID was
  `16023787bf338ab49243e98273f99585`.
- Active run: `PASS` — browser polling observed `CLAIMED` and
  `COMPLETED`; seven browser run reads were observed.
- Runs: `PASS` — `COMPLETED` filter showed the submitted run, ref `main`, and
  `1 / win 1`.
- Run Detail: `PASS` — source identity, commit, result/AI statuses,
  validation, diagnostics, evidence, Terraform and Kubernetes sections
  rendered.
- Report: `PASS` through the safe backend report endpoint.
- Manifest: `PASS` through the safe backend manifest endpoint; visible
  `schema_version` and run identity were verified.
- Error state: `PASS` for a nonexistent run; safe `not found` UI and no
  `goroutine` or `panic:` text.
- Reload/direct navigation: `PASS` — completed state reconstructed after
  reload.
- Screenshots and result evidence were written under the ignored
  `.platformlens-ui-e2e/browser-evidence/` directory; they are not product
  artifacts.

The earlier exploratory browser plumbing is retained only as history and is
not used for the final freeze decision.

## Final Playwright Browser Closeout

- Browser framework: Playwright Test `1.55.0`.
- Browser engine: Microsoft Edge `153.0.4234.32`, selected through
  Playwright `channel: "msedge"`.
- Mode and OS: headless on Windows.
- Test command: `npm run test:e2e` after a clean `npm ci`.
- Suite: `web/e2e/operator-console.spec.ts`, one narrow operator journey.
- Final result: `1 passed` in `30.7s`.
- Playwright-managed Chromium installation was attempted first, but Windows
  extraction did not produce the expected executable; the documented Edge
  fallback was used successfully. No Chrome was used for this final run.
- Dashboard: `PASS` — identity, version area, health, readiness, and recent
  runs rendered.
- New Analysis: `PASS` — the actual form submitted the deterministic local
  fixture and navigated to the existing `#runs/<run_id>` route.
- Active run/polling: `PASS` — browser polling observed the authoritative
  terminal `COMPLETED` state.
- Runs: `PASS` — the submitted run appeared with the `COMPLETED` state and
  the implemented state filter.
- Run Detail: `PASS` — run identity, attempt/winning attempt, source/ref/
  commit, result/coverage/review/evaluation, validation, diagnostics, evidence,
  Terraform, and Kubernetes sections rendered.
- AI mode: `PASS` — deterministic reviewer/evaluator statuses rendered as
  completed; no live LLM was required.
- Report: `PASS` — report content rendered through the Go presentation API.
- Manifest: `PASS` — schema version, run identity, commit, and artifact entries
  rendered through the safe backend endpoint.
- Reload/direct navigation: `PASS` — reloading the `#runs/<run_id>` detail route
  reconstructed the authoritative run.
- Error state: `PASS` — nonexistent run showed a clean `not found` state with
  no Go stack trace, panic, or raw storage path dump.
- LocalStack-backed UI submission: `PASS` — backend used LocalStack
  DynamoDB/S3 and restricted worker credentials; real AWS was not used.
- Browser screenshots: `web/test-results/stage2.5-dashboard.png` and
  `web/test-results/stage2.5-run-detail.png`; generated report/cache paths are
  ignored and are not product artifacts.

## Source Cache Investigation

- Original symptom: one full-suite Windows run reported
  `source-cache/*.lock: Access is denied` in a 20-run batch; the targeted
  rerun immediately afterward completed 20/20.
- Investigation scope: three default Windows-temp runs plus three controlled
  short-temp-root runs of
  `TestLocalStackMultiProcessAndProcessKillAcceptance` using LocalStack,
  restricted worker credentials, process-kill recovery, and the existing
  repository lock/worktree cleanup.
- Default-root result: one run passed; two later runs left one run in
  `RETRIEVING` before the bounded wave timeout. The exact
  `source-cache/*.lock Access is denied` message did not recur, and all
  acceptance ports/processes were cleaned up.
- Controlled diagnosis: an intentionally deep temp root reproduced Git's
  `worktrees/.../refs: Filename too long`; a short root `C:\pltmp-platformlens`
  then passed 3/3, with each run completing process-kill attempt 1→2 and a
  20/20 batch with 0 failed, 0 reclaims, and 0 duplicate-authority
  violations.
- Classification: `NON-REPRODUCIBLE` as the original lock-access symptom,
  with bounded evidence of Windows temp-path-length/file-handle timing
  sensitivity. No lock weakening or architecture change was made.
- Current closeout targeted rerun after the final frontend/browser changes:
  process-kill and 20-run multi-process acceptance `PASS` (20/20, 0 failed,
  0 reclaims, 0 duplicate-authority violations).

## Remaining Limitations

- The pre-existing Windows local-fixture behavior remains documented: default
  relative workspace paths failed marker creation, while the documented
  absolute directory configuration succeeded. It did not block the final
  browser closeout.
- Stage 3 real-AWS validation remains intentionally not started.

## Scope Confirmation

- No real AWS was used.
- No Stage 3 implementation was started.
- No authentication, RBAC, multi-tenancy, accounts, billing, notifications,
  WebSocket, SSE, GraphQL, Next.js, SSR, mobile app, or micro-frontend was
  added.
- No new AWS service was added.
- No browser-to-cloud/database path was added.
- No architecture redesign was made.
- The frozen v0.8.7 design documents were not modified.
- No commit, push, or tag was created.
