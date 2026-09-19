package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/miku-wwl/platform-lens/internal/cloudaws"
	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/runs"
)

func (s *Service) startHeartbeat(ctx context.Context, cancel context.CancelFunc, run domain.AnalysisRun) func() {
	heartbeatCtx, stop := context.WithCancel(ctx)
	var mu sync.Mutex
	expected := *run.LeaseExpiresAt
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Duration(s.Config.HeartbeatSeconds) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				current := expected
				mu.Unlock()
				updated, err := s.renewLeaseWithRetry(heartbeatCtx, run, current)
				if err != nil {
					cancel()
					return
				}
				if updated.LeaseExpiresAt != nil {
					mu.Lock()
					expected = *updated.LeaseExpiresAt
					mu.Unlock()
				}
			}
		}
	}()
	return func() { stop(); <-done }
}

// renewLeaseWithRetry keeps retryable cloud uncertainty separate from
// authoritative ownership/fencing failures. It deliberately returns after
// the same bounded safety window used by the Stage 1 acceptance contract.
func (s *Service) renewLeaseWithRetry(ctx context.Context, run domain.AnalysisRun, expected time.Time) (domain.AnalysisRun, error) {
	next := expected.Add(time.Duration(s.Config.HeartbeatSeconds*2) * time.Second)
	updated, err := s.Repository.RenewLease(ctx, run.RunID, run.AttemptNo, s.Config.WorkerID, expected, next)
	if err == nil {
		return updated, nil
	}
	if !retryableLeaseError(err) {
		return updated, err
	}

	maxAttempts := s.Config.AWSMaxAttempts
	if maxAttempts < 2 {
		maxAttempts = 2
	}
	for retryAttempt := 2; retryAttempt <= maxAttempts; retryAttempt++ {
		if !s.Clock.Now().Before(expected.Add(-time.Duration(maxInt(1, s.Config.HeartbeatSeconds)) * time.Second)) {
			break
		}
		delay := time.NewTimer(time.Duration(retryAttempt-1) * 50 * time.Millisecond)
		select {
		case <-ctx.Done():
			delay.Stop()
			return updated, ctx.Err()
		case <-delay.C:
		}
		updated, err = s.Repository.RenewLease(ctx, run.RunID, run.AttemptNo, s.Config.WorkerID, expected, next)
		if err == nil {
			return updated, nil
		}
		if !retryableLeaseError(err) {
			break
		}
	}
	return updated, err
}

func retryableLeaseError(err error) bool {
	return !errors.Is(err, runs.ErrConditional) && !errors.Is(err, runs.ErrFencingLost) && cloudaws.IsRetryable(err)
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
