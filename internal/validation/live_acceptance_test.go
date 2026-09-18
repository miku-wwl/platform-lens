package validation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/execution"
	"github.com/miku-wwl/platform-lens/internal/runtime"
)

// These checks are opt-in because they require the locked external binaries
// and network access to the immutable Kubernetes schema revision.
func TestLiveTFLintAdapterAcceptance(t *testing.T) {
	if os.Getenv("PLATFORMLENS_LIVE_VALIDATORS") != "1" {
		t.Skip("set PLATFORMLENS_LIVE_VALIDATORS=1 to run live validator acceptance")
	}
	root := repositoryRoot(t)
	toolchain := loadLockedToolchain(t, root)
	engine := NewEngine(runtime.DefaultConfig(), execution.NewCommandRunner(), toolchain)
	fixture := filepath.Join(root, "tests", "fixtures", "live-tflint", "issue")
	output, err := engine.Execute(context.Background(), fixture, PlanOutput{TerraformTargets: []domain.TerraformTarget{{TargetID: "terraform-1", RootPath: "."}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range output.Results {
		if result.Producer == "tflint" {
			if result.Status != domain.ValidationFail || result.ProducerVersion != "0.55.1" || result.TargetID != "terraform-1" || len(result.Diagnostics) == 0 || result.Diagnostics[0].RuleCode == "" || result.Diagnostics[0].File != "main.tf" || result.Diagnostics[0].StartLine != 5 {
				t.Fatalf("live TFLint adapter did not normalize issue output: %+v", result)
			}
			return
		}
	}
	t.Fatal("live TFLint result was not produced")
}

func TestLiveKubeconformAdapterAcceptance(t *testing.T) {
	if os.Getenv("PLATFORMLENS_LIVE_VALIDATORS") != "1" {
		t.Skip("set PLATFORMLENS_LIVE_VALIDATORS=1 to run live validator acceptance")
	}
	root := repositoryRoot(t)
	toolchain := loadLockedToolchain(t, root)
	engine := NewEngine(runtime.DefaultConfig(), execution.NewCommandRunner(), toolchain)
	resources := []domain.ResourceIdentity{
		{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "default", Name: "platformlens-valid", File: "tests/fixtures/live-kubeconform/valid.yaml"},
		{APIVersion: "apps/v1", Kind: "Deployment", Name: "platformlens-invalid", File: "tests/fixtures/live-kubeconform/invalid.yaml"},
		{APIVersion: "example.platformlens.invalid/v1", Kind: "PlatformLensCustomThing", Name: "platformlens-custom", File: "tests/fixtures/live-kubeconform/missing-schema.yaml"},
	}
	output, err := engine.Execute(context.Background(), root, PlanOutput{KubernetesResources: resources})
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Results) != 3 {
		t.Fatalf("unexpected live Kubeconform result count: %+v", output.Results)
	}
	if output.Results[0].Status != domain.ValidationPass || output.Results[1].Status != domain.ValidationFail || output.Results[2].Status != domain.ValidationSkippedSchemaMissing {
		t.Fatalf("live Kubeconform adapter mapping failed: %+v", output.Results)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func loadLockedToolchain(t *testing.T, root string) runtime.Toolchain {
	t.Helper()
	toolchain, err := runtime.LoadToolchain(filepath.Join(root, "toolchain.lock"))
	if err != nil {
		t.Fatal(err)
	}
	return toolchain
}
