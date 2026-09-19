package runs

import (
	"context"

	"github.com/miku-wwl/platform-lens/internal/domain"
)

const (
	DefaultListLimit = 25
	MaxListLimit     = 100
	MaxListOffset    = 10000
)

// ListOptions describes a bounded, read-only presentation query. It is kept
// separate from the lifecycle Repository contract so listing cannot alter
// claim, lease, fencing, or terminal-write semantics.
type ListOptions struct {
	Limit      int
	Offset     int
	State      domain.RunState
	Repository string
}

type ListResult struct {
	Runs    []domain.AnalysisRun
	HasMore bool
}

// Lister is an optional presentation capability implemented by the concrete
// repositories. The lifecycle Repository interface remains unchanged.
type Lister interface {
	ListRuns(context.Context, ListOptions) (ListResult, error)
}

func NormalizeListOptions(options ListOptions) ListOptions {
	if options.Limit <= 0 {
		options.Limit = DefaultListLimit
	}
	if options.Limit > MaxListLimit {
		options.Limit = MaxListLimit
	}
	if options.Offset < 0 {
		options.Offset = 0
	}
	if options.Offset > MaxListOffset {
		options.Offset = MaxListOffset
	}
	return options
}
