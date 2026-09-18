package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/miku-wwl/platform-lens/internal/discovery"
	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/execution"
	"github.com/miku-wwl/platform-lens/internal/runtime"
)

type Engine struct {
	Config    runtime.Config
	Runner    *execution.CommandRunner
	Toolchain runtime.Toolchain
	Discovery discovery.Discovery
}

func NewEngine(config runtime.Config, runner *execution.CommandRunner, toolchain runtime.Toolchain) *Engine {
	return &Engine{Config: config, Runner: runner, Toolchain: toolchain, Discovery: discovery.Discovery{Limits: config.Limits}}
}

type PlanOutput struct {
	Plan                domain.ValidationPlan
	TerraformTargets    []domain.TerraformTarget
	KubernetesResources []domain.ResourceIdentity
	Skipped             []map[string]string
}

func (e *Engine) BuildPlan(workspace, requestedPath string) (PlanOutput, error) {
	targets, err := e.Discovery.Terraform(workspace, requestedPath)
	if err != nil {
		return PlanOutput{}, err
	}
	resources, skipped, err := e.Discovery.Kubernetes(workspace)
	if err != nil {
		return PlanOutput{}, err
	}
	plan := domain.ValidationPlan{SchemaVersion: 1, ScopeChecks: []domain.ValidationStep{{StepID: "scope-source", Kind: "SCOPE_CHECK", Required: true, Producer: "PlatformLens"}}}
	for _, target := range targets {
		plan.Preparations = append(plan.Preparations, domain.ValidationStep{StepID: target.TargetID + "-init", Kind: "PREPARATION", TargetID: target.TargetID, Required: true, Producer: "terraform-init"})
		plan.Validators = append(plan.Validators, domain.ValidationStep{StepID: target.TargetID + "-fmt", Kind: "SCOPE_CHECK", TargetID: target.TargetID, Required: true, Producer: "terraform-fmt"}, domain.ValidationStep{StepID: target.TargetID + "-validate", Kind: "VALIDATOR", TargetID: target.TargetID, Required: true, Producer: "terraform-validate"}, domain.ValidationStep{StepID: target.TargetID + "-tflint", Kind: "VALIDATOR", TargetID: target.TargetID, Required: false, Producer: "tflint"})
	}
	if len(resources) > 0 {
		plan.Validators = append(plan.Validators, domain.ValidationStep{StepID: "kubernetes-kubeconform", Kind: "VALIDATOR", Required: true, Producer: "kubeconform"})
	}
	return PlanOutput{Plan: plan, TerraformTargets: targets, KubernetesResources: resources, Skipped: skipped}, nil
}

type Output struct {
	Results      []domain.ValidationResult
	Executions   []domain.ToolExecution
	Diagnostics  []domain.Diagnostic
	Dependencies []domain.TerraformDependencyProvenance
}

func (e *Engine) Execute(ctx context.Context, workspace string, plan PlanOutput) (Output, error) {
	output := Output{}
	for _, target := range plan.TerraformTargets {
		root := filepath.Join(workspace, filepath.FromSlash(target.RootPath))
		fmtResult, fmtExec, fmtDiag := e.terraformFmt(ctx, target, root)
		output.Results = append(output.Results, fmtResult)
		output.Executions = append(output.Executions, fmtExec)
		output.Diagnostics = append(output.Diagnostics, fmtDiag...)
		initResult, initExec, initDiag := e.terraformInit(ctx, target, root)
		output.Results = append(output.Results, initResult)
		output.Executions = append(output.Executions, initExec)
		output.Diagnostics = append(output.Diagnostics, initDiag...)
		validateResult, validateExec, validateDiag := e.terraformValidate(ctx, target, root)
		output.Results = append(output.Results, validateResult)
		output.Executions = append(output.Executions, validateExec)
		output.Diagnostics = append(output.Diagnostics, validateDiag...)
		lintResult, lintExec, lintDiag := e.tflint(ctx, target, root)
		output.Results = append(output.Results, lintResult)
		output.Executions = append(output.Executions, lintExec)
		output.Diagnostics = append(output.Diagnostics, lintDiag...)
		output.Dependencies = append(output.Dependencies, BuildProvenance(workspace, target))
	}
	if len(plan.KubernetesResources) > 0 {
		results, execution, diagnostics := e.kubeconform(ctx, workspace, plan.KubernetesResources)
		output.Results = append(output.Results, results...)
		output.Executions = append(output.Executions, execution)
		output.Diagnostics = append(output.Diagnostics, diagnostics...)
	}
	return output, nil
}

