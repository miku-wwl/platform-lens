package runs

import (
	"context"
	"errors"
	"time"

	"github.com/miku-wwl/platform-lens/internal/domain"
)

var (
	ErrNotFound    = errors.New("run not found")
	ErrConditional = errors.New("conditional write failed")
	ErrFencingLost = errors.New("run ownership was fenced")
)

type Repository interface {
	Ready(context.Context) error
	CreateRun(context.Context, string, string, string) (domain.AnalysisRun, error)
	FindQueuedCandidates(context.Context, int) ([]domain.AnalysisRun, error)
	ClaimRun(context.Context, string, string, time.Duration) (domain.AnalysisRun, error)
	PinSourceIfAbsent(context.Context, string, int, string, string, string, domain.RefType) (domain.AnalysisRun, error)
	RenewLease(context.Context, string, int, string, time.Time, time.Time) (domain.AnalysisRun, error)
	FindReclaimCandidates(context.Context, time.Time, int) ([]domain.AnalysisRun, error)
	ReclaimExpiredRun(context.Context, string, int, string, time.Time, domain.RunState, string, time.Time, time.Duration) (domain.AnalysisRun, error)
	UpdatePhase(context.Context, string, int, string, domain.RunState, domain.RunState) (domain.AnalysisRun, error)
	CompleteRun(context.Context, string, int, string, domain.AnalysisOutcome, domain.CoverageStatus, domain.ReviewStatus, domain.EvaluationStatus, string, string) (domain.AnalysisRun, error)
	FailRun(context.Context, string, int, string, string, string) (domain.AnalysisRun, error)
	GetRun(context.Context, string) (domain.AnalysisRun, error)
}
