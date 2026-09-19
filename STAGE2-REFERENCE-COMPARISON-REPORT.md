# PlatformLens Stage 2 Reference Comparison

## Summary

This report records the read-only comparison of the current PlatformLens backend
against the four reference archives supplied in the repository root. The
archives were used as engineering references only. No source file was copied
from an archive and no archive was modified.

Stage 2 is intentionally conservative. PlatformLens already has stronger
authoritative run semantics than several of the reference patterns: a durable
commit pin, conditional RunRepository mutations, attempt fencing, pinned
replay, and manifest-before-completion. Those contracts remain authoritative.

The comparison identified a small set of safe polish areas:

- split the large application orchestration method into explicit, testable
  responsibilities without changing its sequence;
- centralize repeated artifact JSON persistence and manifest read-back checks;
- make runtime/worker error and logging context more consistent;
- clarify Git acquisition helpers and keep repository-local serialization
  explicit;
- improve test fixture readability and characterization coverage around the
  refactors.

No new backend feature, AWS service, frontend, workflow engine, or persistence
semantics is proposed.

## ANZ golden-retriever

Reference repository: `golden-retriever-main.zip` (`golden-retriever`).
The archive did not contain a clear `LICENSE` or `COPYING` file in its root or
listed tree, so only concepts were considered.

| concept | current PlatformLens behavior | difference | decision | reason |
|---|---|---|---|---|
| Retriever/pinner responsibility split | `internal/source.Runtime` owns isolated Git CLI execution, cache setup, fetch, local resolution, verification, worktree creation, and cleanup. `internal/app.Service` performs the durable `PinSourceIfAbsent` write. | The current runtime has more responsibilities in one file, but the durable pin belongs to RunRepository rather than an in-memory pinner. | PARTIAL APPLY | Clarify helper boundaries and names; do not move durable pinning into an in-memory session object. |
| Fetch before resolve | Named refs are fetched under a cache lock, then resolved locally with explicit branch/tag ambiguity checks. | The reference has a generic `FetchRefOrAll` fallback; PlatformLens intentionally fetches selected refs and rejects ambiguity. | NO CHANGE | Selected-ref fetch, full OID validation, and ambiguity rejection are frozen security and reproducibility behavior. |
| Reference identity model | `HEAD`, branch, tag, and full commit OID are represented by `domain.RefType`; annotated tags are peeled with `^{commit}`. | PlatformLens rejects abbreviated OIDs and persists `commit_oid` per Run. | NO CHANGE | This is stricter than the reference and is required for pinned replay. |
| Repository synchronization | `AcquireFileLock` serializes cache Git operations across processes; worktree add/remove also uses the same repository lock. | The implementation uses a small lock-file protocol rather than the reference's in-memory `once` helper. | PARTIAL APPLY | Encapsulate lock acquisition/ownership details and improve cancellation/readability; retain process-safe locking. |
| Same-repository concurrency | Different Runs may share a cache while each Run pins its own commit and attempt. | A session cache/pinner can naturally imply one cached ref identity. | NO CHANGE | A shared in-memory pin would regress the durable per-Run pin contract. |
| Read-only acquisition versus materialization | Acquisition and worktree materialization are separate methods, but both live in `source/git.go`. | The reference separates retrieval and reader packages. | PARTIAL APPLY | Use internal helper grouping/comments and focused tests, without changing acquisition order or replay behavior. |

### Accepted golden-retriever inspiration

- explicit source identity naming;
- small helpers around fetch, resolve, verify, and materialization;
- repository-local serialization as a named responsibility;
- characterization tests for full OID, HEAD, branch/tag ambiguity, and pinned
  replay.

### Rejected golden-retriever patterns

- in-memory once-style source pinning as authoritative state;
- replacing Git CLI isolation with a different Git implementation;
- falling back to broad repository fetch as the normal path;
- allowing short OIDs or re-resolving a symbolic ref during recovery.

## CBA project-echo

Reference repository: `project-echo-main.zip` (`Project ECHO`). The archive
contains an MIT license.

