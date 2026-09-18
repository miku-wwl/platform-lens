# PlatformLens Stage 1 Hardening and Acceptance Report

Date: 2026-09-19

## Result

**PARTIAL**.

All locally resolvable Stage 1 correctness gaps in the requested P0/P1 list were fixed and covered by tests. The result remains PARTIAL because the current machine does not have TFLint or Kubeconform installed for live binary verification, and `go test -race ./...` is blocked by `CGO_ENABLED=0` with no detected C compiler. No real AWS account was used.

The original `STAGE1-IMPLEMENTATION-REPORT.md` was not overwritten. The frozen design documents were not modified.

## P0 Fixes

### P0-1 Recovery, reclaim, and replay

- Original issue: `Service.Process` reacquired `requested_ref` for a reclaimed run, and repository reclaim methods were not reachable from a running worker path.
- Changed files: `internal/app/service.go`, `internal/source/git.go`, `cmd/platformlens/main.go`.
- Fix: reclaimed runs use `AcquirePinned` and the recorded full `commit_oid`; branch/tag resolution is skipped. `ProcessQueuedAndReclaim`, `RecoverExpired`, and `WorkerLoop` provide the minimal Stage 1 polling path. The HTTP service starts the worker loop.
- Test: `TestReclaimReplayUsesPinnedCommit` pins commit A, moves the branch to B, reclaims attempt 2, replays A, verifies an attempt-2 manifest, and rejects old-attempt completion.
- Result: PASS.

### P0-2 DynamoDB lost-update race

- Original issue: lifecycle mutations used `GetItem` followed by whole-record `PutItem`, allowing stale heartbeats or phase transitions to overwrite newer fields.
- Changed files: `internal/runs/dynamo.go`.
- Fix: lifecycle mutations now use `UpdateItem` with field-scoped `SET`/`REMOVE` expressions and conditional guards. Run fields are represented individually for consistent reads while the legacy payload remains a compatibility envelope.
- Test: `TestLocalStackDynamoAtomicLifecycleCAS` verifies heartbeat/phase concurrency, renew/reclaim fencing, and stale completion/failure behavior against LocalStack.
- Result: PASS with LocalStack.

### P0-3 Failure fencing

- Original issue: the error path reread the current run before failure handling and could use a newer attempt number.
- Changed files: `internal/app/service.go`.
- Fix: each processing attempt captures immutable `attemptNo` and `workerID`; `FailRun` is called with those values. A later read is only for reporting after the CAS attempt fails.
- Test: SQLite and LocalStack stale-attempt tests verify old failure/completion cannot mutate a reclaimed attempt; replay integration exercises the service path.
- Result: PASS.

### P0-4 Instance-unique worker ID

- Original issue: the default worker identity was the constant `local-worker`.
- Changed files: `internal/runtime/id.go`, `internal/runtime/config.go`.
- Fix: the default identity combines hostname, process ID, and random process-start material and remains stable in one `Config`/service instance.
- Test: `TestSourceBoundaryAndWorkerIdentity` verifies distinct generated identities and rejects the old constant.
- Result: PASS.

### P0-5 Remote HEAD/default branch

- Original issue: `HEAD` could resolve the local bare cache's initial symbolic branch instead of the remote repository default branch.
- Changed files: `internal/source/git.go`, `tests/core_test.go`.
- Fix: `git ls-remote --symref origin HEAD` determines the remote default branch before fetch and verification.
- Test: `TestGitFixture` uses a `main` fixture and verifies `HEAD` resolves `refs/heads/main`, while annotated tags, ambiguity, and short OID rejection remain covered.
- Result: PASS.

### P0-6 Source security boundary before persistence

- Original issue: the default configuration allowed local Git and accepted HTTP/credential-bearing forms before persistence.
- Changed files: `internal/runtime/config.go`, `internal/source/git.go`, `internal/app/service.go`.
- Fix: default `AllowLocalGit=false`; `Service.Submit` canonicalizes and validates before `CreateRun`. Only explicit local test/dev mode accepts a local path. HTTP, file, SSH, scp-style, user-info, and credential-query URLs are rejected.
- Test: `TestSourceBoundaryAndWorkerIdentity` and `TestServiceRejectsBeforePersistence` verify URL policy and zero persistence for rejected submissions.
- Result: PASS.

### P0-7 Kubeconform contract and one-result mapping

- Original issue: the adapter did not pass immutable schema configuration and could leave resources without a terminal result when output was partial or unusable.
- Changed files: `toolchain.lock`, `internal/validation/engine.go`, `internal/validation/engine_test.go`.
- Fix: invocation includes fixed Kubernetes version, immutable revision-based schema location, JSON, strict, and summary flags. Every discovered resource receives exactly one PASS, FAIL, ERROR, or SKIPPED_SCHEMA_MISSING result; missing/partial/malformed/process-error output is normalized conservatively.
- Test: `TestStructuredValidatorsAndInitGate` covers valid, invalid, missing-schema, mixed, malformed, and process-error cases with one result per resource.
- Result: PASS in deterministic adapter tests; live binary verification is BLOCKED by the missing executable.

