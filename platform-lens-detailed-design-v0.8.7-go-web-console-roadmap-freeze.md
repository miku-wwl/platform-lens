# PlatformLens — Detailed Design v0.8.7 GO + WEB CONSOLE FINAL DELIVERY ROADMAP FREEZE

## 1. Overview

PlatformLens 是一个面向 Git-based Infrastructure-as-Code 的 Evidence-Driven Infrastructure Analysis Platform。

V1 输入：

```text
Repository URL
Git Reference
Optional Repository Path
```

V1 输出：

```text
Pinned Source Identity
Deterministic Diagnostics
Structured Evidence
AI-assisted Findings
Finding Evaluation
Template-Driven Report
Audit Artifacts
```

V1 只分析单个 Git snapshot，不做 base→target diff。

---

## 2. Core Principles

### 2.1 Immutable Source

每个 Run 最终固定：

```text
repository_url
requested_ref
resolved_ref
ref_type
commit_oid
```

`commit_oid` 一旦持久化，不允许改变。

### 2.2 Deterministic Facts First

事实只来自 deterministic producers：

```text
Git
Terraform
TFLint
Kubeconform
```

LLM 只能：

```text
interpret
correlate
explain
recommend
```

### 2.3 Evidence-Bound AI

每个 factual AI observation 必须引用 Evidence。

### 2.4 Recover by Replay

```text
successful reclaim
→ new attempt
→ pinned commit_oid
→ fresh workspace
→ full replay
```

不恢复 previous attempt partial runtime state。

### 2.5 Lease for Liveness, Fencing for Safety

Lease：

```text
heartbeat
stale detection
reclaim eligibility
```

Safety：

```text
attempt_no
+
lease_owner
+
conditional mutation
```


### 2.6 Local-First Cloud Verification

PlatformLens 把 LocalStack Ultimate 作为 V1 主要云端验证环境。

```text
Stage 1
→ prove application/cloud contracts locally as far as practical

Stage 2
→ polish the frozen backend implementation without changing behavior

Stage 2.5
→ add a lightweight Web Console over stable APIs

Stage 3
→ validate only the remaining real-AWS differences using the final backend + frontend
```

LocalStack 负责尽可能验证：

```text
DynamoDB conditional writes
S3 artifact semantics
IAM least privilege
AWS SDK credential/retry configuration
multi-worker behavior
transient service failures
network latency
crash/reclaim/replay
```

但不得把 LocalStack 结果表述成：

```text
proof of real DynamoDB GSI timing
proof of real AWS quota behavior
proof of real regional failure behavior
```

这些现实差异只在薄 Stage 3 做 final smoke validation；Stage 2.5 只增加 presentation layer，不替代真实 AWS 验证。

---

## 3. Architecture

```text
API / CLI
   ↓
Run Store
   ↓
Conditional Claim
   ↓
Run Orchestrator
   ↓
Isolated Git Runtime
   ↓
Repo Lock
   ↓
Fetch Selected Ref
   ↓
Local Resolve / Verify
   ↓
CAS Pin Source
   ↓
Fresh Attempt Workspace
   ↓
Target Discovery
   ↓
ValidationPlan
   ↓
ValidationResult
   ↓
Diagnostic
   ↓
Evidence
   ↓
Redacted Bounded Context
   ↓
Reviewer
   ↓
Deterministic Evaluator
   ↓
Semantic Evaluator
   ↓
Template Report
   ↓
Manifest Commit
   ↓
COMPLETED
```

---


## 3.1 Go Implementation Baseline

PlatformLens V1 使用 Go 实现。

推荐仓库结构：

```text
platform-lens/
├── cmd/
│   └── platformlens/
│       └── main.go
├── internal/
│   ├── api/
│   ├── source/
│   ├── runs/
│   ├── execution/
│   ├── discovery/
│   ├── validation/
│   │   ├── terraform/
│   │   └── kubernetes/
│   ├── evidence/
│   ├── review/
│   ├── evaluation/
│   ├── storage/
│   ├── report/
│   └── runtime/
├── infra/
├── fixtures/
├── tests/
├── toolchain.lock
├── go.mod
├── go.sum
└── README.md
```

实现原则：

```text
one Go service / binary
package boundaries follow architecture boundaries
internal/ for non-public implementation
strong typed structs for persisted/API schemas
interfaces only at real substitution boundaries
```

核心 Go mapping：

```text
cancellation / deadline
→ context.Context

heartbeat / worker concurrency
→ goroutines + context cancellation

local synchronization
→ sync.Mutex / sync.RWMutex / sync.Once as appropriate

external commands
→ os/exec via CommandRunner

HTTP API
→ net/http

structured logging
→ log/slog

SQLite
→ database/sql + pinned SQLite driver

DynamoDB / S3
→ AWS SDK for Go v2

clock abstraction
→ Clock interface

JSON schemas
→ typed Go structs + explicit validation
```

Git / Terraform / TFLint / Kubeconform 保持为外部 CLI，不为了“纯 Go”改写它们的真实 CLI semantics。

Go implementation 不改变任何 frozen architecture invariant。

### 3.2 Web Console Baseline

Stage 2.5 增加独立的轻量 Web Console：

```text
web/
├── src/
│   ├── api/
│   ├── components/
│   ├── features/
│   │   ├── runs/
│   │   ├── analysis/
│   │   └── evidence/
│   ├── pages/
│   ├── types/
│   └── main.tsx
├── package.json
├── tsconfig.json
└── vite.config.ts
```

推荐技术栈：

```text
React
TypeScript
Vite
```

默认不引入：

```text
Next.js
SSR
GraphQL
micro-frontend
BFF
frontend-owned business workflow
```

Web Console 只通过 PlatformLens HTTP API 工作：

```text
Browser
→ React Web Console
→ Go HTTP API
→ Application Service
→ RunRepository / ArtifactStorage
```

前端不得直接访问 DynamoDB、S3、SQLite、Filesystem 或 workspace。

---

### 3.3 Web Console UX Boundary

Stage 2.5 的目标不是把 PlatformLens 改造成大而全 SaaS，而是提供 operator/demo surface。

核心页面：

```text
Dashboard
New Analysis
Runs
Run Detail
```

`New Analysis`：

```text
repository_url
requested_ref
optional requested_path
```

`Runs`：

```text
run_id
state
attempt_no
repository
requested_ref
commit_oid?
analysis_outcome?
coverage_status?
updated_at
```

`Run Detail` 至少展示：

```text
Source Identity
Lifecycle Timeline
Attempt History
Validation Results
Diagnostics
Evidence
AI Findings
Evaluation
Terraform Provenance
Kubernetes Results
Artifacts
Report
Manifest
```

状态可视化：

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

Recovery 可视化：

```text
Attempt 1
CLAIMED → ... → worker lost

Attempt 2
CLAIMED → replay pinned commit → ... → COMPLETED
```

前端显示的数据必须来自后端 authoritative state / artifacts，不得自行推导另一个 lifecycle truth。

---

## 4. AnalysisRun

```text
AnalysisRun
- run_id

Source:
- repository_url
- requested_ref
- resolved_ref
- ref_type
- commit_oid
- requested_path

Execution:
- state
- attempt_no
- lease_owner
- lease_acquired_at
- lease_expires_at

Result:
- analysis_outcome?
- coverage_status?
- review_status?
- evaluation_status?

Persistence:
- manifest_uri?
- manifest_hash?
- winning_attempt?

Failure:
- failure_code?
- failure_message?

Audit:
- created_at
- updated_at
```