| concept | current PlatformLens behavior | difference | decision | reason |
|---|---|---|---|---|
| Lifecycle organization | `internal/domain`, `internal/runs`, and `internal/app` model and execute the QUEUED → CLAIMED → ... → COMPLETED/FAILED lifecycle. | `app.Service.Process` is a long orchestration method. | APPLY | Extract named orchestration helpers while preserving the exact phase sequence and failure codes. |
| Conditional state writes | SQLite and DynamoDB use conditional writes; lease owner, attempt, expected state, and expected expiry are checked as applicable. | The reference has simpler state/timestamp conditions. | NO CHANGE | PlatformLens attempt fencing is stronger and must remain authoritative. |
| Stale work recovery | `FindReclaimCandidates` plus `ReclaimExpiredRun` uses expected attempt, owner, expiry, and state, then increments the attempt. | The reference primarily uses stale `processing_started_at`. | NO CHANGE | Timestamp-only recovery would permit stale claimants to mutate a newer attempt. |
| SQLite runtime hygiene | SQLite uses `database/sql`, WAL, busy timeout, `BEGIN IMMEDIATE`, context-aware calls, and a clock abstraction. | SQL schema and transaction helper are concentrated in `internal/runs/sqlite.go`. | APPLY | Extract small database/scan/time helpers and add focused characterization tests; preserve SQL predicates and transactions. |
| Path confinement | `internal/security/paths.go` performs existing-path and output-parent symlink confinement; artifact storage has its own safe path/key checks. | Similar safeguards are spread across source, evidence, and storage boundaries. | PARTIAL APPLY | Consolidate only shared error identity/helpers; do not weaken symlink or prefix checks. |
| Cleanup ownership | `Process` defers best-effort worktree cleanup; artifact retention is separate. | Cleanup ownership is easy to miss inside the long method. | APPLY | Introduce a named attempt cleanup boundary and keep cleanup best-effort after authoritative state handling. |
| Graceful cancellation | Contexts reach validators and command execution; process trees are terminated on cancellation. | Some background operations intentionally use `context.Background()` for failure reporting/cleanup. | PARTIAL APPLY | Make the intentional boundary explicit; do not let cleanup cancellation change failure persistence semantics. |

### Accepted project-echo inspiration

- explicit lifecycle responsibility boundaries;
- conditional persistence as the concurrency contract;
- reusable SQLite transaction and row-mapping helpers;
- path-security characterization tests;
- cleanup ownership documented at the attempt boundary.

### Rejected project-echo patterns

- replacing `attempt_no` fencing with timestamp-only stale recovery;
- importing media-pipeline-specific schema/migration behavior;
- introducing a second state machine or background recovery model.

## CBA architect-agent

Reference repository: `architect-agent-main.zip` (`Architect Agent`). The archive
contains an MIT license and explicitly warns that its workflow must not be run
on untrusted inputs or deployed as a secure/production system.

| concept | current PlatformLens behavior | difference | decision | reason |
|---|---|---|---|---|
| Specialist workflow boundaries | `internal/review`, `internal/evaluation`, and `internal/evidence` already expose typed Go interfaces and typed domain outputs. | Application orchestration still wires reviewer, deterministic validation, and semantic evaluation inline. | APPLY | Extract a clearly named review/evaluation stage helper without introducing a graph framework. |
| Structured outputs | Review and evaluation use typed Go structs and JSON artifacts. | Error/status classification is mostly expressed by existing domain statuses and generic errors. | PARTIAL APPLY | Add small typed/domain error boundaries where identity matters; retain serialized status values. |
| Independent evaluator | `DeterministicEvaluator.Validate` runs before `SemanticEvaluator.Evaluate`; fake implementations keep CI offline. | The order is visible in `Service.Process`, not as a named pipeline boundary. | APPLY | Name and test the deterministic-first boundary; do not permit semantic evaluation to invent deterministic facts. |
| Bounded retry | Current reviewer/evaluator interfaces do not add a workflow retry loop. | The reference workflow uses a graph retry policy, while its evaluator example also demonstrates a much larger LLM retry policy. | NO CHANGE | PlatformLens keeps bounded/optional AI behavior and must not adopt LangGraph or large retries. |
| Safe output paths | The reference attempts output confinement but uses a string-prefix check that is weaker than PlatformLens's `filepath.Rel` and symlink-aware checks. | PlatformLens has stronger path helpers. | NO CHANGE | Retain the stronger `internal/security` implementation; use only the general responsibility concept. |

### Accepted architect-agent inspiration

- typed specialist boundaries;
- explicit structured input/output validation;
- independent deterministic evaluation before semantic evaluation;
- keeping repository content as data rather than instructions.

### Rejected architect-agent patterns

- LangGraph or a new workflow engine;
- copying prompt/retry chains;
- large LLM retry budgets;
- the reference's weaker string-prefix output-path guard.

## ANZ pkg

Reference repository: `pkg-master.zip` (`anz-bank/pkg`). The archive contains
an Apache License 2.0.

