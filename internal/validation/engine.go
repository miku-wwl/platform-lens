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
	"github.com/miku-wwl/platform-lens/internal/evidence"
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
		moduleMetadata := []ModuleMetadata{}
		moduleMetadataAvailable := false
		if initResult.Status == domain.ValidationPass {
			validateResult, validateExec, validateDiag := e.terraformValidate(ctx, target, root)
			output.Results = append(output.Results, validateResult)
			output.Executions = append(output.Executions, validateExec)
			output.Diagnostics = append(output.Diagnostics, validateDiag...)
			lintResult, lintExec, lintDiag := e.tflint(ctx, target, root)
			output.Results = append(output.Results, lintResult)
			output.Executions = append(output.Executions, lintExec)
			output.Diagnostics = append(output.Diagnostics, lintDiag...)
		} else {
			output.Results = append(output.Results,
				e.skippedResult(target.TargetID+"-validate", "VALIDATOR", "terraform-validate", target.TargetID, true),
				e.skippedResult(target.TargetID+"-tflint", "VALIDATOR", "tflint", target.TargetID, false),
			)
		}
		if initResult.Status == domain.ValidationPass {
			var modulesExecution domain.ToolExecution
			var modulesDiagnostics []domain.Diagnostic
			moduleMetadata, moduleMetadataAvailable, modulesExecution, modulesDiagnostics = e.terraformModules(ctx, target, root)
			output.Executions = append(output.Executions, modulesExecution)
			output.Diagnostics = append(output.Diagnostics, modulesDiagnostics...)
		}
		output.Dependencies = append(output.Dependencies, BuildProvenance(workspace, target, moduleMetadata, moduleMetadataAvailable))
	}
	if len(plan.KubernetesResources) > 0 {
		results, execution, diagnostics := e.kubeconform(ctx, workspace, plan.KubernetesResources)
		output.Results = append(output.Results, results...)
		output.Executions = append(output.Executions, execution)
		output.Diagnostics = append(output.Diagnostics, diagnostics...)
	}
	return output, nil
}

func (e *Engine) terraformModules(ctx context.Context, target domain.TerraformTarget, root string) ([]ModuleMetadata, bool, domain.ToolExecution, []domain.Diagnostic) {
	result, version := e.command(ctx, "terraform", []string{"modules", "-json"}, root, e.terraformEnv(target.TargetID, root))
	toolExecution := e.execution("terraform-modules", version, result)
	if result.StartError != "" || result.ExitCode == nil || *result.ExitCode != 0 {
		return nil, false, toolExecution, []domain.Diagnostic{e.diagnostic("terraform-modules", target.TargetID, result, domain.ValidationError)}
	}
	metadata, err := ParseModulesJSON(result.Stdout)
	if err != nil {
		return nil, false, toolExecution, []domain.Diagnostic{e.machineDiagnostic("terraform-modules", target.TargetID, "terraform modules -json returned unusable JSON")}
	}
	return metadata, true, toolExecution, nil
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
	diagnostics := []domain.Diagnostic{}
	if result.ExitCode != nil {
		var payload terraformValidateOutput
		if json.Unmarshal(result.Stdout, &payload) == nil && payload.Valid != nil {
			if *payload.Valid {
				status = domain.ValidationPass
			} else {
				status = domain.ValidationFail
			}
			for _, item := range payload.Diagnostics {
				diagnostics = append(diagnostics, e.terraformDiagnostic(target, item))
			}
		} else if result.StartError == "" {
			diagnostics = append(diagnostics, e.machineDiagnostic("terraform-validate", target.TargetID, "terraform validate returned unusable JSON"))
		}
	}
	return e.convert(target.TargetID+"-validate", "VALIDATOR", "terraform-validate", version, target.TargetID, status, result, diagnostics...)
}
func (e *Engine) tflint(ctx context.Context, target domain.TerraformTarget, root string) (domain.ValidationResult, domain.ToolExecution, []domain.Diagnostic) {
	config := filepath.Join(root, ".platformlens.tflint.hcl")
	_ = os.WriteFile(config, []byte("plugin \"terraform\" { enabled = true }\n"), 0o600)
	result, version := e.command(ctx, "tflint", []string{"--format", "json", "--config", config}, root, nil)
	status := domain.ValidationError
	diagnostics := []domain.Diagnostic{}
	var payload tflintOutput
	parsed := json.Unmarshal(result.Stdout, &payload) == nil
	if parsed {
		for _, item := range payload.Issues {
			diagnostics = append(diagnostics, e.tflintDiagnostic(target, item))
		}
	} else if result.StartError == "" && len(result.Stdout) > 0 {
		diagnostics = append(diagnostics, e.machineDiagnostic("tflint", target.TargetID, "tflint returned unusable JSON"))
	}
	if result.ExitCode != nil {
		switch *result.ExitCode {
		case 0:
			status = domain.ValidationPass
		case 2:
			status = domain.ValidationFail
		}
	}
	return e.convert(target.TargetID+"-tflint", "VALIDATOR", "tflint", version, target.TargetID, status, result, diagnostics...)
}

