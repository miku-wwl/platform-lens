# PlatformLens

PlatformLens is a Go local-first, evidence-driven infrastructure analysis service. It pins one verified Git commit, executes deterministic Terraform/Kubernetes validation, binds diagnostics to evidence, evaluates findings, and commits a canonical report manifest.

## Stage 1 structure

The service is a single Go binary under `cmd/platformlens`. Architecture boundaries live under `internal/`: source acquisition, fenced run persistence, command execution, discovery, validation adapters, evidence, review/evaluation, storage, reporting, and HTTP API.

Stage 1 supports SQLite/filesystem mode and AWS-compatible DynamoDB/S3 mode through LocalStack Ultimate. Real AWS is intentionally out of scope; Stage 2 changes endpoints/configuration without changing the contracts. Stage 3 reference-driven polishing is also out of scope.

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
go run ./cmd/platformlens analyze --ref main C:\path\to\fixture-repository
go run ./cmd/platformlens get RUN_ID
go run ./cmd/platformlens serve
```

The HTTP API listens on `127.0.0.1:8000` by default. `POST /analysis` accepts `{ "repository_url": "https://...", "requested_ref": "main", "requested_path": "" }`; local absolute paths are allowed for fixtures only. `GET /analysis/{run_id}` returns the fenced run state.

## LocalStack Ultimate mode

The running LocalStack endpoint is configured with `AWS_ENDPOINT_URL=http://localhost:4566`. Provision the AWS-compatible resources using the checked-in Terraform:

```powershell
$env:AWS_ACCESS_KEY_ID = "test"
$env:AWS_SECRET_ACCESS_KEY = "test"
terraform -chdir=infra/localstack init
terraform -chdir=infra/localstack apply -auto-approve
$env:AWS_ENDPOINT_URL = "http://localhost:4566"
go run ./cmd/platformlens serve
```

The Go DynamoDB/S3 implementations are the same implementations intended for Stage 2; only endpoint and credential configuration changes. The E2E gate must use deterministic fake reviewer/evaluator implementations and must not call a live model or real AWS.

## Artifact layout

Artifacts are written under `runs/<run_id>/attempts/<attempt_no>/`. The manifest hashes every pre-manifest artifact, excludes itself, writes one exact canonical JSON byte slice, and persists its SHA-256 with the terminal `COMPLETED` CAS mutation. Failed/reclaimed attempts may leave orphan artifacts; they cannot become authoritative.

## Optional live AI

The required tests use `DeterministicFakeReviewer` and `DeterministicFakeEvaluator`. A live provider may implement the same interfaces later, but no live model or credential is required for Stage 1 tests or acceptance.
