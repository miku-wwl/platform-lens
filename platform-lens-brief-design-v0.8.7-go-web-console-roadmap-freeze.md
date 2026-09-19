# PlatformLens — Brief Design v0.8.7 GO + WEB CONSOLE FINAL DELIVERY ROADMAP FREEZE

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

v0.8.7 不改变 PlatformLens 核心架构，只冻结最终 delivery 顺序：

```text
Stage 1 = Thick LocalStack Ultimate
          Build + Prove
          PASS / FREEZE

Stage 2 = Reference-Driven Code Polish
          behavior-preserving refactor
          full Stage 1 regression
          RE-FREEZE

Stage 2.5 = Web Console / Operator UX
            React + TypeScript + Vite
            presentation/read APIs only
            UI acceptance
            FREEZE

Stage 3 = Thin Real AWS Final Validation
          short-lived final reality check on final backend + frontend
```

目标：

> **先在 LocalStack Ultimate 把 correctness / cloud contract 做厚，再对照四个 OSS 仓润色后端代码，随后增加轻量 Web Console，最后只用很薄的真实 AWS 验证最终产品。**

这样真实 AWS 验证发生在代码润色之后，避免 Stage 3 再改代码导致之前的真实 AWS 证据过期。

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

真实 AWS 不属于 Stage 1；真实 AWS 最终验证属于 Stage 3。


---


## 10. Delivery Strategy

### Stage 1 — Thick LocalStack Ultimate / Build & Prove

目标：

```text
绝大多数产品开发
+
绝大多数 AWS-compatible contract verification
+
并发 / recovery / IAM / multi-process / process-kill validation
```

Stage 1 已完成并冻结：

```text
go test PASS
go test -race PASS
TFLint live PASS
Kubeconform live PASS
SQLite/Filesystem E2E PASS
LocalStack DynamoDB/S3 E2E PASS
LocalStack IAM enforcement PASS
LocalStack CAS/concurrency PASS
multi-process 20-run acceptance PASS
real process-kill reclaim/replay PASS
```

原则：

> **Stage 1 = Build & Prove。Stage 1 已 PASS / FREEZE，不再继续扩大核心后端功能。**

### Stage 2 — Reference-Driven Code Polish / Re-Freeze

对照：

```text
ANZ golden-retriever
CBA project-echo
CBA architect-agent
ANZ pkg
```

只做：

```text
package / module boundary cleanup
API / model naming cleanup
concurrency / recovery readability hardening
runtime hygiene
error taxonomy
logging consistency
resource cleanup
test readability
fixture quality
duplicate-code cleanup
documentation / comments
```

约束：

```text
no new backend product scope
no architecture redesign
no new AWS services
preserve all frozen invariants
behavior-preserving refactor
tests before risky refactor
independent implementation
respect OSS licenses
```

完成后必须完整重跑 Stage 1 acceptance：

```text
go vet PASS
go test PASS
go test -race PASS
TFLint live PASS
Kubeconform live PASS
SQLite E2E PASS
LocalStack E2E PASS
LocalStack IAM PASS
DynamoDB CAS/concurrency PASS
multi-process acceptance PASS
process-kill reclaim/replay PASS
manifest verification PASS
```

成功后：

```text
Stage 2 = PASS / RE-FREEZE
```

### Stage 2.5 — Web Console / Operator UX

Stage 2.5 新增一个轻量前端，用于提交分析、观察 Run 生命周期、查看验证结果与证据。

技术栈：

```text
React
TypeScript
Vite
```

架构：

```text
Browser
  ↓
PlatformLens Web Console
  ↓
Existing Go HTTP API
  ↓
PlatformLens Core
```

核心页面：

```text
Dashboard
New Analysis
Runs
Run Detail
```

Run Detail 至少展示：

```text
Source Identity
Run / Attempt Timeline
Validation Results
Diagnostics
Evidence
AI Findings
Evaluation
Terraform Provenance
Kubernetes Results
Manifest
```

推荐把状态机可视化：

```text
QUEUED
→ CLAIMED
→ RETRIEVING
→ VALIDATING
→ REVIEWING
→ EVALUATING
→ PERSISTING
→ COMPLETED / FAILED
```

以及 recovery timeline：

```text
Attempt 1
→ worker lost

Attempt 2
→ reclaim
→ replay pinned commit
→ COMPLETED
```

Stage 2.5 允许增加非常薄的 presentation/read API，例如：

```text
GET /analysis
GET /analysis/{run_id}/artifacts
GET /analysis/{run_id}/report
```

但必须遵守：

```text
UI may read/submit
UI may add pagination/filtering DTOs
UI must not own business logic

no RunRepository semantic change
no state-machine change
no fencing/CAS change
no evidence semantic change
no manifest authority change
```

Stage 2.5 acceptance：

```text
frontend build PASS
frontend tests PASS
Go regression PASS
go test -race PASS

submit Run from UI PASS
observe Run state PASS
view diagnostics PASS
view evidence PASS
view findings/evaluation PASS
view manifest/report PASS
LocalStack end-to-end demo PASS
```

成功后：

```text
Stage 2.5 = PASS / FREEZE
```

原则：

> **Stage 2.5 是 presentation layer，不是新的 control plane。**

### Stage 3 — Thin Real AWS Final Validation

Stage 3 使用 **Stage 2.5 freeze 后的最终 backend + frontend**，不进行第二轮开发。

保持：

```text
same final Go backend
same AWS SDK implementation
same RunRepository contract
same ArtifactStorage contract
same state machine
same manifest/evidence schemas
same Web Console
```

只验证 LocalStack 无法最终证明的现实差异：

```text
real AWS IAM allow / deny
real DynamoDB conditional-write behavior
real GSI candidate discovery smoke
real S3 read/write + manifest read-back/hash
real credential chain / TLS/network
one happy-path Run
one process-kill/reclaim Run
one UI-driven happy-path smoke
basic CloudWatch evidence
real cost evidence
terraform destroy
```

默认不要求：

```text
EC2 / ECS / EKS deployment
always-on AWS compute
large multi-tenant SaaS control plane
frontend direct AWS/database access
load testing
large benchmark
real-AWS chaos campaign
long soak test
new product features
```

原则：

> **Stage 3 = Final Reality Check。短生命周期、低成本、验证完成即 destroy。**

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
large multi-tenant SaaS control plane
frontend direct AWS/database access
```

> **This v0.8.7 delivery roadmap is frozen: Stage 1 LocalStack → Stage 2 OSS Polish → Stage 2.5 Web Console → Stage 3 Thin Real AWS Final Validation.**
