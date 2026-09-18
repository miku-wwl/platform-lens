# PlatformLens Stage 1 LocalStack Cloud Acceptance

## Result

`PARTIAL — environment verification only`

The Stage 1H runtime contract, LocalStack DynamoDB/S3 behavior, readiness, manifest authority, heartbeat handling, two-process batch, and real process-kill recovery passed. The remaining PARTIAL status is limited to capabilities unavailable or not enabled in the current local environment: LocalStack IAM enforcement/fault injection and the Windows race-detector compiler.

No real AWS account was used.

## Stage 1H AWS Runtime Contract

- Backend selection is explicit: `PLATFORMLENS_BACKEND=local` selects SQLite/Filesystem; `PLATFORMLENS_BACKEND=aws` selects DynamoDB/S3. Endpoint presence no longer selects the backend.
- `internal/cloudaws` owns one AWS SDK v2 configuration policy for region, optional endpoint, default credential chain, and bounded retry attempts. The default is three attempts and `PLATFORMLENS_AWS_MAX_ATTEMPTS` is configurable.
- Production DynamoDB/S3 constructors contain no hard-coded LocalStack credentials. Test harnesses set explicit credentials through environment variables.
- Runtime constructors do not call `CreateTable` or `CreateBucket`. Terraform owns the table, GSI, bucket, worker identity, policy, and access key. Missing resources fail readiness/startup.
- `/readyz` checks repository readiness, artifact readiness, and worker-loop acceptance. `/healthz` remains lightweight.
- S3 `Exists` returns `(false, nil)` only for not-found. Access denied, throttling, 5xx, and transport failures remain errors.
- S3 `Put` returns the canonical `s3://<bucket>/runs/<run_id>/attempts/<attempt>/<artifact>` URI. The manifest is written, read back byte-for-byte, hashed from the read-back bytes, and only then passed to conditional `CompleteRun`.
- Focused configuration, AWS classification, readiness, S3 semantics, heartbeat, constructor, and manifest-failure tests passed.
- LocalStack constructor non-provisioning checks passed for DynamoDB and S3.

## IAM Acceptance

LocalStack runtime observed:

- Image: `localstack/localstack-pro:dev`
- Build: `2026.9.0.dev245` (`1e8dfc77`)
- Terraform apply: PASS; six resources applied, including the worker user, policy, attachment, and access key.
- Terraform policy scope: DynamoDB `DescribeTable/GetItem/PutItem/UpdateItem/Query` for the run table and candidate index; S3 `ListBucket` for the artifact bucket and `GetObject/PutObject` for the `runs/*` object prefix.
- No create/delete infrastructure or IAM administration actions are granted.

The current LocalStack container does not expose an `ENFORCE_IAM` setting. The gated test was first skipped with that explicit capability blocker. A follow-up probe used the Terraform-generated worker key and confirmed the positive `DescribeTable`/`HeadBucket` calls, but the worker was also allowed to create a forbidden DynamoDB table. The probe failed at that negative assertion, as expected when enforcement is absent; the temporary table was cleaned up.

`BLOCKED — current LocalStack IAM enforcement is not enabled`

No IAM PASS was invented. The negative S3 check and restricted-credential full E2E remain to be run after starting a LocalStack instance with a supported enforcement mechanism.

## Cloud Fault Acceptance

- Deterministic AWS SDK error classification passed for conditional, not-found, access-denied, retryable, and unknown failures.
- Heartbeat tests passed for transient retry-then-success and bounded persistent uncertainty cancellation.
- The process acceptance used an opt-in 3-second operation delay and passed with independent heartbeat scheduling.
- The current LocalStack instance has no configured, verified fault/chaos injection mechanism for DynamoDB/S3 5xx, throttling, outage, or network latency. LocalStack-specific fault injection is therefore `BLOCKED`; deterministic SDK-level coverage remains PASS.

## Multi-Worker Acceptance

- Independent OS worker processes: 2 at a time, with distinct worker IDs and shared LocalStack DynamoDB/S3.
- Batch: 20 submitted runs.
- Result: 20 completed, 0 failed.
- The acceptance verified terminal `winning_attempt`, canonical winning manifest URI, cleared terminal lease metadata, and zero duplicate-authority violations.
- Candidate discovery and claim races were exercised through the two independent worker loops and concurrent API processing.

## Process-Kill Recovery

PASS.

Worker A claimed the run and pinned commit A. The fixture branch then moved to commit B. Worker A was terminated with an external OS process kill, not graceful cancellation. After lease expiry, worker B reclaimed the run as attempt 2, replayed pinned commit A, completed the run, stored the attempt-2 manifest URI, set `winning_attempt=2`, and cleared terminal lease metadata.

## Race Detector

`PARTIAL — environment verification only`

- `go env CGO_ENABLED`: `0`
- `go test -race ./...`: blocked because race builds require CGO.
- With `CGO_ENABLED=1`, the toolchain reported `gcc` not found. No usable `gcc`, `clang`, `cc`, or `cl` was available on PATH.

No application semantics were changed to bypass the race detector.

## Existing Regression

- `go vet ./...`: PASS
- `go test ./...`: PASS
- Terraform `fmt -check` and `validate` for `infra/localstack`: PASS
- Terraform apply for LocalStack infrastructure: PASS
- TFLint live acceptance with locked `0.55.1` binary: PASS
- Kubeconform live acceptance with locked `0.6.7` binary: PASS
- SQLite/Filesystem E2E and manifest hash verification: PASS
- LocalStack DynamoDB/S3 E2E and manifest read-back hash verification: PASS
- LocalStack DynamoDB atomic lifecycle/CAS: PASS
- LocalStack constructor non-provisioning checks: PASS
- Readiness, S3 error classification, backend-mode, heartbeat, and manifest failure tests: PASS
- Multi-process 20-run and process-kill acceptance: PASS

## Remaining Blockers

1. Current LocalStack container IAM enforcement is not enabled; restricted-credential positive/negative API acceptance is blocked by that environment capability/configuration.
2. No verified LocalStack DynamoDB/S3 fault-injection mechanism is configured in the current instance; LocalStack-specific chaos scenarios are blocked.
3. Windows lacks a usable C compiler and has `CGO_ENABLED=0`; `go test -race ./...` remains environment-blocked.

These are not classified as application correctness failures.

## Scope Confirmation

- No real AWS was used.
- Stage 2 was not implemented.
- No Stage 3 reference-driven refactor was performed.
- No new PlatformLens product scope was added.
- The frozen v0.8.5 design documents were not modified.
- This turn does not commit or push changes.