func (e *Engine) kubeconform(ctx context.Context, workspace string, resources []domain.ResourceIdentity) ([]domain.ValidationResult, domain.ToolExecution, []domain.Diagnostic) {
	files := map[string]bool{}
	for _, resource := range resources {
		files[resource.File] = true
	}
	template := strings.ReplaceAll(e.Toolchain.Lock.Kubeconform.SchemaLocationTemplate, "{commit}", e.Toolchain.Lock.Kubeconform.SchemaRepositoryCommit)
	if template == "" || template == "default" {
		failure := execution.CommandResult{Argv: []string{"kubeconform"}, StartError: "immutable kubeconform schema location is not configured"}
		return e.kubeResults(resources, failure, "", nil)
	}
	args := []string{"-output", "json", "-strict", "-summary", "-verbose", "-schema-location", template, "-kubernetes-version", e.Toolchain.Lock.Kubeconform.KubernetesVersion}
	fileNames := make([]string, 0, len(files))
	for file := range files {
		fileNames = append(fileNames, file)
	}
	sort.Strings(fileNames)
	args = append(args, fileNames...)
	result, version := e.command(ctx, "kubeconform", args, workspace, nil)
	records := map[string]kubeRecord{}
	malformed := false
	var envelope struct {
		Resources []kubeRecord `json:"resources"`
	}
	if json.Unmarshal(result.Stdout, &envelope) == nil && len(envelope.Resources) > 0 {
		for _, record := range envelope.Resources {
			e.addKubeRecord(records, record, &malformed)
		}
	} else {
		for _, line := range strings.Split(string(result.Stdout), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var record kubeRecord
			if json.Unmarshal([]byte(line), &record) != nil || record.Filename == "" {
				malformed = true
				continue
			}
			e.addKubeRecord(records, record, &malformed)
		}
	}
	if result.StartError == "" && len(result.Stdout) > 0 && malformed {
		return e.kubeResults(resources, result, version, records)
	}
	return e.kubeResults(resources, result, version, records)
}

func (e *Engine) addKubeRecord(records map[string]kubeRecord, record kubeRecord, malformed *bool) {
	record.Filename = filepath.ToSlash(filepath.Clean(record.Filename))
	if record.Filename == "." || record.Kind == "" || record.Name == "" {
		*malformed = true
		return
	}
	records[resourceKey(record.Filename, record.Kind, record.Namespace, record.Name, record.Document)] = record
}

