package validation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/execution"
	"github.com/miku-wwl/platform-lens/internal/runtime"
)

func TestStructuredValidatorsAndInitGate(t *testing.T) {
	bin := t.TempDir()
	writeExecutable(t, bin, "terraform.cmd", `@echo off
if "%1"=="version" echo Terraform v1.14.0
if "%1"=="version" exit /b 0
if "%1"=="fmt" exit /b 0
if "%1"=="init" if exist initfail (echo init failed 1>&2 & exit /b 1)
if "%1"=="init" exit /b 0
if "%1"=="modules" (echo modules-ran>modules.ran & echo {"modules":[{"key":"child","source":"./child","version":"1.0.0","dir":"modules/child"}]} & exit /b 0)
if "%1"=="validate" (echo validate-ran>validate.ran & echo {"valid":false,"diagnostics":[{"severity":"error","summary":"invalid config","detail":"token=SUPER_SECRET_VALUE","range":{"filename":"main.tf","start":{"line":2,"column":1},"end":{"line":2,"column":3}}}]} & exit /b 1)
exit /b 0
`)
	writeExecutable(t, bin, "tflint.cmd", `@echo off
if "%1"=="--version" echo TFLint version 0.55.1
if "%1"=="--version" exit /b 0
echo tflint-ran>tflint.ran
echo {"issues":[{"rule":{"name":"terraform_required_providers"},"severity":"warning","message":"token=SUPER_SECRET_VALUE","range":{"filename":"main.tf","start":{"line":3,"column":1},"end":{"line":3,"column":2}}}]}
exit /b 2
`)
	writeExecutable(t, bin, "kubeconform.cmd", `@echo off
if "%1"=="-v" echo kubeconform version 0.6.7
if "%1"=="-v" exit /b 0
echo {"filename":"valid.yaml","kind":"Deployment","name":"good","namespace":"default","status":"valid","document":0}
echo {"filename":"invalid.yaml","kind":"Deployment","name":"bad","status":"invalid","message":"invalid spec","document":0}
echo {"filename":"missing.yaml","kind":"Widget","name":"custom","status":"missing","message":"schema missing","document":0}
exit /b 1
`)
	withPath(t, bin, func() {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "main.tf"), []byte("terraform {}\nline2\nline3\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "modules", "child"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "modules", "child", "main.tf"), []byte("module content\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		toolchain := testToolchain(t)
		config := runtime.DefaultConfig()
		config.AllowToolchainOverride = false
		config.Limits.CommandTimeoutSeconds = 10
		engine := NewEngine(config, execution.NewCommandRunner(), toolchain)
		plan := PlanOutput{TerraformTargets: []domain.TerraformTarget{{TargetID: "terraform-1", RootPath: "."}}}
		output, err := engine.Execute(context.Background(), root, plan)
		if err != nil {
			t.Fatal(err)
		}
		if len(output.Results) != 4 {
			t.Fatalf("unexpected Terraform result count: %+v", output.Results)
		}
		validate := output.Results[2]
		if validate.Status != domain.ValidationFail || len(validate.Diagnostics) != 1 || validate.Diagnostics[0].StartLine != 2 || strings.Contains(validate.Diagnostics[0].Message, "SUPER_SECRET_VALUE") {
			t.Fatalf("structured Terraform diagnostics missing/redaction failed: %+v", validate)
		}
		lint := output.Results[3]
		if lint.Status != domain.ValidationFail || len(lint.Diagnostics) != 1 || lint.Diagnostics[0].RuleCode != "terraform_required_providers" || strings.Contains(lint.Diagnostics[0].Message, "SUPER_SECRET_VALUE") {
			t.Fatalf("structured TFLint diagnostics missing/redaction failed: %+v", lint)
		}
		if validate.ProducerVersion != "1.14.0" || lint.ProducerVersion != "0.55.1" {
			t.Fatalf("actual versions not recorded: %+v %+v", validate, lint)
		}
		if _, err := os.Stat(filepath.Join(root, "modules.ran")); err != nil {
			t.Fatalf("terraform modules -json was not invoked: %v", err)
		}
		if len(output.Dependencies) != 1 || len(output.Dependencies[0].Modules) != 1 || output.Dependencies[0].Modules[0].ModuleKey != "child" || output.Dependencies[0].Modules[0].ContentTreeHash == "" || output.Dependencies[0].ModuleProvenanceStatus != "COMPLETE" {
			t.Fatalf("public module metadata was not used for provenance: %+v", output.Dependencies)
		}

		blocked := t.TempDir()
		if err := os.WriteFile(filepath.Join(blocked, "main.tf"), []byte("terraform {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(blocked, "initfail"), []byte("1"), 0o600); err != nil {
			t.Fatal(err)
		}
		blockedOutput, err := engine.Execute(context.Background(), blocked, plan)
		if err != nil {
			t.Fatal(err)
		}
		if len(blockedOutput.Results) != 4 || blockedOutput.Results[1].Status != domain.ValidationError || blockedOutput.Results[2].Status != domain.ValidationSkipped || blockedOutput.Results[2].Producer != "terraform-validate" || blockedOutput.Results[3].Status != domain.ValidationSkipped {
			t.Fatalf("init failure did not gate validators: %+v", blockedOutput.Results)
		}
		if _, err := os.Stat(filepath.Join(blocked, "validate.ran")); !os.IsNotExist(err) {
			t.Fatalf("terraform validate ran after init failure: %v", err)
		}
		if _, err := os.Stat(filepath.Join(blocked, "tflint.ran")); !os.IsNotExist(err) {
			t.Fatalf("tflint ran after init failure: %v", err)
		}

		resources := []domain.ResourceIdentity{
			{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "default", Name: "good", File: "valid.yaml"},
			{APIVersion: "apps/v1", Kind: "Deployment", Name: "bad", File: "invalid.yaml"},
			{APIVersion: "example.io/v1", Kind: "Widget", Name: "custom", File: "missing.yaml"},
		}
		kubeOutput, err := engine.Execute(context.Background(), root, PlanOutput{KubernetesResources: resources})
		if err != nil {
			t.Fatal(err)
		}
		if len(kubeOutput.Results) != len(resources) || kubeOutput.Results[0].Status != domain.ValidationPass || kubeOutput.Results[1].Status != domain.ValidationFail || kubeOutput.Results[2].Status != domain.ValidationSkippedSchemaMissing {
			t.Fatalf("kubeconform result mapping failed: %+v", kubeOutput.Results)
		}
		writeExecutable(t, bin, "kubeconform.cmd", `@echo off
if "%1"=="-v" echo kubeconform version 0.6.7
if "%1"=="-v" exit /b 0
echo not-json
exit /b 0
`)
		malformed, err := engine.Execute(context.Background(), root, PlanOutput{KubernetesResources: resources})
		if err != nil {
			t.Fatal(err)
		}
		for _, result := range malformed.Results {
			if result.Status != domain.ValidationError || len(result.Diagnostics) != 1 {
				t.Fatalf("malformed output was not normalized to per-resource ERROR: %+v", malformed.Results)
			}
		}
		writeExecutable(t, bin, "kubeconform.cmd", `@echo off
if "%1"=="-v" echo kubeconform version 0.6.7
if "%1"=="-v" exit /b 0
exit /b 1
`)
		failedProcess, err := engine.Execute(context.Background(), root, PlanOutput{KubernetesResources: resources})
		if err != nil {
			t.Fatal(err)
		}
		for _, result := range failedProcess.Results {
			if result.Status != domain.ValidationError || len(result.Diagnostics) != 1 {
				t.Fatalf("tool error was not normalized to per-resource ERROR: %+v", failedProcess.Results)
			}
		}
	})
}

func TestParseModulesJSONPublicCLIEnvelope(t *testing.T) {
	metadata, err := ParseModulesJSON([]byte(`{"modules":[{"key":"module.child","source":"registry.example/child","version":"1.2.3","dir":".terraform/modules/child","revision":"abc123"}]}`))
	if err != nil || len(metadata) != 1 || metadata[0].ModuleKey != "module.child" || metadata[0].DeclaredSource != "registry.example/child" || metadata[0].ResolvedVCSRevision != "abc123" {
		t.Fatalf("public CLI module metadata was not parsed: %+v %v", metadata, err)
	}
}

func testToolchain(t *testing.T) runtime.Toolchain {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	toolchain, err := runtime.LoadToolchain(filepath.Join(root, "toolchain.lock"))
	if err != nil {
		t.Fatal(err)
	}
	return toolchain
}

func writeExecutable(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func withPath(t *testing.T, dir string, fn func()) {
	t.Helper()
	old := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+old); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Setenv("PATH", old) }()
	fn()
}