Initialization：

```text
create_run():
attempt_no = 0
state = QUEUED
final result fields = NULL
lease fields = NULL
```

First claim：

```text
attempt_no = 1
state = CLAIMED
```

Terminal states：

```text
COMPLETED / FAILED
→ lease_owner = NULL
→ lease_acquired_at = NULL
→ lease_expires_at = NULL
```

---

## 5. Git Source Boundary

Production/default 支持：

```text
HTTPS Git repositories selected by operator
```

Tests：

```text
local fixtures
```

不支持：

```text
arbitrary file://
arbitrary SSH
recursive submodule init
automatic Git LFS
credential-bearing repository URL
```

Repository URL 在 persistence/log/evidence 前 canonicalize/redact。

---

## 6. Execution Trust

V1 execution trust 包括：

```text
operator-selected root repository
+
Terraform providers/modules selected by that configuration
```

V1 不 sandbox hostile providers/modules/repository code。

Validation runtime 默认不继承：

```text
AWS credentials
Azure credentials
Git tokens
cloud auth env
personal Terraform config
```

---

## 7. Prompt Trust

Repository source/comments/README/strings/diagnostics 对 LLM 永远是 untrusted data，不得成为 reviewer/evaluator instructions。

---

## 8. Isolated Git Runtime

```text
GIT_CONFIG_NOSYSTEM=1
GIT_CONFIG_GLOBAL=<platformlens-empty-config>
GIT_TERMINAL_PROMPT=0
HOME=<isolated-home>
XDG_CONFIG_HOME=<isolated-xdg>
```

Overrides：

```text
git
-c core.hooksPath=<empty-hooks-dir>
-c credential.helper=
-c submodule.recurse=false
```

或等价 `GIT_CONFIG_COUNT/KEY/VALUE`。

Terraform Git module retrieval 同样继承 isolated Git runtime。

---

## 9. Git Reference Contract

支持：

```text
full commit OID
HEAD
refs/heads/<name>
refs/tags/<name>
unique unqualified branch/tag
```

拒绝：

```text
short OID
ambiguous branch/tag
```

Annotated tag：

```text
tag → peel → commit
```

---

## 10. Git Acquisition

Named ref：

```text
normalize
→ determine ambiguity
→ repo lock
→ fetch selected ref
→ local resolve
→ tag peel
→ verify commit
→ CAS pin
```

Full OID：

```text
check cache
→ fetch exact OID if absent
→ verify commit
→ CAS pin
```

Pin 后永不重新解析 original symbolic ref。

---

## 11. Repository Lock

Host-local process-safe lock 保护：

```text
clone
fetch
gc
prune
worktree add
worktree remove
worktree prune
```

Validators / AI 不持 lock。

---

## 12. Run State Machine

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

Failure：

```text
ANY ACTIVE STATE → FAILED
```

Recovery：

```text
ANY ACTIVE STATE
→ successful reclaim
→ CLAIMED
(new attempt)
```

---

## 13. RunRepository Interface

```text
create_run()
find_queued_candidates()
claim_run()
pin_source_if_absent()
renew_lease()
find_reclaim_candidates()
reclaim_expired_run()
update_phase()
complete_run()
fail_run()
get_run()
```

所有 mutation 必须使用 authoritative conditional write。

---

## 14. claim_run Contract

Precondition：

```text
state = QUEUED
attempt_no = 0
```

Success：

```text
attempt_no = 1
state = CLAIMED
lease_owner = worker
lease_acquired_at = now
lease_expires_at = new_expiry
```

Concurrent claim：

```text
exactly one succeeds
```

---

## 15. update_phase Contract

Precondition：

```text
run_id matches
attempt_no = current_attempt
lease_owner = current_worker
state = expected_from_state
```

Success：

```text
state = next_state
```

Old/stale attempt mutation 必须 rejected。

---

## 16. pin_source_if_absent Contract

Precondition：

```text
commit_oid IS NULL
attempt_no = current_attempt
lease_owner = current_worker
state = RETRIEVING
```

Success 写入：

```text
commit_oid
resolved_ref
ref_type
```

Pin at most once。

---

## 17. renew_lease Contract

Caller supplies：

```text
attempt_no
lease_owner
expected_lease_expires_at
new_lease_expires_at
```

Precondition：

```text
attempt_no matches
lease_owner matches
lease_expires_at = expected_lease_expires_at
state is active
```

Success：

```text
lease_expires_at = new_lease_expires_at
```

---

## 18. Reclaim Candidate

Discovery is not authoritative。

Candidate record：

```text
run_id
attempt_no
lease_owner
lease_expires_at
state
```

`lease_expires_at` 作为 `expected_lease_expires_at` 参与 reclaim CAS。

---

## 19. reclaim_expired_run Contract

Caller supplies：

```text
expected_attempt_no
expected_lease_owner
expected_lease_expires_at
expected_state
```

Precondition：

```text
attempt_no = expected_attempt_no
lease_owner = expected_lease_owner
lease_expires_at = expected_lease_expires_at
state = expected_state
lease_expires_at <= observer_now
```

Success：

```text
attempt_no += 1
lease_owner = new_worker
lease_acquired_at = observer_now
lease_expires_at = new_expiry
state = CLAIMED
```

如果旧 owner 已 renew 导致 expiry 变化，stale reclaim 必须失败。

---

## 20. complete_run Contract

Precondition：

```text
attempt_no = current_attempt
lease_owner = current_worker
state = PERSISTING
```

Success 原子写：

```text
state = COMPLETED
analysis_outcome
coverage_status
review_status
evaluation_status
winning_attempt
manifest_uri
manifest_hash

lease_owner = NULL
lease_acquired_at = NULL
lease_expires_at = NULL
```

---

## 21. fail_run Contract

Precondition：

```text
attempt_no = current_attempt
lease_owner = current_worker
state is active
```

Success：

```text
state = FAILED
failure_code
failure_message

lease_owner = NULL
lease_acquired_at = NULL
lease_expires_at = NULL
```

旧 attempt 不得 fail 新 attempt。

---

## 22. Heartbeat and Cancellation

Heartbeat 独立 goroutine 运行，不能被：

```text
Git
Terraform
validators
AI
S3 writes
```

阻塞。

Heartbeat error 必须分类：

### 22.1 Authoritative Fencing Loss

例如：

```text
attempt_no mismatch
lease_owner mismatch
expected lease expiry mismatch
conditional ownership/state failure
```

处理：

```text
immediate attempt cancellation
→ terminate child processes
→ stop local authoritative work
```

### 22.2 Transient Cloud Failure

例如：

```text
throttling
HTTP 429
HTTP 500 / 503
temporary network error
timeout
```

不能把第一次 transient error 直接等价成 fencing loss。

处理原则：

```text
AWS SDK bounded retry
→ heartbeat loop retains last confirmed expiry
→ retry within bounded safety window
→ if renewal cannot be confirmed before safety deadline:
     cancel local attempt
     do not commit authoritative result
     allow lease expiry + reclaim
```

