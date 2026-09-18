package tests

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/runs"
	"github.com/miku-wwl/platform-lens/internal/runtime"
)

func TestLocalStackDynamoAtomicLifecycleCAS(t *testing.T) {
	endpoint := os.Getenv("PLATFORMLENS_LOCALSTACK_ENDPOINT")
	if endpoint == "" {
		t.Skip("set PLATFORMLENS_LOCALSTACK_ENDPOINT to run DynamoDB concurrency acceptance")
	}
	ctx := context.Background()
	clock := runtime.NewFakeClock(time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC))
	table := "platformlens-runs-hardening-" + runtime.NewID()[:8]
	repoA, err := runs.NewDynamoRepository(ctx, endpoint, "us-east-1", table, "candidate-index", clock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = repoA.Client.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: aws.String(table)}) }()
	repoB, err := runs.NewDynamoRepository(ctx, endpoint, "us-east-1", table, "candidate-index", clock)
	if err != nil {
		t.Fatal(err)
	}

	run, err := repoA.CreateRun(ctx, "https://example.invalid/platform.git", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repoA.ClaimRun(ctx, run.RunID, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	oldExpiry := *claimed.LeaseExpiresAt
	nextExpiry := oldExpiry.Add(time.Minute)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, _ = repoA.RenewLease(ctx, run.RunID, claimed.AttemptNo, "worker-a", oldExpiry, nextExpiry)
	}()
	go func() {
		defer wait.Done()
		_, _ = repoB.UpdatePhase(ctx, run.RunID, claimed.AttemptNo, "worker-a", domain.StateClaimed, domain.StateRetrieving)
	}()
	wait.Wait()
	final, err := repoA.GetRun(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if final.State != domain.StateRetrieving || final.LeaseExpiresAt == nil || !final.LeaseExpiresAt.Equal(nextExpiry) {
		t.Fatalf("atomic updates regressed state or lease: %+v", final)
	}

	reclaimRun, err := repoA.CreateRun(ctx, "https://example.invalid/reclaim.git", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	reclaimClaim, err := repoA.ClaimRun(ctx, reclaimRun.RunID, "worker-old", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	oldExpiry = *reclaimClaim.LeaseExpiresAt
	var renewErr, reclaimErr error
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, renewErr = repoA.RenewLease(ctx, reclaimRun.RunID, 1, "worker-old", oldExpiry, oldExpiry.Add(time.Minute))
	}()
	go func() {
		defer wait.Done()
		_, reclaimErr = repoB.ReclaimExpiredRun(ctx, reclaimRun.RunID, 1, "worker-old", oldExpiry, domain.StateClaimed, "worker-new", oldExpiry, time.Minute)
	}()
	wait.Wait()
	final, err = repoA.GetRun(ctx, reclaimRun.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if final.AttemptNo == 1 && !errors.Is(reclaimErr, runs.ErrConditional) {
		t.Fatalf("renew won without fencing reclaim: renew=%v reclaim=%v final=%+v", renewErr, reclaimErr, final)
	}
	if final.AttemptNo == 2 && (final.LeaseOwner != "worker-new" || !errors.Is(renewErr, runs.ErrConditional)) {
		t.Fatalf("reclaim won without fencing heartbeat: renew=%v reclaim=%v final=%+v", renewErr, reclaimErr, final)
	}

	staleRun, err := repoA.CreateRun(ctx, "https://example.invalid/stale.git", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	staleClaim, err := repoA.ClaimRun(ctx, staleRun.RunID, "worker-old", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repoA.ReclaimExpiredRun(ctx, staleRun.RunID, 1, "worker-old", *staleClaim.LeaseExpiresAt, domain.StateClaimed, "worker-new", oldExpiry.Add(2*time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := repoA.CompleteRun(ctx, staleRun.RunID, 1, "worker-old", domain.OutcomeNoFindings, domain.CoverageComplete, domain.ReviewCompleted, domain.EvaluationCompleted, "old", "old"); !errors.Is(err, runs.ErrConditional) {
		t.Fatalf("old completion was not fenced: %v", err)
	}
	if _, err := repoA.FailRun(ctx, staleRun.RunID, 1, "worker-old", "STALE", "old"); !errors.Is(err, runs.ErrConditional) {
		t.Fatalf("old failure was not fenced: %v", err)
	}
	final, err = repoA.GetRun(ctx, staleRun.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if final.AttemptNo != 2 || final.LeaseOwner != "worker-new" || final.State != domain.StateClaimed {
		t.Fatalf("stale attempt mutated reclaimed run: %+v", final)
	}
}
