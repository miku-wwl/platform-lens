# PlatformLens — Brief Design v0.8.5 GO LOCALSTACK-HEAVY ROADMAP FREEZE

## 1. 项目定位

PlatformLens 是一个面向 Platform Engineering / SRE 的 **Evidence-Driven Infrastructure Analysis Platform**。

核心链路：

```text
Git Source
   ↓
Isolated Git Runtime
   ↓
Fetch + Local Resolve + Verify
   ↓
Pin Commit OID Once
   ↓
Fresh Fenced Attempt
   ↓
Target Discovery
   ↓
Deterministic Validation
   ↓
Diagnostics + Evidence
   ↓
Bounded AI Review
   ↓
Independent Evaluation
   ↓
Manifest-Committed Result
```

核心原则：

> **Immutable source. Deterministic facts. Fenced recovery. Evidence-backed AI. Auditable results.**

---


## 1.1 Go Implementation Baseline

V1 implementation language：

```text
Go
```

实现形态：

```text
single Go service / binary
cmd/platformlens
internal/*
```

核心 Go primitives：

```text
context.Context
goroutine / sync
os/exec
database/sql
net/http
log/slog
AWS SDK for Go v2
```

Git / Terraform / TFLint / Kubeconform 继续作为外部 CLI，由统一 `CommandRunner` 调用。

## 1.2 Delivery Boundary

v0.8.5 不改变 PlatformLens 核心架构，只重新划分 Stage 1 / Stage 2 责任：

```text
Stage 1 = thick LocalStack stage
         build + cloud-contract + resilience + IAM validation

Stage 2 = thin real-AWS stage
         short-lived smoke validation only
```

目标：

> **尽量在 LocalStack Ultimate 发现并修复云端逻辑问题，把真实 AWS 只留给不可替代的现实差异验证。**

---

## 2. Run Lifecycle

```text
QUEUED
→ CLAIMED
→ RETRIEVING
→ VALIDATING
→ REVIEWING
→ EVALUATING
→ PERSISTING
→ COMPLETED
```

初始化：

```text
attempt_no = 0
```

首次 claim：

```text
attempt_no = 1
```

Reclaim：

```text
attempt_no += 1
state = CLAIMED
fresh workspace
full replay
```

Lease 用于 liveness；`attempt_no` 用于 authoritative fencing。

Terminal state：

```text
COMPLETED / FAILED
→ clear lease metadata
```

---

## 3. Source Identity

```text
repo lock
→ fetch selected ref
→ local resolve / tag peel
→ verify commit
→ CAS pin commit_oid
```

一旦 pin：

```text
never resolve original symbolic ref again
```

V1 使用 isolated Git runtime，宿主 Git config / hooks / credential helpers 不得影响执行。

---

## 4. RunRepository Safety

所有 mutation 使用 conditional guard。

```text
update_phase:
attempt_no + lease_owner + expected_state

pin_source:
commit_oid IS NULL + current attempt/owner

complete_run:
current attempt/owner + state=PERSISTING

reclaim:
expected attempt/owner/state/lease expiry
```

---

## 5. Validation

Terraform：

```text
fmt       → Scope Check
init      → Preparation
validate  → Validator
TFLint    → Validator
```

Kubernetes：

```text
one kubeconform process
→ one terminal ValidationResult per discovered resource
```

Outcome：

```text
finding exists → FINDINGS
required supported ERROR → INCONCLUSIVE
effective_validation_count = 0 → INCONCLUSIVE
otherwise → NO_FINDINGS
```

---

## 6. Terraform Provenance

Terraform V1 baseline：

```text
Terraform >= 1.10
```

每个 Terraform target 单独记录：

```text
source lockfile hash
effective lockfile hash
lockfile origin
provider selections
provider package hashes
module provenance
module content tree hash
```

无 source lockfile 时：

```text
lockfile_origin = GENERATED
provenance status = PARTIAL
```

---

## 7. Evidence & AI

```text
ValidationResult
→ Diagnostic
→ Evidence
→ Redaction
→ Bounded Context
→ Reviewer
→ Deterministic Evaluator
→ Semantic Evaluator
```

```text
structurally invalid → UNSUPPORTED
structurally valid + semantic evaluator unavailable → NOT_EVALUATED
```

AI 不可用不破坏 deterministic result。

---

## 8. Manifest

Manifest：

```text
hash pre-manifest artifacts
→ build canonical JSON
→ write exact UTF-8 bytes
→ SHA256 exact bytes
→ conditional complete_run()
```

Canonical JSON：