不得在 lease ownership 不确定时写：

```text
COMPLETED
```

LLM / validator / artifact operations 同样继承 attempt context。

CommandRunner 在 cancellation 后终止 process tree。

---

## 23. Workspace Lifecycle

每 attempt：

```text
fresh workspace
```

Completion/failure：

```text
best-effort cleanup
```

启动/维护：

```text
stale workspace sweeper
```

Artifact retention 与 workspace cleanup 分离。

---

## 24. Workspace Path Confinement

所有 workspace-relative path 在读取或作为 cwd 前必须 confinement：

```text
requested_path
Terraform target root
Kubernetes manifest path
Diagnostic.file
SourceExcerpt.file
artifact-relative source locator
```

```go
func ResolveExistingWithin(root, relative string) (string, error) {
    if filepath.IsAbs(relative) {
        return "", ErrPathOutsideWorkspace
    }

    rootAbs, err := filepath.Abs(root)
    if err != nil {
        return "", err
    }

    candidate := filepath.Join(rootAbs, filepath.Clean(relative))
    candidate, err = filepath.EvalSymlinks(candidate)
    if err != nil {
        return "", err
    }

    rel, err := filepath.Rel(rootAbs, candidate)
    if err != nil ||
        rel == ".." ||
        strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
        return "", ErrPathOutsideWorkspace
    }

    return candidate, nil
}
```

拒绝：

```text
../ traversal
absolute outside path
symlink escape
prefix confusion
```

对于必须存在的 repository/tool 输入路径，使用 `filepath.EvalSymlinks` 后再做 containment 检查。
对于尚未创建的输出路径，先 confinement 已存在 parent directory，再由 PlatformLens 自己创建文件。

---

## 25. Terraform Target Discovery

若提供 `requested_path`：

```text
requested_path = authoritative analysis scope
```

否则：

```text
conservative deterministic candidate discovery
```

```text
TerraformTarget
- target_id
- root_path
- discovery_method
- files[]
- lockfile_present
- source_lockfile_hash?
```

Child module 不默认成为独立 root target。

---

## 26. Kubernetes Discovery

每个 raw manifest document：

```text
ResourceIdentity
- api_version
- kind
- namespace?
- name?
- file
- document_index
```

Helm/Kustomize：

```text
SKIPPED_UNSUPPORTED
```

---

## 27. ValidationPlan

```text
ValidationPlan
├── scope_checks[]
├── preparations[]
└── validators[]
```

```text
ValidationStep
- step_id
- kind
- target_id?
- required
- producer
```

---

## 28. ValidationResult

```text
ValidationResult
- step_id
- step_kind
- producer
- producer_version
- target_id?
- resource_identity?
- required
- status
- duration_ms
- exit_code?
- diagnostics[]
- raw_output_ref?
```

Status：

```text
PASS
FAIL
ERROR
SKIPPED
SKIPPED_UNSUPPORTED
SKIPPED_SCHEMA_MISSING
```

---

## 29. Diagnostic

```text
Diagnostic
- diagnostic_id
- producer
- target_id?
- resource_identity?
- severity?
- rule_code?
- message
- file?
- start_line?
- end_line?
- raw_output_ref?
```

---

## 30. Tool Adapter Rules

Raw CLI output 必须经过 producer-specific adapter。

```text
PASS = normal completion, no validation failure
FAIL = normal validation completion, deterministic problem found
ERROR = tool could not complete responsibility
```

---

## 31. Terraform Adapters

```text
fmt:
clean → PASS
difference → FAIL
execution problem → ERROR
```

```text
init:
exit 0 → PREPARATION PASS
non-zero/timeout/dependency failure → PREPARATION ERROR
```

```text
validate -json:
valid=true → PASS
valid=false → FAIL
unexpected/unparseable execution result → ERROR
```

PASS 可携带 warning diagnostics。

---

## 32. TFLint Adapter

```text
0 → PASS
1 → ERROR
2 → FAIL
```

只使用 PlatformLens-owned config、built-in rules；不执行 `tflint --init` 和 external plugins。

---

## 33. Kubeconform Adapter

One process = one ToolExecution。

**Every discovered supported Kubernetes resource must end with exactly one terminal ValidationResult.**

```text
valid → PASS
invalid → FAIL
missing schema → SKIPPED_SCHEMA_MISSING
resource/tool failure → ERROR
```

如果 process partial output 后失败：

```text
already-normalized resources → retain result
remaining discovered resources → ERROR
```

如果 machine output completely unusable：

```text
all discovered supported resources → ERROR
```

---

## 34. Effective Validation Count

```text
effective_validation_count
=
count(VALIDATOR results where status in {PASS, FAIL})
```

Kubernetes 按 resource-level result 计数。

---

## 35. Outcome Rules

```text
if deterministic_finding_count > 0:
    FINDINGS
elif required_supported_error_count > 0:
    INCONCLUSIVE
elif effective_validation_count == 0:
    INCONCLUSIVE
else:
    NO_FINDINGS
```

Coverage 独立：

```text
NONE
PARTIAL
COMPLETE
```

---

## 36. Terraform Baseline / Runtime

V1 requires：

```text
Terraform >= 1.10
```

实际运行版本由：

```text
toolchain.lock
```

固定到精确版本。

Environment：

```text
TF_IN_AUTOMATION=1
TF_INPUT=0
TF_DATA_DIR=<attempt-private-dir>
TF_CLI_CONFIG_FILE=<platformlens-owned-config>
```

同时继承 isolated Git runtime。

---

## 37. Per-Target Terraform Dependency Provenance

Dependency provenance **按 target 记录**。

```text
TerraformDependencyProvenance
- target_id

Lockfile:
- source_lockfile_present
- source_lockfile_hash?
- effective_lockfile_hash
- lockfile_origin
- provider_provenance_status

Providers:
- providers[]

Modules:
- module_provenance_status
- module_provenance_artifact?
```

`lockfile_origin`：

```text
SOURCE
GENERATED
```

如果 source repo 自带 lockfile：

```text
-lockfile=readonly
lockfile_origin = SOURCE
```

如果没有 source lockfile：

```text
terraform init generates attempt-local lockfile
lockfile_origin = GENERATED
provider_provenance_status = PARTIAL
```

---

## 38. Provider Provenance Schema

每个 provider selection：

```text
ProviderSelection
- source_address
- selected_version
- constraints?
- package_hashes[]
```

Provider provenance 来自 effective `.terraform.lock.hcl`。

记录：

```text
source_lockfile_hash?
effective_lockfile_hash
provider selections
provider package hashes
```

Source lockfile 表示 repo 原始输入；effective lockfile 表示本次 validation 实际 provider selection。

---

## 39. Terraform Module Provenance

`.terraform.lock.hcl` 不锁 remote module selection。

Terraform >=1.10 时，init 后使用：

```text
terraform modules -json
```

作为 module metadata source。

同时对 downloaded module directory 计算 canonical：

```text
content_tree_hash
```

每 target/module 记录：

```text
target_id
module_key
declared_source
declared_version_or_ref?
resolved_version?
resolved_local_path
resolved_vcs_revision?
content_tree_hash
```

无法完整确定 identity：

```text
module_provenance_status = PARTIAL
```

不依赖 `.terraform/modules/modules.json` 作为稳定公共 contract。

