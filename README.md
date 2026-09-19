# PlatformLens

PlatformLens is a Go local-first, evidence-driven infrastructure analysis service. It pins one verified Git commit, executes deterministic Terraform/Kubernetes validation, binds diagnostics to evidence, evaluates findings, and commits a canonical report manifest.

## Delivery roadmap

The service is a single Go binary under `cmd/platformlens`. Architecture boundaries live under `internal/`: source acquisition, fenced run persistence, command execution, discovery, validation adapters, evidence, review/evaluation, storage, reporting, and HTTP API.

The frozen v0.8.7 delivery roadmap is:

1. **Stage 1 — Thick LocalStack Ultimate: Build & Prove** — `PASS / FREEZE`
2. **Stage 2 — Reference-Driven Code Polish** — behavior-preserving refactor and full regression, `PASS / RE-FREEZE`
3. **Stage 2.5 — Web Console / Operator UX** — React + TypeScript + Vite
4. **Stage 3 — Thin Real AWS Final Validation**

Stage 1 supports SQLite/filesystem mode and AWS-compatible DynamoDB/S3 mode through LocalStack Ultimate. Real AWS is intentionally reserved for Stage 3.

## Requirements

- Go 1.24.2 (the exact project toolchain is recorded in `toolchain.lock`)
- Git
- Terraform 1.14.0
- TFLint 0.55.1 and Kubeconform 0.6.7 for complete validator coverage
- Docker Desktop with LocalStack Ultimate for AWS-compatible E2E

The current development machine has Go and Terraform. TFLint/Kubeconform are checked at runtime and missing tools produce explicit validation errors; they are never silently replaced by mocks.

## Build and test

```powershell
go mod download
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
go build -o .platformlens/platformlens.exe ./cmd/platformlens
```

## SQLite/filesystem mode

```powershell
go run ./cmd/platformlens toolchain
$env:PLATFORMLENS_ALLOW_LOCAL_GIT = "true" # TEST/DEV fixture use only
$env:PLATFORMLENS_DATA_DIR = (Join-Path (Get-Location) ".platformlens")
$env:PLATFORMLENS_WORKSPACE_DIR = (Join-Path $env:PLATFORMLENS_DATA_DIR "workspaces")
$env:PLATFORMLENS_SOURCE_CACHE_DIR = (Join-Path $env:PLATFORMLENS_DATA_DIR "source-cache")
$env:PLATFORMLENS_ARTIFACT_DIR = (Join-Path $env:PLATFORMLENS_DATA_DIR "artifacts")
go run ./cmd/platformlens analyze --ref main C:\path\to\fixture-repository
go run ./cmd/platformlens get RUN_ID
go run ./cmd/platformlens serve
```

The absolute directory settings above are required for the local Git fixture workflow because Git worktrees are materialized from the source cache. The HTTP API listens on `127.0.0.1:8000` by default. `POST /analysis` accepts `{ "repository_url": "https://...", "requested_ref": "main", "requested_path": "" }`; production/default source handling accepts HTTPS repositories only. A local absolute path is accepted only when `PLATFORMLENS_ALLOW_LOCAL_GIT=true`, which is a TEST/DEV fixture opt-in. `GET /analysis/{run_id}` returns the fenced run state.

## LocalStack Ultimate mode

Select the AWS-compatible backend explicitly and point it at the running LocalStack endpoint. `AWS_ENDPOINT_URL` alone does not select the backend. Provision the AWS-compatible resources using the checked-in Terraform; the runtime does not create the DynamoDB table or S3 bucket automatically:

```powershell
terraform -chdir=infra/localstack init
terraform -chdir=infra/localstack apply -auto-approve
$env:PLATFORMLENS_BACKEND = "aws"
$env:AWS_ENDPOINT_URL = "http://localhost:4566"
$env:AWS_REGION = "us-east-1"
$env:AWS_ACCESS_KEY_ID = (terraform -chdir=infra/localstack output -raw worker_access_key_id).Trim()
$env:AWS_SECRET_ACCESS_KEY = (terraform -chdir=infra/localstack output -raw worker_secret_access_key).Trim()
go run ./cmd/platformlens serve
```

The Go DynamoDB/S3 implementations are shared by the LocalStack Stage 1 gate and the thin real-AWS Stage 3 validation; only AWS configuration and endpoint selection change. The E2E gate uses deterministic fake reviewer/evaluator implementations and does not call a live model or real AWS.

## Stage 2.5 Web Console / Operator UX

The `web/` directory contains the Stage 2.5 React + TypeScript + Vite presentation layer. The browser talks only to the Go HTTP API; it does not access DynamoDB, S3, LocalStack, SQLite, filesystem paths, or AWS credentials directly.

Requirements: Node.js and npm. Start the backend in one terminal, then start the Vite console in another:

```powershell
# Terminal 1: SQLite/filesystem backend
go run ./cmd/platformlens serve

# Terminal 2: Web Console
Set-Location web
npm ci
npm run dev
```

Open `http://localhost:5173`. The Vite development proxy forwards `/analysis`, `/healthz`, `/readyz`, and `/version` to `http://127.0.0.1:8000`. For LocalStack, start the backend with the LocalStack environment shown above; the same console and proxy are used.

Frontend gates:

```powershell
Set-Location web
npm ci
npm run typecheck
npm test
npm run build
```

The console provides Dashboard, New Analysis, Runs, and Run Detail surfaces. Run Detail polls active runs, renders the defined lifecycle with only the authoritative current state highlighted, and reads report/manifest/evidence through bounded backend endpoints. Local Git fixture analysis remains TEST/DEV-only and still requires `PLATFORMLENS_ALLOW_LOCAL_GIT=true` in the backend process.

## Artifact layout

Artifacts are written under `runs/<run_id>/attempts/<attempt_no>/`. The manifest hashes every pre-manifest artifact, excludes itself, writes one exact canonical JSON byte slice, and persists its SHA-256 with the terminal `COMPLETED` CAS mutation. Failed/reclaimed attempts may leave orphan artifacts; they cannot become authoritative.

## Optional live AI

The required tests use `DeterministicFakeReviewer` and `DeterministicFakeEvaluator`. A live provider may implement the same interfaces later, but no live model or credential is required for Stage 1 tests or acceptance.