type kubeRecord struct {
	Filename  string `json:"filename"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Msg       string `json:"msg"`
	Error     string `json:"error"`
	Document  int    `json:"document"`
}

func (e *Engine) kubeResults(resources []domain.ResourceIdentity, result execution.CommandResult, version string, records map[string]kubeRecord) ([]domain.ValidationResult, domain.ToolExecution, []domain.Diagnostic) {
	results := make([]domain.ValidationResult, 0, len(resources))
	diagnostics := []domain.Diagnostic{}
	for _, resource := range resources {
		status := domain.ValidationError
		var normalized []domain.Diagnostic
		record, found := records[resourceKey(resource.File, resource.Kind, resource.Namespace, resource.Name, resource.DocumentIndex)]
		if !found && resource.Namespace != "" {
			record, found = records[resourceKey(resource.File, resource.Kind, "", resource.Name, resource.DocumentIndex)]
		}
		if found {
			message := record.Message
			if message == "" {
				message = record.Msg
			}
			switch strings.ToLower(record.Status) {
			case "valid", "pass", "statusvalid":
				status = domain.ValidationPass
			case "invalid", "fail", "statusinvalid":
				status = domain.ValidationFail
			case "missing", "schema_missing":
				status = domain.ValidationSkippedSchemaMissing
			default:
				lowerMessage := strings.ToLower(message + " " + record.Error)
				if strings.Contains(lowerMessage, "schema") && (strings.Contains(lowerMessage, "missing") || strings.Contains(lowerMessage, "could not find")) {
					status = domain.ValidationSkippedSchemaMissing
				}
			}
			if message == "" {
				message = record.Error
			}
			if message != "" || status == domain.ValidationFail || status == domain.ValidationError {
				if message == "" {
					message = record.Error
				}
				normalized = []domain.Diagnostic{{DiagnosticID: runtime.NewID(), Producer: "kubeconform", Resource: &resource, Severity: "ERROR", Message: redacted(message)}}
			}
		}
		if !found && (result.StartError != "" || result.ExitCode == nil || len(records) == 0 || (result.ExitCode != nil && *result.ExitCode != 0)) {
			message := result.StartError
			if message == "" {
				message = "kubeconform output did not contain this resource"
			}
			normalized = []domain.Diagnostic{{DiagnosticID: runtime.NewID(), Producer: "kubeconform", Resource: &resource, Severity: "ERROR", Message: redacted(message)}}
		}
		if len(normalized) > 0 {
			diagnostics = append(diagnostics, normalized...)
		}
		results = append(results, e.result("kubeconform-"+resource.File+"-"+itoa(resource.DocumentIndex), "VALIDATOR", "kubeconform", version, "", true, status, result, &resource, normalized...))
	}
	execution := e.execution("kubeconform", version, result)
	if result.StartError != "" || result.ExitCode == nil {
		for _, resource := range resources {
			_, found := records[resourceKey(resource.File, resource.Kind, resource.Namespace, resource.Name, resource.DocumentIndex)]
			if !found && resource.Namespace != "" {
				_, found = records[resourceKey(resource.File, resource.Kind, "", resource.Name, resource.DocumentIndex)]
			}
			if !found {
				diagnostics = append(diagnostics, domain.Diagnostic{DiagnosticID: runtime.NewID(), Producer: "kubeconform", TargetID: resource.File, Severity: "ERROR", Message: redacted(result.StartError)})
			}
		}
	}
	return results, execution, diagnostics
}

func (e *Engine) command(ctx context.Context, executable string, args []string, cwd string, env map[string]string) (execution.CommandResult, string) {
	actual, err := e.Toolchain.Verify(executable, e.Config.AllowToolchainOverride)
	if err != nil {
		return execution.CommandResult{Argv: append([]string{executable}, args...), StartError: err.Error()}, actual
	}
	return e.Runner.Run(ctx, execution.CommandSpec{Executable: executable, Args: args, WorkingDirectory: cwd, Environment: env, AllowedExecutables: map[string]bool{executable: true}, Timeout: time.Duration(e.Config.Limits.CommandTimeoutSeconds) * time.Second, StdoutCap: e.Config.Limits.MaxRawOutputBytes, StderrCap: e.Config.Limits.MaxRawOutputBytes}), actual
}
func (e *Engine) convert(stepID, kind, producer, version, targetID string, status domain.ValidationStatus, result execution.CommandResult, normalized ...domain.Diagnostic) (domain.ValidationResult, domain.ToolExecution, []domain.Diagnostic) {
	diagnostics := []domain.Diagnostic{}
	diagnostics = append(diagnostics, normalized...)
	if status != domain.ValidationPass && status != domain.ValidationSkipped {
		if len(normalized) == 0 {
			diagnostics = append(diagnostics, e.diagnostic(producer, targetID, result, status))
		}
	}
	return e.result(stepID, kind, producer, version, targetID, kind != "VALIDATOR" || producer != "tflint", status, result, nil, normalized...), e.execution(producer, version, result), diagnostics
}
func (e *Engine) result(stepID, kind, producer, version, targetID string, required bool, status domain.ValidationStatus, result execution.CommandResult, resource *domain.ResourceIdentity, diagnostics ...domain.Diagnostic) domain.ValidationResult {
	var code *int
	if result.ExitCode != nil {
		value := *result.ExitCode
		code = &value
	}
	return domain.ValidationResult{StepID: stepID, StepKind: kind, Producer: producer, ProducerVersion: version, TargetID: targetID, Resource: resource, Required: required, Status: status, DurationMS: result.Duration.Milliseconds(), ExitCode: code, Diagnostics: diagnostics}
}
func (e *Engine) skippedResult(stepID, kind, producer, targetID string, required bool) domain.ValidationResult {
	return domain.ValidationResult{StepID: stepID, StepKind: kind, Producer: producer, TargetID: targetID, Required: required, Status: domain.ValidationSkipped, Diagnostics: []domain.Diagnostic{}}
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
	return domain.Diagnostic{DiagnosticID: runtime.NewID(), Producer: producer, TargetID: targetID, Severity: "ERROR", Message: redacted(limitText(message, 16384))}
}

func (e *Engine) machineDiagnostic(producer, targetID, message string) domain.Diagnostic {
	return domain.Diagnostic{DiagnosticID: runtime.NewID(), Producer: producer, TargetID: targetID, Severity: "ERROR", Message: redacted(message)}
}

func redacted(value string) string { result, _ := evidence.Redact(value); return result }

type terraformValidateOutput struct {
	Valid       *bool                 `json:"valid"`
	Diagnostics []terraformDiagnostic `json:"diagnostics"`
}

type sourceRange struct {
	Filename string                     `json:"filename"`
	Start    struct{ Line, Column int } `json:"start"`
	End      struct{ Line, Column int } `json:"end"`
}

type terraformDiagnostic struct {
	Severity string       `json:"severity"`
	Summary  string       `json:"summary"`
	Detail   string       `json:"detail"`
	Range    *sourceRange `json:"range,omitempty"`
}

func (e *Engine) terraformDiagnostic(target domain.TerraformTarget, item terraformDiagnostic) domain.Diagnostic {
	message := item.Summary
	if item.Detail != "" {
		message += ": " + item.Detail
	}
	diagnostic := domain.Diagnostic{DiagnosticID: runtime.NewID(), Producer: "terraform-validate", TargetID: target.TargetID, Severity: strings.ToUpper(item.Severity), Message: redacted(limitText(message, 16384))}
	if item.Range != nil {
		diagnostic.File = normalizeTargetFile(target, item.Range.Filename)
		diagnostic.StartLine = item.Range.Start.Line
		diagnostic.EndLine = item.Range.End.Line
	}
	return diagnostic
}

type tflintOutput struct {
	Issues []tflintIssue `json:"issues"`
}

type tflintIssue struct {
	Rule struct {
		Name string `json:"name"`
	} `json:"rule"`
	Message  string      `json:"message"`
	Severity string      `json:"severity"`
	Range    sourceRange `json:"range"`
}

func (e *Engine) tflintDiagnostic(target domain.TerraformTarget, item tflintIssue) domain.Diagnostic {
	return domain.Diagnostic{DiagnosticID: runtime.NewID(), Producer: "tflint", TargetID: target.TargetID, Severity: strings.ToUpper(item.Severity), RuleCode: item.Rule.Name, Message: redacted(limitText(item.Message, 16384)), File: normalizeTargetFile(target, item.Range.Filename), StartLine: item.Range.Start.Line, EndLine: item.Range.End.Line}
}

func normalizeTargetFile(target domain.TerraformTarget, file string) string {
	file = filepath.ToSlash(file)
	if filepath.IsAbs(file) || target.RootPath == "." || target.RootPath == "" {
		return file
	}
	return filepath.ToSlash(filepath.Join(target.RootPath, filepath.FromSlash(file)))
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