```text
sorted object keys
stable array ordering
no insignificant whitespace
UTF-8
LF
no BOM
```

Manifest 不 hash 自己。

---

## 9. Runtime / CI

V1 包含：

```text
Go Clock interface + FakeClock
context-based cancellation
goroutine heartbeat
os/exec CommandRunner
database/sql
net/http API
log/slog structured logging
AWS SDK for Go v2
toolchain.lock enforcement
resource limits
health/readiness/version
workspace sweeper
```

Stage 1 acceptance 分两层：

```text
Fast deterministic gate
→ go fmt / vet / test / race
→ SQLite + fake-AI
→ validator fixtures

LocalStack Ultimate cloud gate
→ DynamoDB / S3
→ multi-worker CAS
→ crash / reclaim / replay
→ least-privilege IAM enforcement
→ bounded retry / fault / latency tests
→ authoritative manifest verification
```

真实 LLM 只作为 optional smoke test。

真实 AWS 不属于 Stage 1。


---


## 10. Three-Stage Delivery Strategy

### Stage 1 — Thick LocalStack Ultimate / Local-First

目标：

```text
绝大多数开发
+
绝大多数 AWS-compatible contract verification
+
并发 / recovery / IAM / fault injection
```

Stage 1A–1G 保留现有核心实现，并新增：

```text
Stage 1H — AWS Runtime Contract Hardening
- explicit backend mode independent from endpoint override
- Terraform owns infrastructure
- no hard-coded LocalStack credentials inside clients
- shared AWS SDK configuration
- bounded standard retries
- precise AWS error classification
- repository/artifact/worker readiness
- canonical S3 manifest URI

Stage 1I — LocalStack Cloud Resilience
- least-privilege IAM enforcement
- IAM allow/deny verification
- DynamoDB throttling / 5xx fault tests
- S3 write failure tests
- network latency tests
- heartbeat under transient cloud faults
- multi-process workers
- real process kill → reclaim → pinned replay
- concurrent batch acceptance
```

原则：

> **Stage 1 不是 demo，而是主要的 correctness + cloud-behavior verification stage。**

Stage 1 freeze 需要：

```text
go test -race PASS
TFLint live PASS
Kubeconform live PASS
LocalStack E2E PASS
LocalStack IAM PASS
LocalStack resilience/fault tests PASS
multi-worker crash/recovery PASS
```

环境无法执行的 required gate 必须明确标记 `PARTIAL — environment verification only`，不能伪装 PASS。

### Stage 2 — Thin Real AWS Smoke Validation

Stage 2 不重新开发 PlatformLens。

保持：

```text
same Go binary
same AWS SDK implementation
same RunRepository contract
same ArtifactStorage contract
same state machine
same manifest/evidence schemas
```

只验证 LocalStack 无法最终证明的现实差异：

```text
real AWS credential chain / TLS
real DynamoDB conditional-write behavior
real GSI candidate discovery smoke
real S3 artifact read/write
real IAM allow + deny
one happy-path Run
one crash/reclaim/replay Run
cost evidence
terraform destroy
```

默认不要求：

```text
EC2 / ECS / EKS deployment
long-running infrastructure
load testing
chaos testing
large-scale benchmarks
new product features
```

可以直接让本地 PlatformLens worker 连接短生命周期真实 DynamoDB/S3/IAM，从而把 AWS 成本压到最低。

原则：

> **Stage 2 是短生命周期的 reality check，不是第二次开发。**

### Stage 3 — Reference-Driven Code Polish

在 Stage 1 + Stage 2 行为稳定后，对照四个参考仓：

```text
ANZ golden-retriever
CBA project-echo
CBA architect-agent
ANZ pkg
```

进行：

```text
code organization cleanup
API/model naming cleanup
concurrency/recovery hardening
runtime hygiene
test quality improvement
error/logging consistency
documentation/readability cleanup
```

约束：

```text
no new product scope
no architecture redesign
preserve all frozen invariants
tests before risky refactor
independent implementation
respect OSS licenses
do not mechanically copy source
```

Stage 3 的目标是：

> **让已经被 LocalStack + Real AWS 验证过的 PlatformLens 更像成熟工程代码，而不是增加功能。**


---

## 11. Scope Freeze


不包含：

```text
Git diff
source archival
Backstage
PR bot
auto-remediation
Terraform generation
Helm/Kustomize rendering
SQS / Step Functions
EKS
RAG
multi-cloud
hostile-repository sandbox
LangGraph
real-AWS load/chaos stage
always-on AWS compute
```

> **This v0.8.5 Go / LocalStack-heavy delivery boundary is frozen for implementation.**
