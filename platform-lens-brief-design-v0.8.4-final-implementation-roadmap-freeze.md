# PlatformLens — Brief Design v0.8.4 FINAL IMPLEMENTATION ROADMAP FREEZE

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
Clock abstraction
toolchain.lock enforcement
structured logging
resource limits
health/readiness/version
heartbeat + cancellation
workspace sweeper
```

Required CI E2E：

```text
LocalStack
+
deterministic fake reviewer/evaluator
```

真实 LLM 只作为 optional smoke test。

---


## 10. Three-Stage Delivery Strategy

### Stage 1 — LocalStack Ultimate / Local-First Completion

目标：

```text
尽可能完成完整 PlatformLens
```

包括：

```text
Milestone 0–6 core implementation
SQLite / Filesystem
DynamoDB / S3 through LocalStack Ultimate
failure injection
concurrency tests
fake-AI deterministic CI E2E
optional live-AI smoke
```

原则：

> **Stage 1 不是 demo；它应完成绝大多数产品逻辑。**

### Stage 2 — Real AWS Validation

保持：

```text
same architecture
same RunRepository contract
same ArtifactStorage contract
same application code
```

重点验证：

```text
real DynamoDB conditional writes
real S3
IAM least privilege
deployment/restart behavior
network/TLS
throttling/service errors
CloudWatch observability
cost evidence
```

Stage 2 不新增产品功能，只验证真实 AWS semantics。

### Stage 3 — Reference-Driven Code Polish

在功能与 AWS 行为稳定后，对照四个参考仓：

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

> **让已经正确运行的 PlatformLens 更像成熟工程代码，而不是增加功能。**

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
```

> **This design is frozen for implementation.**
