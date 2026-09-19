package runs

import (
	"context"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/miku-wwl/platform-lens/internal/domain"
)

func (r *DynamoRepository) ListRuns(ctx context.Context, options ListOptions) (ListResult, error) {
	options = NormalizeListOptions(options)
	input := &dynamodb.ScanInput{
		TableName: aws.String(r.Table),
	}
	names := map[string]string{}
	valuesMap := map[string]types.AttributeValue{}
	filters := []string{}
	if options.State != "" {
		names["#state"] = "state"
		valuesMap[":state"] = &types.AttributeValueMemberS{Value: string(options.State)}
		filters = append(filters, "#state = :state")
	}
	if options.Repository != "" {
		names["#repository"] = "repository_url"
		valuesMap[":repository"] = &types.AttributeValueMemberS{Value: options.Repository}
		filters = append(filters, "#repository = :repository")
	}
	if len(filters) > 0 {
		input.FilterExpression = aws.String(strings.Join(filters, " AND "))
		input.ExpressionAttributeNames = names
		input.ExpressionAttributeValues = valuesMap
	}
	items := []domain.AnalysisRun{}
	for {
		response, err := r.Client.Scan(ctx, input)
		if err != nil {
			return ListResult{}, err
		}
		page, err := decodeItems(response.Items)
		if err != nil {
			return ListResult{}, err
		}
		items = append(items, page...)
		if len(response.LastEvaluatedKey) == 0 {
			break
		}
		input.ExclusiveStartKey = response.LastEvaluatedKey
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].RunID > items[j].RunID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	start := options.Offset
	if start > len(items) {
		start = len(items)
	}
	end := start + options.Limit
	if end > len(items) {
		end = len(items)
	}
	result := ListResult{Runs: items[start:end], HasMore: end < len(items)}
	return result, nil
}

var _ Lister = (*DynamoRepository)(nil)