### P0-8 Redaction before AI context

- Original issue: raw `ValidationOutput.Diagnostics` were passed to the reviewer alongside redacted evidence.
- Changed files: `internal/evidence/evidence.go`, `internal/validation/engine.go`, `internal/app/service.go`.
- Fix: diagnostics are normalized/redacted, evidence payloads and tool arguments are redacted, source excerpts are confined/bounded/redacted, and `BuildContext` constructs a redacted copy before reviewer invocation.
- Test: `TestRedactionHappensBeforeReviewerContext` verifies a fake token is absent from context and reviewer observations while redacted evidence is retained.
- Result: PASS.

## P1 Fixes

### P1-1 Toolchain enforcement

- Original issue: expected versions were displayed but not enforced, and expected versions could be recorded as observed versions.
- Changed files: `internal/runtime/toolchain.go`, `internal/runtime/config.go`, `internal/validation/engine.go`.
- Fix: actual executable versions are checked before execution; mismatch and missing/malformed output become structured execution errors unless explicit override is enabled. Provenance records the actual observed version or an empty value when unavailable.
- Test: `TestToolchainVerifyEnforcesObservedVersion` and the fake validator fixture cover exact/mismatch/missing/malformed/override behavior.
- Result: PASS in unit tests.

### P1-2 Source versus effective Terraform lockfile

- Original issue: provenance determined SOURCE/GENERATED from the post-init file, which could turn a generated lockfile into a false SOURCE result.
- Changed files: `internal/validation/provenance.go`.
- Fix: origin and source hash use discovery-time `TerraformTarget.LockfilePresent` and `SourceLockfileHash`; effective hash uses the post-init file.
- Test: `TestTerraformProvenanceSourceVsGeneratedAndModuleHash` covers both origins.
- Result: PASS.

### P1-3 Module tree hash in production provenance

- Original issue: `CanonicalModuleTreeHash` was not connected to module provenance.
- Changed files: `internal/validation/provenance.go`.
- Fix: `.terraform/modules/modules.json` is parsed, local module paths are confined to the workspace, and content tree hashes/version are recorded. VCS identity is not fabricated; status remains PARTIAL when identity is incomplete.
- Test: `TestTerraformProvenanceSourceVsGeneratedAndModuleHash` verifies a module content hash and PARTIAL identity status.
- Result: PASS.

### P1-4 Source excerpts in real evidence flow

- Original issue: source excerpt support existed as a helper but was not connected to diagnostics.
- Changed files: `internal/app/service.go`, `internal/evidence/evidence.go`.
- Fix: diagnostics with valid file/line locations create confined, bounded, hashed, redacted excerpt evidence; diagnostics without locations do not invent excerpts.
- Test: `TestRedactionHappensBeforeReviewerContext` verifies confined excerpt content and redaction; validator tests verify source locations are normalized.
- Result: PASS in local tests.

### P1-5 Structured Terraform diagnostics

- Original issue: `terraform validate -json` was reduced to `valid=true/false`.
- Changed files: `internal/validation/engine.go`.
- Fix: severity, summary/detail, file, line range, target, and producer are normalized into `Diagnostic` objects while preserving PASS/FAIL/ERROR status semantics.
- Test: `TestStructuredValidatorsAndInitGate` verifies structured location and redaction.
- Result: PASS in deterministic adapter tests.

### P1-6 Structured TFLint diagnostics

- Original issue: only TFLint exit code/raw output was captured.
- Changed files: `internal/validation/engine.go`.
- Fix: JSON issues are normalized with rule code, severity, message, file, and line range while retaining exit semantics.
- Test: `TestStructuredValidatorsAndInitGate` verifies issue normalization and actual producer version.
- Result: PASS in deterministic adapter tests; live binary verification is BLOCKED.

### P1-7 Init failure gates downstream validators

- Original issue: validate and TFLint ran after a failed Terraform init and produced misleading follow-on errors.
- Changed files: `internal/validation/engine.go`.
- Fix: target-dependent validators are represented as SKIPPED when init is not PASS.
- Test: `TestStructuredValidatorsAndInitGate` verifies no validate/TFLint marker is produced after init failure.
- Result: PASS.

### P1-8 Repository locking