---

## 40. Canonical Module Tree Hash

模块目录 hash 使用固定算法：

```text
1. recursively enumerate entries
2. use workspace/module-relative POSIX paths
3. sort paths lexicographically
4. exclude VCS/runtime metadata defined by PlatformLens
5. regular file entry hashes:
   relative path
   entry type
   file content bytes
6. symlink entry hashes:
   relative path
   entry type
   symlink target text
7. do not hash:
   mtime
   owner
   absolute path
```

禁止 follow symlink 到 module tree 外部。

最终：

```text
SHA256(canonical entry stream)
```

Algorithm version：

```text
module_tree_hash_version = 1
```

写入 provenance。

---

## 41. Kubernetes Schema Provenance

记录：

```text
kubernetes_version
schema_repository
schema_repository_commit
schema_location_template
```

不使用 mutable branch。

---

## 42. Toolchain Lock

Repository 根：

```text
toolchain.lock
```

固定：

```text
Go build toolchain version
terraform exact version (>=1.10)
tflint exact version
kubeconform exact version
```

Runtime：

```text
Terraform / TFLint / Kubeconform actual != expected
→ EXECUTION_ERROR
```

Go toolchain version 在 build/CI 阶段 enforce，并写入 build provenance。

除非显式 dev mode 允许 warning。

---

## 43. Clock Abstraction

所有：

```text
lease timestamps
created_at
updated_at
started_at
completed_at
```

通过 Go interface 获取：

```go
type Clock interface {
    Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time {
    return time.Now().UTC()
}
```

Tests 使用 deterministic `FakeClock`，禁止在 lease/reclaim 单元测试中直接依赖 wall clock。

---

## 44. CommandRunner

Go `CommandRunner` 统一封装外部 CLI：

```text
executable allowlist
fixed argv
confined cwd
environment allowlist
timeout
stdout/stderr cap
duration
context cancellation
process-tree cleanup
```

基础调用使用：

```go
cmd := exec.CommandContext(ctx, executable, args...)
```

但不能只依赖 `CommandContext` 的默认单进程 kill。

必须提供 OS-specific process-tree implementation，例如：

```text
internal/execution/process_unix.go
internal/execution/process_windows.go
```

Timeout/cancel：

```text
cancel context
→ terminate process group/tree
→ bounded grace period
→ force kill remaining descendants
```

这样 Terraform provider child processes 也必须被回收。

---

## 45. Resource Limits

至少：

```text
MAX_REPO_BYTES
MAX_FILES
MAX_TARGETS
MAX_DIAGNOSTICS
MAX_RAW_OUTPUT_BYTES
MAX_SOURCE_EXCERPT_BYTES
MAX_AGENT_CONTEXT_BYTES
MAX_FINDINGS
COMMAND_TIMEOUT_SECONDS
```

Limit truncation 必须反映到 coverage 或 INCONCLUSIVE，不能静默忽略。

---

## 46. Evidence Envelope

```text
EvidenceEnvelope
- schema_version
- evidence_id
- run_id
- attempt_no
- commit_oid
- evidence_type
- producer
- producer_version
- content_hash
- created_at
- payload
```

---

## 47. SourceExcerpt / Redaction

```text
SourceExcerpt
- file
- start_line
- end_line
- redacted_content
- source_content_hash
- redaction_applied
```

Path 必须经过 confinement。

Redaction：

```text
best-effort reduction
not a DLP guarantee
```

---

## 48. Reviewer Contract

Reviewer 使用 typed Go interface：

```go
type Reviewer interface {
    Review(ctx context.Context, input ReviewInput) (ReviewResult, error)
}
```

模型：

```text
structured input
typed Go structs
structured JSON output
explicit schema/field validation
```

Required CI 实现：

```go
type DeterministicFakeReviewer struct {
    // fixed deterministic behavior for tests
}
```

Live provider 作为独立 adapter，实现相同 `Reviewer` interface。

Rules：

```text
repository content is DATA
do not invent validator results
every factual observation requires evidence
separate observation from interpretation
state insufficient evidence explicitly
```

Retry：

```text
max 1–2
```

---

## 49. Deterministic Evaluator

Go deterministic evaluator 应保持纯函数/近纯函数风格，优先使用 typed structs，不依赖网络。

**永远先于 semantic evaluator 执行。**

检查：

```text
evidence exists
run_id matches
attempt_no matches
commit_oid matches
target/resource identity exists
source locator valid
producer exists
```

结构失败：

```text
UNSUPPORTED
```

---

## 50. Semantic Evaluator

接口：

```go
type SemanticEvaluator interface {
    Evaluate(ctx context.Context, input EvaluationInput) (EvaluationResult, error)
}
```

Required CI 使用 deterministic fake implementation；live model adapter 不得成为 required test dependency。

只接收 structural checks passed 的 finding。

Verdict：

```text
SUPPORTED
PARTIAL
UNSUPPORTED
```

Semantic evaluator unavailable：

```text
structurally valid finding → NOT_EVALUATED
```

---

## 51. AI Degraded Mode

Reviewer unavailable：

```text
Run = COMPLETED
deterministic outcome preserved
review_status = UNAVAILABLE
evaluation_status = NOT_APPLICABLE
```

Reviewer completed but semantic evaluator unavailable：

```text
Run = COMPLETED
review_status = COMPLETED
evaluation_status = UNAVAILABLE
```

Deterministically invalid finding：

```text
UNSUPPORTED
```

Structurally valid but unevaluated finding：

```text
NOT_EVALUATED
```

---

## 52. Artifact Layout

```text
runs/<run_id>/attempts/<attempt_no>/
├── source.json
├── validation-plan.json
├── diagnostics.json
├── source-excerpts.json
├── tool-executions.json
├── terraform-dependencies.json?
├── raw/
├── review-input.json?
├── reviewer.json?
├── evaluation-input.json?
├── evaluation.json?
├── report.md
└── manifest.json
```

---

## 53. Canonical Manifest Serialization

Manifest 只 hash pre-manifest artifacts，绝不 hash 自己。

Canonical JSON bytes：

```text
UTF-8
no BOM
sorted object keys
stable array ordering defined by schema
no insignificant whitespace
LF line ending where textual newline exists
```

Go implementation：

1. Manifest 使用 typed Go structs。
2. 所有具有 set semantics 的 slices 在 marshal 前 deterministic sort。
3. map keys 必须使用 deterministic JSON encoding。
4. 使用同一份 bytes 进行 write + SHA256。

```go
canonicalizeManifest(manifest)

b, err := json.Marshal(manifest)
if err != nil {
    return err
}

sum := sha256.Sum256(b)

if err := os.WriteFile(path, b, 0o600); err != nil {
    return err
}
```

`encoding/json` 输出的 exact `b` 就是 authoritative manifest bytes。

禁止：

```text
marshal once for hash
pretty-print again for file
```

必须：

```text
same bytes → file
same bytes → SHA256
```

---

## 54. Manifest Schema

至少包含：

```text
schema_version
platformlens_version
validation_plan_schema_version

run
source
result

toolchain:
  go_build_version
  terraform expected + actual version
  tflint expected + actual version
  kubeconform expected + actual version
  toolchain_lock_hash

config:
  git_runtime_config_hash
  terraform_cli_config_hash
  tflint_config_hash
  redaction_rules_version
  context_builder_version

dependencies:
  terraform_dependencies[]
  kubernetes_version
  schema_repository
  schema_repository_commit

ai:
  nullable reviewer/evaluator provenance

artifacts:
  existing_path → sha256 / size / sensitivity

timestamps
```

