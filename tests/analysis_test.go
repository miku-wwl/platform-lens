package tests

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miku-wwl/platform-lens/internal/app"
	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/evaluation"
	"github.com/miku-wwl/platform-lens/internal/evidence"
	"github.com/miku-wwl/platform-lens/internal/runtime"
	"github.com/miku-wwl/platform-lens/internal/security"
	"github.com/miku-wwl/platform-lens/internal/validation"
)

func TestPathConfinement(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "inside.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := security.ResolveExistingWithin(root, "inside.txt"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"../outside.txt", filepath.Join(root, "inside.txt")} {
		if _, err := security.ResolveExistingWithin(root, value); !errors.Is(err, security.ErrPathOutsideWorkspace) {
			t.Fatalf("path %q should be rejected: %v", value, err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := security.ResolveOutputWithin(root, "nested/output.json"); err != nil {
		t.Fatal(err)
	}
}

func TestModuleHashIgnoresMtimeAndAbsoluteRoot(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	for _, root := range []string{left, right} {
		if err := os.WriteFile(filepath.Join(root, "main.tf"), []byte("resource \"x\" \"y\" {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(root, ".terraform"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".terraform", "ignored"), []byte("different"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := validation.CanonicalModuleTreeHash(left)
	if err != nil {
		t.Fatal(err)
	}
	second, err := validation.CanonicalModuleTreeHash(right)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same content has different hashes: %s %s", first, second)
	}
}

func TestEvidenceRedactionAndEvaluatorPrecedence(t *testing.T) {
	redacted, applied := evidence.Redact("token=abc password: secret")
	if !applied || redacted == "token=abc password: secret" {
		t.Fatalf("redaction failed: %q", redacted)
	}
	clock := runtime.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	diagnostic := domain.Diagnostic{DiagnosticID: "d1", Producer: "terraform", Message: "bad"}
	item := evidence.Diagnostic("run", 1, "commit", diagnostic, clock)
	finding := domain.Finding{FindingID: "f1", EvidenceIDs: []string{item.EvidenceID}}
	valid, structural := (evaluation.DeterministicEvaluator{}).Validate([]domain.Finding{finding}, []domain.EvidenceEnvelope{item}, "wrong-run", 1, "commit")
	if len(valid) != 0 || len(structural.Items) != 1 || structural.Items[0].Verdict != domain.VerdictUnsupported {
		t.Fatalf("identity mismatch must be unsupported: %+v %+v", valid, structural)
	}
	valid, structural = (evaluation.DeterministicEvaluator{}).Validate([]domain.Finding{finding}, []domain.EvidenceEnvelope{item}, "run", 1, "commit")
	if len(valid) != 1 || len(structural.Items) != 0 {
		t.Fatalf("valid structure rejected: %+v %+v", valid, structural)
	}
	output, err := (evaluation.UnavailableEvaluator{}).Evaluate(context.Background(), evaluation.Input{Findings: valid, Evidence: []domain.EvidenceEnvelope{item}})
	if err == nil || output.Items != nil {
		t.Fatalf("unavailable evaluator should return error")
	}
}

func TestOutcomeRules(t *testing.T) {
	limits := runtime.DefaultConfig().Limits
	result := []domain.ValidationResult{{StepID: "v", StepKind: "VALIDATOR", Required: true, Status: domain.ValidationPass}}
	outcome, coverage := appOutcome(result, limits)
	if outcome != domain.OutcomeNoFindings || coverage != domain.CoverageComplete {
		t.Fatalf("unexpected no-findings outcome: %s %s", outcome, coverage)
	}
	result[0].Status = domain.ValidationFail
	outcome, _ = appOutcome(result, limits)
	if outcome != domain.OutcomeFindings {
		t.Fatalf("fail should produce findings: %s", outcome)
	}
	result[0].Status = domain.ValidationError
	outcome, coverage = appOutcome(result, limits)
	if outcome != domain.OutcomeInconclusive || coverage != domain.CoveragePartial {
		t.Fatalf("required error should be inconclusive: %s %s", outcome, coverage)
	}
}

func appOutcome(results []domain.ValidationResult, limits runtime.Limits) (domain.AnalysisOutcome, domain.CoverageStatus) {
	return app.Outcome(results, limits)
}