| concept | current PlatformLens behavior | difference | decision | reason |
|---|---|---|---|---|
| Clock abstraction | `internal/runtime.Clock`, `RealClock`, and `FakeClock` are used by repositories and evidence. | A few non-authoritative source/command timing paths still call `time.Now` directly. | PARTIAL APPLY | Use the existing clock where it is already owned by a service/repository; avoid making process timing or lock diagnostics part of Run semantics. |
| Environment/config ownership | `internal/runtime.Config` parses PlatformLens environment variables and validates backend/limits. | A few API/CLI concerns remain in `cmd/platformlens`. | APPLY | Keep environment parsing in runtime and make construction boundaries more explicit; do not change environment names or backend selection. |
| Health/readiness/version | `internal/api` exposes `/healthz`, `/readyz`, and `/version`; readiness delegates to repository, storage, and worker state. | The API server is intentionally lightweight and does not need a separate health framework. | NO CHANGE | The reference's health package is useful as a concept, but a new framework would expand scope. |
| Structured logging | `log/slog` is used for startup and worker failures. | Worker logs do not consistently carry run/attempt/worker context. | APPLY | Add stable structured fields at the worker/orchestration boundaries; never log credentials, raw source, or raw CLI output. |
| Build metadata | `runtime.BuildInfo` combines version, commit, Go version, and toolchain hash. | It is currently a small local package rather than a global health state object. | NO CHANGE | Existing API contract is sufficient and avoids global mutable state. |
| Test time travel | `FakeClock` supports deterministic timestamps but not ticker/timer simulation. | The reference offers a richer context clock. | PARTIAL APPLY | Improve tests using the existing fake clock where useful; do not refactor heartbeat scheduling into a new clock framework in Stage 2. |

### Accepted ANZ pkg inspiration

- one clear runtime/config owner;
- structured logging fields;
- lightweight health/readiness/version composition;
- deterministic clock use at lifecycle persistence boundaries.

### Rejected ANZ pkg patterns

- importing a general-purpose enterprise logging/health framework;
- package-level mutable server state;
- changing API response schemas solely to mirror the reference.

## PlatformLens Independent Findings

The current code review found the following local maintainability issues that do
not require copying a reference implementation:

1. `internal/app/service.go` combines claim/replay, artifact persistence,
   evidence construction, review/evaluation, manifest commit, recovery, and
   heartbeat code in one file.
2. Artifact JSON writes are repeated inline and make the authoritative
   persistence sequence difficult to scan.
3. The heartbeat retry decision is correct but embedded in the goroutine body;
   a small named helper would make fencing-loss versus retryable-cloud-failure
   behavior easier to test.
4. `internal/source/git.go` contains the full Git runtime and lock protocol;
   helper naming and source-stage comments can make the frozen fetch/resolve/
   verify/pin order clearer without changing it.
5. SQLite row scanning and transaction boundaries are correct but deserve
   focused characterization tests around rollback and conditional failure.
6. Worker logs can consistently carry `run_id`, `attempt_no`, `worker_id`, and
   phase/state context.

## Changes Accepted

The companion refactor plan limits implementation to a small number of groups:

- explicit application pipeline helpers and a persistence writer;
- named heartbeat retry/error classification helpers;
- conservative Git/source responsibility cleanup;
- runtime logging/config construction cleanup;
- focused characterization tests and documentation of preserved contracts.

## Changes Rejected

- Web Console, React, TypeScript, Vite, or presentation APIs;
- real AWS resources or `infra/aws`;
- new AWS services, provisioning in runtime, or alternate AWS clients;
- LangGraph, new workflow engines, or large AI retry loops;
- replacement of durable RunRepository pinning/fencing;
- broad mechanical source copying from any archive;
- changes to frozen v0.8.7 design documents;
- adding the reference ZIP archives to source control.

## License / Clean-Room Boundary

The four ZIP archives remain read-only, untracked reference material:

- `golden-retriever-main.zip`: no clear license file was present in the listed
  archive tree; concepts only, no copied source.
- `project-echo-main.zip`: MIT; concepts only, no copied source.
- `architect-agent-main.zip`: MIT; concepts only, no copied source.
- `pkg-master.zip`: Apache-2.0; concepts only, no copied source.

All Stage 2 changes are independently written for PlatformLens. Reference
names are retained only in this report and the re-freeze report to explain the
engineering comparison. No reference code, prompts, assets, or license text is
being incorporated into PlatformLens.