func (e *Engine) terraformEnv(targetID, root string) map[string]string {
	dataDir := filepath.Join(root, ".platformlens-tfdata", targetID)
	_ = os.MkdirAll(dataDir, 0o700)
	cli := filepath.Join(root, ".platformlens.tfrc")
	_ = os.WriteFile(cli, []byte("plugin_cache_dir = \"\"\n"), 0o600)
	return map[string]string{"TF_IN_AUTOMATION": "1", "TF_INPUT": "0", "TF_DATA_DIR": dataDir, "TF_CLI_CONFIG_FILE": cli}
}

func (e *Engine) terraformFmt(ctx context.Context, target domain.TerraformTarget, root string) (domain.ValidationResult, domain.ToolExecution, []domain.Diagnostic) {
	result, version := e.command(ctx, "terraform", []string{"fmt", "-check", "-recursive", "-no-color"}, root, e.terraformEnv(target.TargetID, root))
	status := domain.ValidationError
	if result.StartError == "" && result.ExitCode != nil {
		if *result.ExitCode == 0 {
			status = domain.ValidationPass
		} else if *result.ExitCode == 3 {
			status = domain.ValidationFail
		}
	}
	return e.convert(target.TargetID+"-fmt", "SCOPE_CHECK", "terraform-fmt", version, target.TargetID, status, result)
}
func (e *Engine) terraformInit(ctx context.Context, target domain.TerraformTarget, root string) (domain.ValidationResult, domain.ToolExecution, []domain.Diagnostic) {
	args := []string{"init", "-backend=false", "-input=false", "-no-color"}
	if target.LockfilePresent {
		args = append(args, "-lockfile=readonly")
	}
	result, version := e.command(ctx, "terraform", args, root, e.terraformEnv(target.TargetID, root))
	status := domain.ValidationError
	if result.StartError == "" && result.ExitCode != nil && *result.ExitCode == 0 {
		status = domain.ValidationPass
	}
	return e.convert(target.TargetID+"-init", "PREPARATION", "terraform-init", version, target.TargetID, status, result)
}
func (e *Engine) terraformValidate(ctx context.Context, target domain.TerraformTarget, root string) (domain.ValidationResult, domain.ToolExecution, []domain.Diagnostic) {
	result, version := e.command(ctx, "terraform", []string{"validate", "-json", "-no-color"}, root, e.terraformEnv(target.TargetID, root))
	status := domain.ValidationError
	if result.ExitCode != nil {
		var payload struct {
			Valid *bool `json:"valid"`
		}
		if json.Unmarshal(result.Stdout, &payload) == nil && payload.Valid != nil {
			if *payload.Valid {
				status = domain.ValidationPass
			} else {
				status = domain.ValidationFail
			}
		}
	}
	return e.convert(target.TargetID+"-validate", "VALIDATOR", "terraform-validate", version, target.TargetID, status, result)
}
func (e *Engine) tflint(ctx context.Context, target domain.TerraformTarget, root string) (domain.ValidationResult, domain.ToolExecution, []domain.Diagnostic) {
	config := filepath.Join(root, ".platformlens.tflint.hcl")
	_ = os.WriteFile(config, []byte("plugin \"terraform\" { enabled = true }\n"), 0o600)
	result, _ := e.command(ctx, "tflint", []string{"--format", "json", "--config", config}, root, nil)
	status := domain.ValidationError
	if result.ExitCode != nil {
		switch *result.ExitCode {
		case 0:
			status = domain.ValidationPass
		case 2:
			status = domain.ValidationFail
		}
	}
	return e.convert(target.TargetID+"-tflint", "VALIDATOR", "tflint", "", target.TargetID, status, result)
}

