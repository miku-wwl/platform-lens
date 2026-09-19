# PlatformLens Stage 2 Refactor Plan

This plan is deliberately limited to behavior-preserving code polish. The
frozen v0.8.7 design documents, reference ZIP archives, state machine, source
pinning, CAS/fencing, evidence semantics, and AWS contract are not refactor
targets.

## S2-P0-01

- Priority: P0
- Current code: `internal/app/service.go`, `Service.Process`
- Problem: One method contains the complete attempt lifecycle, artifact writes,
  evidence construction, review/evaluation, manifest commit, and failure
  handling. The frozen order is correct but difficult to audit.
- Reference inspiration: project-echo lifecycle separation; architect-agent
  specialist workflow boundaries.
- Planned change: extract named private helpers for attempt setup, deterministic
  output persistence, evidence/review/evaluation, and manifest commit. Keep the
  existing calls, state transitions, failure codes, and context boundaries in
  the same order.
- Frozen behavior: claim → retrieving → pin/replay → validating → reviewing →
  evaluating → persisting → manifest read-back → conditional complete.
- Tests protecting it: `tests/core_test.go`, `tests/analysis_test.go`,
  `tests/e2e_test.go`, manifest failure tests, pinned replay tests, and the full
  deterministic suite.
- Risk: Medium; controlled by characterization tests and small commits in the
  working tree.
- Status: Applied

## S2-P0-02

- Priority: P0
- Current code: `internal/app/service.go` artifact persistence closure and
  manifest handling.
- Problem: JSON artifact writes are repeated inline and make it harder to see
  which bytes precede the manifest hash.
- Reference inspiration: project-echo explicit persistence stages; current
  v0.8.7 manifest contract.
- Planned change: add a private attempt artifact writer that normalizes the
  artifact URI, stores the exact bytes returned by JSON encoding, and exposes
  the existing relative artifact map to manifest construction.
- Frozen behavior: artifact names, JSON semantics, manifest pre-artifact set,
  exact manifest bytes, read-back comparison, and SHA256 remain unchanged.
- Tests protecting it: `internal/report/report_test.go`,
  `tests/manifest_failure_test.go`, manifest read-back/hash assertions, and
  full E2E.
- Risk: Medium; compare artifact paths and manifest bytes before/after.
- Status: Applied

## S2-P0-03

- Priority: P0
- Current code: `internal/app/service.go` heartbeat goroutine and retry loop.
- Problem: fencing loss, non-retryable failure, and bounded transient retry are
  interleaved in the goroutine body.
- Reference inspiration: project-echo recovery readability; ANZ pkg clock/test
  determinism.
- Planned change: extract small private retry/classification helpers and add
  table-driven characterization tests for conditional/fencing, retryable, and
  cancellation outcomes.
- Frozen behavior: authoritative fencing loss cancels the attempt immediately;
  retryable cloud failures get only the existing bounded retry window; no
  uncertain lease may complete a Run.
- Tests protecting it: `internal/app/heartbeat_test.go`, race suite, and
  process-kill/reclaim acceptance.
- Risk: Medium; no retry count, deadline, or cancellation policy change.
- Status: Applied

## S2-P1-01

- Priority: P1
- Current code: `internal/source/git.go` acquisition, resolution, and worktree
  helpers.
- Problem: fetch/resolve/verify stages and lock ownership are correct but
  densely arranged in one file, making the frozen source contract less obvious.
- Reference inspiration: golden-retriever retrieval/pinner responsibility
  split, used only as a conceptual boundary.
- Planned change: clarify private helper names/comments and isolate the
  repository-lock scope in a small internal helper where safe. Do not move
  durable pinning out of RunRepository or change selected-ref fetch behavior.
- Frozen behavior: isolated Git environment, fetch selected ref, local resolve,
  annotated-tag peel, full OID verification, CAS pin-once, pinned replay, and
  process-safe cache serialization.
- Tests protecting it: source security/ref tests in `tests/core_test.go` and
  `tests/hardening_test.go`, plus full E2E and process-kill acceptance.
- Risk: Medium; source behavior is security-sensitive.
- Status: Applied

## S2-P1-02

