package tests

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/execution"
	"github.com/miku-wwl/platform-lens/internal/runs"
	"github.com/miku-wwl/platform-lens/internal/runtime"
	"github.com/miku-wwl/platform-lens/internal/source"
)

func TestSQLiteClaimRaceAndTerminalFencing(t *testing.T) {
	clock := runtime.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	repository, err := runs.OpenSQLite(filepath.Join(t.TempDir(), "runs.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	run, err := repository.CreateRun(context.Background(), "https://example.invalid/platform.git", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	winners := []domain.AnalysisRun{}
	errorsSeen := []error{}
	var wait sync.WaitGroup
	for _, worker := range []string{"worker-a", "worker-b"} {
		wait.Add(1)
		go func(worker string) {
			defer wait.Done()
			claimed, claimErr := repository.ClaimRun(context.Background(), run.RunID, worker, time.Minute)
			mu.Lock()
			defer mu.Unlock()
			if claimErr == nil {
				winners = append(winners, claimed)
			} else {
				errorsSeen = append(errorsSeen, claimErr)
			}
		}(worker)
	}
	wait.Wait()
	if len(winners) != 1 || len(errorsSeen) != 1 {
		t.Fatalf("claim race winners=%d errors=%v", len(winners), errorsSeen)
	}
	claimed := winners[0]
	if claimed.AttemptNo != 1 || claimed.State != domain.StateClaimed {
		t.Fatalf("unexpected claim: %+v", claimed)
	}
	clock.Advance(2 * time.Minute)
	reclaimed, err := repository.ReclaimExpiredRun(context.Background(), run.RunID, 1, claimed.LeaseOwner, *claimed.LeaseExpiresAt, domain.StateClaimed, "worker-c", clock.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed.AttemptNo != 2 || reclaimed.State != domain.StateClaimed {
		t.Fatalf("unexpected reclaim: %+v", reclaimed)
	}
	if _, err := repository.UpdatePhase(context.Background(), run.RunID, 1, claimed.LeaseOwner, domain.StateClaimed, domain.StateRetrieving); !errors.Is(err, runs.ErrConditional) {
		t.Fatalf("stale phase mutation should be fenced: %v", err)
	}
	if _, err := repository.FailRun(context.Background(), run.RunID, 1, claimed.LeaseOwner, "SYSTEM_ERROR", "stale"); !errors.Is(err, runs.ErrConditional) {
		t.Fatalf("stale fail should be fenced: %v", err)
	}
	if _, err := repository.UpdatePhase(context.Background(), run.RunID, 2, reclaimed.LeaseOwner, domain.StateClaimed, domain.StateRetrieving); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PinSourceIfAbsent(context.Background(), run.RunID, 2, reclaimed.LeaseOwner, "0123456789012345678901234567890123456789", "refs/heads/main", domain.RefBranch); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PinSourceIfAbsent(context.Background(), run.RunID, 2, reclaimed.LeaseOwner, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "refs/heads/main", domain.RefBranch); !errors.Is(err, runs.ErrConditional) {
		t.Fatalf("source must pin once: %v", err)
	}
	if _, err := repository.UpdatePhase(context.Background(), run.RunID, 2, reclaimed.LeaseOwner, domain.StateRetrieving, domain.StateValidating); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpdatePhase(context.Background(), run.RunID, 2, reclaimed.LeaseOwner, domain.StateValidating, domain.StatePersisting); err != nil {
		t.Fatal(err)
	}
	outcome, coverage, review, evaluation := domain.OutcomeNoFindings, domain.CoverageComplete, domain.ReviewCompleted, domain.EvaluationCompleted
	completed, err := repository.CompleteRun(context.Background(), run.RunID, 2, reclaimed.LeaseOwner, outcome, coverage, review, evaluation, "runs/id/manifest.json", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != domain.StateCompleted || completed.LeaseOwner != "" || completed.LeaseExpiresAt != nil || completed.WinningAttempt == nil || *completed.WinningAttempt != 2 {
		t.Fatalf("terminal lease cleanup failed: %+v", completed)
	}
}

func TestSQLiteRenewRejectsStaleExpiry(t *testing.T) {
	clock := runtime.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	repository, err := runs.OpenSQLite(filepath.Join(t.TempDir(), "runs.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	run, _ := repository.CreateRun(context.Background(), "https://example.invalid/a.git", "main", "")
	claimed, err := repository.ClaimRun(context.Background(), run.RunID, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	old := *claimed.LeaseExpiresAt
	next := old.Add(time.Minute)
	if _, err := repository.RenewLease(context.Background(), run.RunID, 1, "worker", old, next); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReclaimExpiredRun(context.Background(), run.RunID, 1, "worker", old, domain.StateClaimed, "other", clock.Now().Add(2*time.Minute), time.Minute); !errors.Is(err, runs.ErrConditional) {
		t.Fatalf("stale expected expiry must fail: %v", err)
	}
}

func TestCommandRunnerCancellationAndCap(t *testing.T) {
	runner := executionForTest()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	result := runner.Run(ctx, commandSpecForTest("cmd", "/c", "ping -n 20 127.0.0.1 >nul"))
	if !result.Cancelled {
		t.Fatalf("expected cancellation: %+v", result)
	}
	result = runner.Run(context.Background(), commandSpecForTest("cmd", "/c", "echo 123456789"))
	if result.StartError != "" || result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("command failed: %+v", result)
	}
}

func executionForTest() *execution.CommandRunner { return execution.NewCommandRunner() }
func commandSpecForTest(executable string, args ...string) execution.CommandSpec {
	return execution.CommandSpec{Executable: executable, Args: args, AllowedExecutables: map[string]bool{executable: true}, Timeout: 10 * time.Second, StdoutCap: 4, StderrCap: 4}
}

func TestGitFixture(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.email", "platformlens@example.invalid")
	git(t, repo, "config", "user.name", "PlatformLens Test")
	if err := os.WriteFile(filepath.Join(repo, "main.tf"), []byte("terraform {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	oid := stringOutput(t, repo, "rev-parse", "HEAD")
	git(t, repo, "tag", "-a", "annotated", "-m", "annotated")
	git(t, repo, "branch", "ambiguous")
	git(t, repo, "tag", "ambiguous")
	config := runtime.DefaultConfig()
	config.AllowLocalGit = true
	config.DataDir = filepath.Join(t.TempDir(), "data")
	config.SourceCacheDir = filepath.Join(config.DataDir, "cache")
	config.WorkspaceDir = filepath.Join(config.DataDir, "workspaces")
	config.Limits.CommandTimeoutSeconds = 30
	if err := config.Prepare(); err != nil {
		t.Fatal(err)
	}
	runtimeGit, err := source.NewRuntime(config, execution.NewCommandRunner())
	if err != nil {
		t.Fatal(err)
	}
	acquired, err := runtimeGit.Acquire(context.Background(), repo, "annotated")
	if err != nil {
		t.Fatal(err)
	}
	if acquired.CommitOID != oid {
		t.Fatalf("annotated tag did not peel: got=%s want=%s", acquired.CommitOID, oid)
	}
	if acquired.RefType != domain.RefTag {
		t.Fatalf("unexpected ref type: %s", acquired.RefType)
	}
	head, err := runtimeGit.Acquire(context.Background(), repo, "HEAD")
	if err != nil || head.CommitOID != oid || head.ResolvedRef != "refs/heads/main" {
		t.Fatalf("remote HEAD should resolve main: %+v err=%v", head, err)
	}
	if _, err := runtimeGit.Acquire(context.Background(), repo, "ambiguous"); !errors.Is(err, source.ErrAmbiguousRef) {
		t.Fatalf("ambiguous ref should fail: %v", err)
	}
	if _, err := runtimeGit.Acquire(context.Background(), repo, oid[:8]); !errors.Is(err, source.ErrShortOID) {
		t.Fatalf("short oid should fail: %v", err)
	}
	if env := runtimeGit.IsolatedEnvironmentForTest(); env["GIT_TERMINAL_PROMPT"] != "0" || env["GIT_CONFIG_NOSYSTEM"] != "1" {
		t.Fatalf("Git isolation missing: %#v", env)
	}
}

func git(t *testing.T, cwd string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = cwd
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
func stringOutput(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = cwd
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(output[:len(output)-1])
}