Arrays with semantic set behavior 必须在 manifest build 前 deterministic sort。

---

## 55. Persistence Commit

```text
write deterministic artifacts
→ write optional AI artifacts
→ build report
→ hash pre-manifest artifacts
→ build canonical manifest
→ write exact manifest bytes
→ hash exact manifest bytes
→ conditional complete_run()
→ COMPLETED
```

---

## 56. SQLite

Go persistence layer 使用：

```text
database/sql
+
pinned SQLite driver
```

优先保持跨平台可重复构建；driver 选择必须在 `go.mod` 中 pin。

Database contract：

```text
WAL
busy_timeout
BEGIN IMMEDIATE
```

至少用于：

```text
claim
renew
reclaim
pin
complete
fail
```

Go repository methods 必须通过 context-aware DB APIs：

```go
QueryContext
ExecContext
BeginTx
```

所有操作遵循 RunRepository CAS contract。

---

## 57. DynamoDB

Go backend 使用：

```text
AWS SDK for Go v2
```

LocalStack Stage 1 与 Real AWS Stage 3 必须复用同一个 repository implementation，仅通过 AWS config / endpoint 配置切换。Stage 2 只允许 behavior-preserving code polish。

GSI：

```text
candidate discovery only
```

Authoritative state：

```text
base-table conditional mutation
```

Reclaim CAS：

```text
expected_attempt_no
expected_owner
expected_lease_expires_at
expected_state
```

Conditional-check failure 必须映射为 domain-level ownership/CAS failure，而不是 generic system error。

---

## 57.1 Shared AWS Runtime Contract

LocalStack Stage 1 与 Real AWS Stage 3 必须使用 **同一套 AWS SDK for Go v2 client implementation**。Stage 2 不得创建另一套 AWS runtime implementation。

Backend selection 与 endpoint override 必须分离：

```text
backend_mode = local | aws

local
→ SQLite + Filesystem

aws + endpoint override
→ DynamoDB + S3 through LocalStack

aws + empty endpoint override
→ DynamoDB + S3 through real AWS
```

禁止继续使用：

```text
endpoint != "" → choose AWS backend
endpoint == "" → choose local backend
```

因为真实 AWS 正常情况下不需要 custom endpoint。

共享 AWS 配置至少包含：

```text
region
optional endpoint override
default credential chain
retry mode
max attempts
request timeout/context
```

禁止在 DynamoDB/S3 client 内硬编码：

```text
access_key = test
secret_key = test
```

LocalStack 测试 credentials 由环境或 test harness 显式提供。

建议集中到：

```text
internal/cloudaws/
```

或等价单一 factory，避免 DynamoDB/S3 各自维护不一致的 AWS config。

Retry：

```text
standard bounded retry
explicit max attempts
```

必须区分：

```text
ConditionalCheckFailed / authoritative CAS failure
→ domain conditional error
→ not treated as transient success

throttling / 5xx / temporary transport
→ retryable according to bounded AWS SDK policy
```

---

## 57.2 Infrastructure Ownership

V1 cloud infrastructure 由 Terraform 创建。

Application runtime 默认 **不得隐式 provisioning**：

```text
no CreateTable during repository constructor
no CreateBucket during storage constructor
```

Runtime startup 只允许：

```text
load config
construct clients
readiness check existing resources
```

`/readyz` 必须实际检查：

```text
RunRepository usable
ArtifactStorage usable
worker loop accepting work
```

DynamoDB readiness 可使用 `DescribeTable`；SQLite readiness 使用轻量 query。
Readiness failure 不得触发资源创建。

原因：

```text
least-privilege worker should not require infrastructure-admin permissions
Stage 1 LocalStack and Stage 3 AWS use the same runtime permission boundary
```

Terraform ownership：

```text
infra/localstack/
→ LocalStack DynamoDB / S3 / IAM test principal

infra/aws/   # Stage 3 only
→ ephemeral real-AWS resources
```

---

## 57.3 S3 Artifact Storage Contract

S3 storage 必须精确区分：

```text
NotFound
AccessDenied
throttling
5xx/service failure
transport failure
```

`Exists()` 不得把所有 error 都转换成：

```text
false, nil
```

Manifest commit：

```text
write manifest bytes
→ S3 PutObject confirmed
→ retain canonical returned artifact URI
→ hash exact bytes
→ conditional complete_run()
```

DynamoDB `manifest_uri` 必须保存 ArtifactStorage 返回的 canonical URI。

S3 backend：

```text
s3://<bucket>/runs/<run_id>/attempts/<attempt_no>/manifest.json
```

如果 manifest PutObject 未确认成功：

```text
Run MUST NOT become COMPLETED
```

Orphan artifacts from losing/stale attempts 允许存在，但永远不 authoritative。

---

## 57.4 LocalStack Least-Privilege IAM Validation

Stage 1 Ultimate acceptance 必须增加本地 IAM enforcement profile。

Terraform 创建 dedicated PlatformLens worker principal。

运行时 principal 只允许 V1 所需动作，例如：

```text
DynamoDB:
DescribeTable
GetItem
PutItem
UpdateItem
Query

S3:
ListBucket          # readiness/head bucket when required
GetObject
PutObject
Head/Get object semantics
```

资源范围必须收窄到：

```text
PlatformLens DynamoDB table + candidate index
PlatformLens artifact bucket + runs/* prefix
```

Worker principal 不应拥有：

```text
dynamodb:CreateTable
dynamodb:DeleteTable
s3:CreateBucket
s3:DeleteBucket
iam:*
administrator access
```

Acceptance 同时验证：

```text
required action → allowed
admin/provisioning action → denied
normal PlatformLens E2E still completes
```

可以使用 LocalStack IAM enforcement / principal policy simulation 作为测试机制。

---

## 57.5 LocalStack Cloud Fault / Retry Validation

Stage 1 使用 LocalStack Ultimate 能力尽可能验证 AWS failure behavior。

优先通过 LocalStack chaos/fault capability 注入：

```text
DynamoDB throttling
DynamoDB 500/503
S3 PutObject 500/503
network latency
temporary service outage
```

如果当前安装版本/entitlement 无法提供对应 chaos capability：

```text
do not fake LocalStack PASS
→ retain AWS-SDK-level deterministic fault tests
→ report LocalStack chaos sub-gate as BLOCKED
```

必须验证：

```text
bounded retries
no state regression
no lease regression
CAS conflict is not retried as success
no false COMPLETED
manifest must exist before completion
transient artifact failure leaves run non-completed or recoverable
```

Latency test 应证明：

```text
long S3/validator operation
does not block independent heartbeat scheduling
```

DynamoDB outage test 应证明：

```text
renewal uncertainty
→ bounded retry
→ cancel before unsafe authority window
→ later reclaim after service recovery
```

---

## 57.6 Multi-Worker / Crash Acceptance

Stage 1 不只做 goroutine-level concurrency。

Ultimate acceptance 至少使用：

```text
2+ independent platformlens OS processes
same LocalStack DynamoDB/S3
unique worker IDs
```