- Original issue: an O_EXCL lock could remain permanently after a crash, and worktree mutations were not consistently under the repository lock.
- Changed files: `internal/source/git.go`, `internal/source/lock_process_unix.go`, `internal/source/lock_process_windows.go`.
- Fix: lock files record PID/host/start metadata; dead local owners can be recovered, live/unknown owners are retained conservatively. Cache fetch, worktree add/remove, and cleanup are locked.
- Test: `TestRepositoryLockAndWorkspaceSweeperAreConservative` covers live lock protection and stale-owner recovery.
- Result: PASS.

### P1-9 Conservative workspace sweeper

- Original issue: workspaces were deleted solely from directory mtime.
- Changed files: `internal/source/git.go`.
- Fix: workspaces require a marker and explicit ownership predicate before deletion; no predicate means no deletion. Active ownership can therefore be preserved conservatively.
- Test: `TestRepositoryLockAndWorkspaceSweeperAreConservative` preserves the active marker and removes only the known stale marker.
- Result: PASS.

## Concurrency Verification

- SQLite duplicate claim, stale phase/fail, pin-once, renew expiry mismatch, terminal lease cleanup: PASS.
- Reclaim/replay and stale old-attempt completion: PASS.
- DynamoDB heartbeat versus phase update: PASS against LocalStack; final state and lease both survive without stale whole-record regression.
- DynamoDB renew versus reclaim: PASS; exactly one CAS winner remains authoritative.
- DynamoDB old completion/failure after reclaim: PASS; both are rejected.
- LocalStack manifest completion path: PASS; terminal lease fields are cleared and manifest URI/hash are present.

## Recovery Verification

1. Run pins commit A from remote `HEAD`/`main`.
2. The source branch moves to commit B.
3. The original lease expires and attempt 2 is reclaimed.
4. Replay calls `AcquirePinned` with commit A and does not resolve `requested_ref`.
5. A fresh attempt-2 workspace/worktree is used and the run completes with commit A.
6. Old attempt 1 completion is rejected by CAS.

Evidence: `TestReclaimReplayUsesPinnedCommit` passed.

## Git Security Verification

- HTTPS boundary: PASS.
- HTTP, file, SSH, scp-style, credential-bearing user-info/query URLs: PASS rejection.
- Rejected source is not persisted: PASS.
- Remote HEAD/default branch: PASS for `main`.
- Annotated tag peeling, ambiguity, full OIDs, short OID rejection: PASS.
- Isolated Git config, hooks/template, prompt, and credential environment: PASS by source/runtime tests and allowlisted command environment.

## Validator Verification

| Area | Implemented | Unit-tested | Live-verified |
|---|---:|---:|---:|
| Terraform fmt/init/validate | YES | YES | Terraform 1.14.0 available; local E2E exercised it |
| Terraform structured diagnostics | YES | YES | NOT separately live-verified |
| TFLint | YES | YES with fake executable | BLOCKED: executable missing |
| Kubeconform | YES | YES with fake executable | BLOCKED: executable missing |
| Immutable Kubeconform schema location | YES | YES by adapter path | BLOCKED: no live binary/network schema run |

## Evidence / AI Verification

- Redaction before reviewer: PASS.
- Diagnostic, tool, and source excerpt evidence identity: PASS.
- Source excerpt confinement, bounded size, raw content hash, and redacted content: PASS.
- Invalid evidence identity produces UNSUPPORTED: PASS in existing evaluator tests.
- Semantic evaluator outage produces NOT_EVALUATED: PASS in existing evaluator tests.
- Reviewer unavailable degradation: PASS in the application path; AI artifacts are optional and run result remains explicit.

## Test Results

- `go fmt ./...`: PASS.
- `go vet ./...`: PASS.
- `go test ./...`: PASS.
- Test matrix: 17 tests listed under `tests`; additional package tests under `internal/runtime` and `internal/validation`.
- `go test -race ./...`: BLOCKED / NOT VERIFIED. Go reports `-race requires cgo`; this machine has `CGO_ENABLED=0` and no `gcc`, `clang`, or `cc` on PATH.
- SQLite/Filesystem E2E: PASS.
- LocalStack DynamoDB/S3 E2E: PASS.
- LocalStack atomic lifecycle/concurrency CAS: PASS.
- Git HEAD/default-branch and reclaim/replay integration: PASS.
- `git diff --check`: PASS after hardening changes.

## Remaining Environmental Blockers

- TFLint `0.55.1` is not installed; live TFLint fixture execution is NOT VERIFIED.
- Kubeconform `0.6.7` is not installed; live Kubeconform fixture execution is NOT VERIFIED.
- Race testing needs CGO enabled and a C compiler; no application workaround was introduced.

Terraform `1.14.0` and Go `1.24.2` are available and match the locked versions used by the implementation.

## Scope Confirmation

- No Stage 2 implementation.
- No Stage 3 reference-driven refactor or polishing.
- No real AWS use; LocalStack only.
- No frozen design document changes.
- No scope expansion.
- No commit or push performed for this hardening pass.