- Priority: P1
- Current code: `internal/runs/sqlite.go` shared scan/transaction helpers and
  `internal/runs/dynamo.go` conditional error mapping.
- Problem: persistence helpers are correct but row decoding, transaction
  boundaries, and conditional error identity are not as discoverable as they
  could be.
- Reference inspiration: project-echo database helper organization and
  conditional state updates.
- Planned change: improve helper naming, add focused tests for rollback and
  `errors.Is` identity, and make the two backend implementations' contract
  comments parallel. No SQL predicate or DynamoDB condition changes.
- Frozen behavior: every authoritative mutation remains conditional; reclaim
  checks expected attempt/owner/expiry/state; terminal states clear lease
  fields; DynamoDB GSI remains candidate discovery only.
- Tests protecting it: `tests/core_test.go`, `tests/dynamo_hardening_test.go`,
  `internal/runs/dynamo_constructor_test.go`, race suite, and LocalStack IAM.
- Risk: Medium; persistence semantics are protected by both local and cloud
  tests.
- Status: Applied (conservative subset)

## S2-P1-03

- Priority: P1
- Current code: `internal/runtime/config.go`, `cmd/platformlens/main.go`, and
  worker startup/logging.
- Problem: runtime ownership is mostly centralized, but service construction
  and worker log fields are sparse.
- Reference inspiration: ANZ pkg config, structured logging, health/readiness,
  and build metadata concepts.
- Planned change: keep environment parsing in `runtime`, add small construction
  helpers and consistent bounded structured fields at worker boundaries. Do
  not add a logging or health framework.
- Frozen behavior: existing environment variables, backend selection,
  LocalStack endpoint override, readiness checks, and `/version` response stay
  unchanged; secrets and raw tool/source data remain excluded from logs.
- Tests protecting it: `internal/runtime/config_test.go`, API tests, toolchain
  tests, and full Go regression.
- Risk: Low to medium; response and environment contracts are characterized.
- Status: Applied (structured logging subset; config/API ownership unchanged)

## S2-P2-01

- Priority: P2
- Current code: test fixtures under `internal/*/*_test.go` and `tests/`.
- Problem: a few acceptance fixtures and repeated setup paths obscure the
  contract each test protects.
- Reference inspiration: reference repositories' focused fixture tests and
  path-security tests.
- Planned change: improve test names/comments and add only narrowly shared
  helpers where they do not hide behavior. Retain real OS-process tests.
- Frozen behavior: no acceptance test is replaced by a mock; no live LocalStack
  or process-kill coverage is weakened.
- Tests protecting it: existing suites plus any newly focused characterization
  tests.
- Risk: Low.
- Status: Applied

## Execution Order

1. Establish characterization tests for the heartbeat and artifact/manifest
   boundaries.
2. Apply S2-P0-01 and S2-P0-02 in small edits; run focused tests and
   `go test ./...`.
3. Apply S2-P0-03; run focused tests and `go test -race ./...`.
4. Apply S2-P1-01 and S2-P1-02; rerun source, persistence, and race tests.
5. Apply S2-P1-03 and S2-P2-01 only if the focused diff remains behavior
   preserving.
6. Run the complete Stage 2 acceptance matrix and write
   `STAGE2-RE-FREEZE-REPORT.md`.

No commit, push, or tag is part of this plan.

## Implementation Outcome

The applied changes are intentionally smaller than the plan's maximum scope:

- `Service.Process` now delegates artifact persistence and the review/evaluation
  stages to private helpers while preserving the original phase order.
- Heartbeat retry classification is isolated in `internal/app/heartbeat.go` and
  retains the existing bounded retry window and cancellation behavior.
- Git cache-path derivation and branch/tag candidate construction are named
  private helpers; durable Run pinning remains in `RunRepository`.
- SQLite now creates a missing database parent directory, with a focused
  readiness test; conditional SQL/DynamoDB contracts were not changed.
- Worker failures now log bounded structured fields (`run_id`, `attempt_no`,
  `worker_id`, `state`, and optional `commit_oid`).
- Focused artifact, heartbeat, Git-reference, and SQLite characterization tests
  were added.

No planned item required a semantic change, and no rejected reference pattern
was introduced.