验证：

```text
concurrent candidate discovery
exactly-one claim
heartbeat + phase updates
stale worker fencing
reclaim competition
manifest authority
```

至少执行：

```text
20+ deterministic fixture Runs
2+ worker processes
```

并包含一个真实 process kill 场景：

```text
worker A claims/pins
→ worker A process is terminated
→ lease expires
→ worker B reclaims
→ fresh workspace
→ replay pinned commit
→ winning_attempt increments
→ COMPLETED
```

不得使用 symbolic ref re-resolution 替代 pinned replay。

---

## 57.7 Stage 1 Cloud Acceptance Evidence

Stage 1 cloud acceptance 输出：

```text
STAGE1-LOCALSTACK-CLOUD-ACCEPTANCE-REPORT.md
```

至少记录：

```text
LocalStack version
IAM enforcement status
AWS SDK retry configuration
worker principal
allowed/denied IAM checks
multi-worker run count
crash/reclaim evidence
fault-injection scenarios
latency scenarios
DynamoDB/S3 E2E
manifest verification
race detector result
environment blockers
```

Stage 1 可以因环境 capability 缺失保持：

```text
PARTIAL — environment verification only
```

但实现错误不得降格为 environmental blocker。


---

## 58. API

V1 HTTP server 使用 Go `net/http`。

除非 routing complexity 明确需要，否则不引入大型 Web framework。

Endpoints：

```text
POST /analysis
GET /analysis/{run_id}
GET /healthz
GET /readyz
GET /version
```

Stage 2.5 为 Web Console 允许增加 presentation/read endpoints：

```text
GET /analysis
GET /analysis/{run_id}/artifacts
GET /analysis/{run_id}/report
```

如果实现需要，可增加：

```text
pagination
state filter
repository filter
bounded sorting
```

这些 endpoint 只能暴露已有 authoritative data，不得创建新的 lifecycle semantics。

Handlers：

```text
thin HTTP layer
→ validate/decode
→ call application service
→ encode typed response
```

Web Console API contract：

```text
frontend DTO may differ from persistence model
but backend remains source of truth

no frontend direct DB/storage access
no business rule duplication in React
no client-side reconstruction of authoritative state
bounded artifact/report reads only
```

所有 request-scoped work 传播：

```go
r.Context()
```

`/readyz`：

```text
RunRepository usable
ArtifactStorage usable
worker accepting work
```

`/version`：

```text
service
version
git_commit
build_id
image_tag
go_version
toolchain_lock_hash
```

Build metadata 可通过 Go linker flags 注入，例如 `-ldflags -X`，Go runtime version 使用 `runtime.Version()`。

---

## 59. Structured Logging

使用 Go 标准库：

```text
log/slog
```

日志必须 structured，不以拼接字符串代替结构字段。

关键字段：

```text
run_id
attempt_no
worker_id
state
phase
target_id
producer
commit_oid
```

禁止：

```text
credentials
raw secrets
raw source excerpts
raw tool output
credential-bearing URLs
```

---

## 60. Failure Model

Core：

```text
SOURCE_ERROR
EXECUTION_ERROR
PERSISTENCE_ERROR
SYSTEM_ERROR
```

AI：

```text
AGENT_UNAVAILABLE
EVALUATION_UNAVAILABLE
```

AI unavailable 不自动导致 Run FAILED。

---

## 61. CI / E2E Strategy

Stage 1 acceptance 分成两个 gate。

### 61.1 Fast Deterministic Gate

```text
go fmt
go vet ./...
go test ./...
go test -race ./...  # required where environment supports Go race detector

Git fixtures
SQLite
validator adapter fixtures
deterministic fake Reviewer/Evaluator
manifest determinism
```

Required tests 不调用真实 LLM。

### 61.2 LocalStack Ultimate Cloud Gate

```text
Terraform provision LocalStack resources
→ DynamoDB/S3 E2E
→ field-scoped conditional lifecycle tests
→ least-privilege IAM enforcement
→ multi-process workers
→ process-kill reclaim/replay
→ cloud fault/latency scenarios
→ S3 manifest read-back/hash verification
```

LocalStack gate 使用和最终 Stage 3 相同的：

```text
AWS SDK implementation
RunRepository implementation
ArtifactStorage implementation
```

只改变：

```text
endpoint
credentials
test environment
```

### 61.3 Live Validators

Stage 1 acceptance 需要真实：

```text
TFLint
Kubeconform
```

fake executable 仅作为 adapter unit test，不能代替 live acceptance。

### 61.4 Live AI

真实模型只作为：

```text
optional smoke test
```

不作为 merge / release / Stage 1 gate。

### 61.5 Claims Boundary

LocalStack E2E 可以证明：

```text
application AWS API contract
IAM policy intent
failure-handling design
retry behavior
concurrency/recovery logic
```

不得宣称证明：

```text
real DynamoDB GSI propagation timing
real AWS regional outage behavior
real quotas
real service latency distribution
```

这些在 Stage 3 只做最小 final smoke validation。

---

## 62. Mandatory Tests

### Provider provenance

```text
source lockfile present
→ SOURCE
→ source/effective hash stable

no source lockfile
→ GENERATED
→ effective lockfile captured
→ provenance PARTIAL
```

### Terraform version / module API

```text
Terraform < 1.10
→ toolchain validation fails

terraform modules -json
→ authoritative module metadata
```

### Module tree hash

```text
same content
different mtimes/absolute paths
→ same hash
```

### Manifest canonical bytes / storage

```text
same logical manifest
same canonical ordering
→ same manifest hash

S3 manifest PutObject failure
→ Run not COMPLETED

successful S3 manifest write
→ manifest_uri uses canonical returned URI
→ read-back bytes hash matches
```

### Terminal lease cleanup

```text
complete/fail
→ all lease fields NULL
```

### Go runtime / concurrency

```text
context cancellation propagates
heartbeat goroutine terminates
no goroutine leak after completed/failed Run
process-tree cancellation works on target OS
go test -race passes for Stage 1 freeze
```

### AWS client contract

```text
no hard-coded LocalStack credentials inside production clients
runtime does not CreateTable/CreateBucket
shared retry configuration
CAS failure classified as authoritative conditional error
S3 NotFound separated from AccessDenied/service error
```

### LocalStack IAM

```text
required worker actions allowed
CreateTable/CreateBucket/admin actions denied
normal E2E succeeds under enforcement
```

### LocalStack resilience

```text
AWS SDK deterministic fault classification
renewal transient failure
bounded retry
no false completion
eventual reclaim
manifest write failure semantics
```

如果当前 LocalStack Ultimate 环境提供可用 chaos/fault injector，则额外执行：

```text
DynamoDB throttling / 5xx
S3 PutObject 5xx
network latency
temporary outage
```

若该 capability 未配置/不可用，可记录为 `BLOCKED — capability`；只要 deterministic fault contract、race、IAM、multi-process 与 process-kill acceptance 全部 PASS，不阻止 Stage 1 freeze。

### Multi-process worker

```text
2+ processes
20+ runs
no double authoritative completion
one real process-kill reclaim/replay
```

### Existing suites

