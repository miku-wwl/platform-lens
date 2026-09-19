package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/smithy-go"
	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/runs"
	"github.com/miku-wwl/platform-lens/internal/runtime"
)

type heartbeatRepository struct {
	calls       atomic.Int32
	alwaysRetry bool
	done        chan struct{}
}

func (r *heartbeatRepository) Ready(context.Context) error { return nil }
func (r *heartbeatRepository) CreateRun(context.Context, string, string, string) (domain.AnalysisRun, error) {
	return domain.AnalysisRun{}, nil
}
func (r *heartbeatRepository) FindQueuedCandidates(context.Context, int) ([]domain.AnalysisRun, error) {
	return nil, nil
}
func (r *heartbeatRepository) ClaimRun(context.Context, string, string, time.Duration) (domain.AnalysisRun, error) {
	return domain.AnalysisRun{}, nil
}
func (r *heartbeatRepository) PinSourceIfAbsent(context.Context, string, int, string, string, string, domain.RefType) (domain.AnalysisRun, error) {
	return domain.AnalysisRun{}, nil
}
func (r *heartbeatRepository) RenewLease(_ context.Context, _ string, _ int, _ string, _, next time.Time) (domain.AnalysisRun, error) {
	call := r.calls.Add(1)
	if call == 1 || r.alwaysRetry {
		if call == 1 && r.done != nil {
			close(r.done)
		}
		return domain.AnalysisRun{}, &smithy.GenericAPIError{Code: "ThrottlingException", Message: "simulated transient cloud fault"}
	}
	if r.done != nil {
		select {
		case <-r.done:
		default:
			close(r.done)
		}
	}
	return domain.AnalysisRun{LeaseExpiresAt: &next}, nil
}
func (r *heartbeatRepository) FindReclaimCandidates(context.Context, time.Time, int) ([]domain.AnalysisRun, error) {
	return nil, nil
}
func (r *heartbeatRepository) ReclaimExpiredRun(context.Context, string, int, string, time.Time, domain.RunState, string, time.Time, time.Duration) (domain.AnalysisRun, error) {
	return domain.AnalysisRun{}, nil
}
func (r *heartbeatRepository) UpdatePhase(context.Context, string, int, string, domain.RunState, domain.RunState) (domain.AnalysisRun, error) {
	return domain.AnalysisRun{}, nil
}
func (r *heartbeatRepository) CompleteRun(context.Context, string, int, string, domain.AnalysisOutcome, domain.CoverageStatus, domain.ReviewStatus, domain.EvaluationStatus, string, string) (domain.AnalysisRun, error) {
	return domain.AnalysisRun{}, nil
}
func (r *heartbeatRepository) FailRun(context.Context, string, int, string, string, string) (domain.AnalysisRun, error) {
	return domain.AnalysisRun{}, nil
}
func (r *heartbeatRepository) GetRun(context.Context, string) (domain.AnalysisRun, error) {
	return domain.AnalysisRun{}, nil
}

func TestHeartbeatRetriesTransientCloudError(t *testing.T) {
	repository := &heartbeatRepository{done: make(chan struct{})}
	config := runtime.DefaultConfig()
	config.WorkerID = "heartbeat-test"
	config.HeartbeatSeconds = 1
	config.AWSMaxAttempts = 3
	service := &Service{Config: config, Clock: runtime.RealClock{}, Repository: repository}
	expires := time.Now().UTC().Add(10 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	stop := service.startHeartbeat(ctx, cancel, domain.AnalysisRun{RunID: "run", AttemptNo: 1, LeaseExpiresAt: &expires})
	defer stop()
	select {
	case <-repository.done:
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat did not call repository")
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for repository.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if repository.calls.Load() < 2 {
		t.Fatalf("transient error was not retried; calls=%d", repository.calls.Load())
	}
	if ctx.Err() != nil {
		t.Fatalf("transient error cancelled attempt context: %v", ctx.Err())
	}
}

func TestHeartbeatStopsAfterBoundedTransientRetries(t *testing.T) {
	repository := &heartbeatRepository{alwaysRetry: true}
	config := runtime.DefaultConfig()
	config.WorkerID = "heartbeat-test"
	config.HeartbeatSeconds = 1
	config.AWSMaxAttempts = 3
	service := &Service{Config: config, Clock: runtime.RealClock{}, Repository: repository}
	expires := time.Now().UTC().Add(10 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	stop := service.startHeartbeat(ctx, cancel, domain.AnalysisRun{RunID: "run", AttemptNo: 1, LeaseExpiresAt: &expires})
	defer stop()
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat did not stop after bounded retries")
	}
	if calls := repository.calls.Load(); calls != 3 {
		t.Fatalf("heartbeat calls=%d, want exactly configured max attempts 3", calls)
	}
}

func TestRetryableLeaseErrorKeepsFencingFailuresNonRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "conditional", err: runs.ErrConditional, want: false},
		{name: "fencing", err: runs.ErrFencingLost, want: false},
		{name: "transient cloud", err: &smithy.GenericAPIError{Code: "ThrottlingException"}, want: true},
		{name: "unknown", err: errors.New("not retryable"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryableLeaseError(tt.err); got != tt.want {
				t.Fatalf("retryableLeaseError()=%v, want %v", got, tt.want)
			}
		})
	}
}
