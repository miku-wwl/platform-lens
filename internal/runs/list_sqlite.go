package runs

import (
	"context"
	"strings"
)

func (r *SQLiteRepository) ListRuns(ctx context.Context, options ListOptions) (ListResult, error) {
	options = NormalizeListOptions(options)
	query := `SELECT ` + columns + ` FROM runs`
	conditions := []string{}
	args := []any{}
	if options.State != "" {
		conditions = append(conditions, "state=?")
		args = append(args, options.State)
	}
	if options.Repository != "" {
		conditions = append(conditions, "repository_url=?")
		args = append(args, options.Repository)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY updated_at DESC, run_id DESC LIMIT ? OFFSET ?"
	args = append(args, options.Limit+1, options.Offset)
	items, err := r.findBy(ctx, query, args...)
	if err != nil {
		return ListResult{}, err
	}
	result := ListResult{Runs: items}
	if len(result.Runs) > options.Limit {
		result.HasMore = true
		result.Runs = result.Runs[:options.Limit]
	}
	return result, nil
}

var _ Lister = (*SQLiteRepository)(nil)