```text
Git pinning/races
RunRepository CAS
renew/reclaim fencing
Kubeconform terminal result coverage
per-target Terraform provenance
Evaluator precedence
toolchain/Clock/limits/API
fake-AI LocalStack E2E
live TFLint/Kubeconform
```

---

## 63. Scope Freeze

v0.8.7 不改变 PlatformLens 核心后端架构；Stage 1 已完成厚 LocalStack 验证，Stage 2 做 reference-driven behavior-preserving polish，Stage 2.5 明确新增轻量 Web Console presentation layer，Stage 3 执行薄 Real AWS final validation。

V1 / Stage 1 包含：

```text
isolated Git runtime
pin-once OID
repo locking
workspace lifecycle
RunRepository CAS contracts

lease/heartbeat/fencing
expected-expiry reclaim
replay/cancellation

Terraform/Kubernetes discovery
producer adapters
live TFLint/Kubeconform
resource-level Kubeconform
per-target provider/module provenance

ValidationResult/Diagnostic
path confinement
Evidence/redaction/context

Reviewer
deterministic evaluator
semantic evaluator
AI degraded mode

toolchain.lock
Clock
logging
limits
health/readiness/version

SQLite/DynamoDB
Filesystem/S3
shared AWS SDK config
Terraform-owned LocalStack infrastructure
LocalStack least-privilege IAM validation
LocalStack fault/latency acceptance
multi-process worker acceptance
fake-AI E2E
manifest commit
```

LocalStack chaos / IAM test machinery 是：

```text
test/acceptance infrastructure
```

不是新的 PlatformLens product workflow。

明确不包含：

```text
Git diff
source archival
Backstage
GitHub/GitLab App
PR comments
Idempotency-Key
full event sourcing
auto-remediation
Agent-generated code/tools
Terraform generation
Helm rendering
Kustomize rendering
Kubernetes API execution
SQS
Step Functions
EKS
Kafka
RAG
Vector DB
multi-cloud
hostile-repository sandbox
LangGraph

real-AWS load testing
real-AWS chaos campaign
always-on AWS compute
large multi-tenant SaaS control plane
frontend direct AWS/database access
production autoscaling platform
```

---

## 64. Final Delivery Strategy

PlatformLens 的核心架构与 frozen invariants 在所有阶段保持不变。

v0.8.7 的最终顺序：

```text
Stage 1
Thick LocalStack Ultimate
Build + Prove
PASS / FREEZE
        ↓
Stage 2
Reference-Driven Code Polish
Behavior-Preserving Refactor
Full Regression
RE-FREEZE
        ↓
Stage 2.5
Web Console / Operator UX
Presentation Layer
UI Acceptance
FREEZE
        ↓
Stage 3
Thin Real AWS Final Validation
Reality Check on Final Product
```

---

### 64.0 Current Implementation Baseline — 2026-09-19

Stage 1 已完成并正式冻结。

已验证：

```text
Go core implementation                 PASS
Git pin/replay                         PASS
SQLite/DynamoDB CAS                    PASS
Filesystem/S3                          PASS
Terraform/TFLint/Kubeconform           PASS
Evidence/AI/Manifest                   PASS
AWS runtime contract                   PASS
LocalStack IAM enforcement             PASS
LocalStack DynamoDB/S3 E2E             PASS
LocalStack lifecycle concurrency       PASS
go test ./...                          PASS
go test -race ./...                    PASS
live TFLint                            PASS
live Kubeconform                       PASS
2-process / 20-run acceptance          PASS
real process-kill reclaim/replay       PASS
manifest read-back/hash verification   PASS
```

LocalStack chaos/fault injector：

```text
BLOCKED — current environment capability not configured
```

但 deterministic failure contract 已验证，因此：

```text
Stage 1 = PASS / FREEZE
```

---

### 64.1 Stage 1 — Thick LocalStack Ultimate / Build & Prove

Stage 1 职责已经完成：

> **不上真实 AWS，也尽可能把 PlatformLens 的产品逻辑、AWS-compatible contract、least privilege、concurrency、recovery、manifest authority 和 multi-worker behavior 验证完成。**

包含：

```text
Stage 1A–1G
Foundation
Git Source
Run Lifecycle
Deterministic Analysis
AI Layer
DynamoDB/S3 LocalStack Backend
Local Failure/E2E

Stage 1H
AWS Runtime Contract Hardening

Stage 1I
LocalStack IAM / Resilience / Multi-Worker Acceptance

Stage 1J
Final Local Freeze Gate
```

后续不再扩大 Stage 1 核心后端 scope。

---

### 64.2 Stage 2 — Reference-Driven Code Polish / Re-Freeze

Stage 2 在不改变产品行为的前提下，对真实后端代码做工程质量提升。

参考四个仓：

```text
ANZ golden-retriever
CBA project-echo
CBA architect-agent
ANZ pkg
```

参考职责：

```text
golden-retriever
→ Git abstraction / synchronization

project-echo
→ lifecycle / persistence / recovery / cleanup

architect-agent
→ reviewer / evaluator / structured AI

ANZ pkg
→ Clock / config / logging / health / runtime hygiene
```

允许：

```text
package/module boundary cleanup
naming consistency
error taxonomy
logging consistency
configuration ownership
concurrency/recovery readability
resource cleanup
duplicate-code reduction
test readability
fixture quality
comments/documentation
```

禁止：

```text
new backend product feature
new AWS service
architecture redesign
workflow engine replacement
frozen invariant changes for stylistic reasons
mechanical source copying
```

每批 risky refactor：

```text
characterization test
→ small change
→ focused test
→ full regression periodically
```

Stage 2 输出：

```text
STAGE2-REFERENCE-COMPARISON-REPORT.md
STAGE2-REFACTOR-PLAN.md
STAGE2-RE-FREEZE-REPORT.md
```

Stage 2 Re-Freeze Gate：

```text
go fmt ./...
go vet ./...
go test ./...
go test -race ./...

live TFLint
live Kubeconform

SQLite/Filesystem E2E
LocalStack DynamoDB/S3 E2E
LocalStack IAM restricted E2E
DynamoDB atomic lifecycle/CAS
Git pin/replay
heartbeat/reclaim
multi-process 20+ runs
real process-kill reclaim/replay
manifest read-back/hash
Terraform provenance
```

全部 PASS 后：

```text
Stage 2 = PASS / RE-FREEZE
```

---

### 64.3 Stage 2.5 — Web Console / Operator UX

Stage 2.5 新增轻量 Web Console，使 PlatformLens 的运行状态、证据链和恢复过程可观察、可演示。

目标：

> **为已经稳定的 PlatformLens backend 增加 presentation layer，而不是重写业务逻辑。**

技术栈：

```text
React
TypeScript
Vite
```

默认架构：

```text
Browser
  ↓
React Web Console
  ↓
PlatformLens Go HTTP API
  ↓
Application Service
  ↓
RunRepository / ArtifactStorage
```

前端不得直连：

```text
DynamoDB
S3
SQLite
Filesystem
workspace
```

核心页面：

```text
Dashboard
New Analysis
Runs
Run Detail
```

#### 64.3.1 Dashboard

显示：

```text
recent runs
state counts
completed/failed summary
recent recovery/reclaim events
backend/ready/version summary
```

Dashboard 只做 presentation，不引入新的 analytics data store。

#### 64.3.2 New Analysis

输入：