func (e *Engine) kubeconform(ctx context.Context, workspace string, resources []domain.ResourceIdentity) ([]domain.ValidationResult, domain.ToolExecution, []domain.Diagnostic) {
	files := map[string]bool{}
	for _, resource := range resources {
		files[resource.File] = true
	}
	args := []string{"-output", "json", "-strict", "-kubernetes-version", e.Toolchain.Lock.Kubeconform.KubernetesVersion}
	for file := range files {
		args = append(args, file)
	}
	sort.Strings(args[5:])
	result, version := e.command(ctx, "kubeconform", args, workspace, nil)
	parsed := map[string]string{}
	for _, line := range strings.Split(string(result.Stdout), "\n") {
		var record struct {
			Filename, Kind, Name, Namespace, Status string
			Document                                int `json:"document"`
		}
		if json.Unmarshal([]byte(line), &record) == nil && record.Filename != "" {
			key := resourceKey(record.Filename, record.Kind, record.Namespace, record.Name, record.Document)
			parsed[key] = strings.ToLower(record.Status)
		}
	}
	results := make([]domain.ValidationResult, 0, len(resources))
	for _, resource := range resources {
		status := domain.ValidationError
		switch parsed[resourceKey(resource.File, resource.Kind, resource.Namespace, resource.Name, resource.DocumentIndex)] {
		case "valid", "pass":
			status = domain.ValidationPass
		case "invalid", "fail":
			status = domain.ValidationFail
		case "missing", "schema_missing":
			status = domain.ValidationSkippedSchemaMissing
		}
		results = append(results, e.result("kubeconform-"+resource.File+"-"+itoa(resource.DocumentIndex), "VALIDATOR", "kubeconform", version, "", true, status, result, &resource))
	}
	execution := e.execution("kubeconform", version, result)
	diagnostics := []domain.Diagnostic{}
	if result.StartError != "" || result.ExitCode == nil || *result.ExitCode != 0 {
		diagnostics = append(diagnostics, e.diagnostic("kubeconform", "", result, domain.ValidationError))
	}
	return results, execution, diagnostics
}

func (e *Engine) command(ctx context.Context, executable string, args []string, cwd string, env map[string]string) (execution.CommandResult, string) {
	expected := ""
	switch executable {
	case "terraform":
		expected = e.Toolchain.Expected("terraform")
	case "tflint":
		expected = e.Toolchain.Expected("tflint")
	case "kubeconform":
		expected = e.Toolchain.Expected("kubeconform")
	}
	return e.Runner.Run(ctx, execution.CommandSpec{Executable: executable, Args: args, WorkingDirectory: cwd, Environment: env, AllowedExecutables: map[string]bool{executable: true}, Timeout: time.Duration(e.Config.Limits.CommandTimeoutSeconds) * time.Second, StdoutCap: e.Config.Limits.MaxRawOutputBytes, StderrCap: e.Config.Limits.MaxRawOutputBytes}), expected
}
func (e *Engine) convert(stepID, kind, producer, version, targetID string, status domain.ValidationStatus, result execution.CommandResult) (domain.ValidationResult, domain.ToolExecution, []domain.Diagnostic) {
	diagnostics := []domain.Diagnostic{}
	if status != domain.ValidationPass && status != domain.ValidationSkipped {
		diagnostics = append(diagnostics, e.diagnostic(producer, targetID, result, status))
	}
	return e.result(stepID, kind, producer, version, targetID, kind != "VALIDATOR" || producer != "tflint", status, result, nil), e.execution(producer, version, result), diagnostics
}
func (e *Engine) result(stepID, kind, producer, version, targetID string, required bool, status domain.ValidationStatus, result execution.CommandResult, resource *domain.ResourceIdentity) domain.ValidationResult {
	var code *int
	if result.ExitCode != nil {
		value := *result.ExitCode
		code = &value
	}
	return domain.ValidationResult{StepID: stepID, StepKind: kind, Producer: producer, ProducerVersion: version, TargetID: targetID, Resource: resource, Required: required, Status: status, DurationMS: result.Duration.Milliseconds(), ExitCode: code, Diagnostics: []domain.Diagnostic{}}
}
func (e *Engine) execution(producer, version string, result execution.CommandResult) domain.ToolExecution {
	return domain.ToolExecution{ExecutionID: runtime.NewID(), Producer: producer, ProducerVersion: version, Argv: result.Argv, ExitCode: result.ExitCode, DurationMS: result.Duration.Milliseconds(), OutputTruncated: result.StdoutTruncated || result.StderrTruncated, TimedOut: result.TimedOut, Cancelled: result.Cancelled}
}
func (e *Engine) diagnostic(producer, targetID string, result execution.CommandResult, status domain.ValidationStatus) domain.Diagnostic {
	message := string(result.Stderr)
	if message == "" {
		message = string(result.Stdout)
	}
	if message == "" {
		message = result.StartError
	}
	if message == "" {
		message = fmt.Sprintf("%s returned status %s", producer, status)
	}
	return domain.Diagnostic{DiagnosticID: runtime.NewID(), Producer: producer, TargetID: targetID, Severity: "ERROR", Message: limitText(message, 16384)}
}
func resourceKey(file, kind, namespace, name string, index int) string {
	return strings.Join([]string{file, kind, namespace, name, itoa(index)}, "\x00")
}
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	return result
}
func limitText(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max] + "\n[TRUNCATED]"
}