```text
repository_url
requested_ref
requested_path?
```

提交：

```text
POST /analysis
```

成功后跳转 Run Detail。

#### 64.3.3 Runs

允许：

```text
pagination
bounded state filter
repository filter
updated-time sort
```

推荐 backend endpoint：

```text
GET /analysis
```

不得通过前端自己扫描 artifact storage 构建 runs 列表。

#### 64.3.4 Run Detail

至少展示：

```text
Run ID
state
attempt_no
winning_attempt
lease/worker summary when active

Source Identity:
repository_url
requested_ref
resolved_ref
ref_type
commit_oid

Validation:
Terraform
TFLint
Kubeconform
coverage
analysis outcome

Diagnostics
Evidence
AI Findings
Evaluation
Terraform dependency provenance
Kubernetes resource results
Artifacts
Report
Manifest
```

Lifecycle timeline：

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

Attempt / recovery timeline：

```text
Attempt 1
→ worker lost / lease expired

Attempt 2
→ reclaim
→ replay pinned commit
→ COMPLETED
```

如果当前 persistence model 不保存足够完整的历史事件，UI 不得虚构历史。

允许两种实现：

```text
1. 只展示现有 authoritative current/final state
2. 增加 bounded audit/read model
```

若选择 2，必须保持：

```text
audit data is observational
not authoritative for claim/lease/fencing
```

且不得改变 RunRepository safety contract。

#### 64.3.5 Read/Presentation APIs

Stage 2.5 允许增加：

```text
GET /analysis
GET /analysis/{run_id}/artifacts
GET /analysis/{run_id}/report
```

必要时增加 typed frontend DTO。

必须：

```text
bounded responses
safe path/artifact lookup
redaction preserved
no secret leakage
no raw credential-bearing URL
no arbitrary filesystem read
```

#### 64.3.6 Frontend Error/State Handling

前端至少区分：

```text
loading
empty
active
completed
failed
backend unavailable
artifact unavailable
review unavailable
evaluation unavailable
```

不得把：

```text
AI unavailable
```

显示为：

```text
Run failed
```

如果 deterministic run 已 COMPLETED。

#### 64.3.7 Stage 2.5 Acceptance

Frontend：

```text
npm/pnpm install with lockfile
typecheck PASS
unit/component tests PASS
production build PASS
```

Backend regression：

```text
go vet PASS
go test PASS
go test -race PASS
```

Functional UI acceptance：

```text
submit analysis from UI PASS
observe active lifecycle PASS
view completed result PASS
view diagnostics PASS
view evidence PASS
view AI findings/evaluation PASS
view Terraform provenance PASS
view Kubernetes results PASS
view report PASS
view manifest PASS
failed-run rendering PASS
AI-unavailable rendering PASS
```

LocalStack demo：

```text
Web Console
→ Go backend
→ LocalStack DynamoDB/S3
→ full analysis
→ COMPLETED
→ UI renders authoritative result
```

Stage 2.5 输出：

```text
STAGE2.5-WEB-CONSOLE-ACCEPTANCE-REPORT.md
```

Completion criteria：

```text
frontend build/test PASS
UI E2E PASS
backend regression PASS
no frozen invariant regression
no direct cloud/database access from frontend
```

成功后：

```text
Stage 2.5 = PASS / FREEZE
```

---

### 64.4 Stage 3 — Thin Real AWS Final Validation

Stage 3 使用 **Stage 2.5 freeze 后的最终 backend + frontend**。

不重新开发 PlatformLens。

保持：

```text
same Stage 2.5 frozen Go backend
same AWS SDK client implementation
same DynamoDB repository
same S3 storage
same IAM policy intent
same state machine
same evidence/manifest schemas
same Web Console
```

默认：

```text
local Go worker(s)
+ local Web Console
→ real AWS public endpoints
```

因此默认不要求：

```text
EC2
ECS
EKS
always-on compute
```

#### AWS-01 Ephemeral Provisioning

Terraform 创建：

```text
DynamoDB table + GSI
S3 artifact bucket
least-privilege IAM principal/role
basic CloudWatch resources if needed
```

#### AWS-02 IAM / Runtime Smoke

验证：

```text
required runtime actions allowed
CreateTable/CreateBucket/admin action denied
real credential chain works
TLS/network works
```

#### AWS-03 Happy Path

运行极少量 deterministic fixture：

```text
submit
→ claim
→ pin
→ validate
→ manifest to real S3
→ read-back/hash
→ COMPLETED
```

#### AWS-04 Recovery Smoke

两个本地 worker：

```text
worker A claim/pin
→ kill A
→ lease expiry
→ worker B reclaim
→ replay pinned commit
→ COMPLETED
```

只需要一个 recovery case。

#### AWS-05 Web Console Smoke

至少执行一次：

```text
Web Console
→ submit Run
→ backend uses real AWS
→ Run completes
→ UI shows final result/report/manifest
```

目的只证明最终 presentation layer 能配合真实 AWS backend 工作，不做大规模 browser E2E matrix。

#### AWS-06 GSI / CloudWatch / Cost

记录：

```text
GSI candidate discovery
CloudWatch logs/metrics evidence
resource list
validation duration
approximate cost
```

#### AWS-07 Cleanup

同一 validation session 结束：

```text
terraform destroy
```

确认测试资源已清理。

Stage 3 completion：

```text
provision PASS
IAM allow/deny PASS
happy path PASS
one recovery PASS
one UI-driven smoke PASS
GSI/S3 smoke PASS
CloudWatch evidence captured
cost evidence captured
destroy PASS
```

原则：

> **Stage 1 proves correctness; Stage 2 polishes the backend; Stage 2.5 makes the system observable and demoable; Stage 3 validates the final product on real AWS.**

如果 Stage 3 暴露真实 AWS 差异：

```text
minimal bugfix only
→ rerun affected regression
→ rerun affected Stage 3 smoke
```

不得借 Stage 3 扩大 scope。

---


## 65. Final Invariants

> **1. A Run pins one verified commit OID at most once.**  
> **2. Every Run mutation is guarded by explicit conditional ownership/state checks.**  
> **3. Reclaim uses the observed lease expiry as part of CAS.**  
> **4. Recovery always creates a new fenced attempt and replays from pinned source.**  
> **5. Terminal Runs have no active lease metadata.**  
> **6. Every supported Kubernetes resource reaches exactly one terminal validation result.**  
> **7. Terraform provider/module provenance is recorded per target.**  
> **8. Module content hashes and manifest hashes use versioned canonical algorithms.**  
> **9. Deterministic evidence validation always precedes semantic evaluation.**  
> **10. Required CI E2E remains deterministic and does not depend on a live LLM.**  
> **11. Runtime AWS clients do not implicitly provision DynamoDB/S3 infrastructure.**  
> **12. LocalStack and Real AWS use the same AWS SDK implementations; endpoint/credentials are configuration only.**  
> **13. A Run cannot become COMPLETED until the authoritative manifest write is confirmed and its exact bytes are hashed.**  
> **14. Transient cloud failures are distinguished from authoritative fencing loss and cannot silently produce false completion.**

> **No further backend architecture redesign is required. Stage 1 is frozen; next work is Stage 2 reference-driven polish, Stage 2.5 Web Console, then Stage 3 thin Real AWS final validation.**
