package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/runtime"
)

type DynamoRepository struct {
	Client *dynamodb.Client
	Table  string
	GSI    string
	Clock  runtime.Clock
}

func NewDynamoRepository(ctx context.Context, endpoint, region, table, gsi string, clock runtime.Clock) (*DynamoRepository, error) {
	config, err := awsConfig(ctx, endpoint, region)
	if err != nil {
		return nil, err
	}
	client := dynamodb.NewFromConfig(config)
	repo := &DynamoRepository{Client: client, Table: table, GSI: gsi, Clock: clock}
	if repo.Clock == nil {
		repo.Clock = runtime.RealClock{}
	}
	if err := repo.EnsureTable(ctx); err != nil {
		return nil, err
	}
	return repo, nil
}

func awsConfig(ctx context.Context, endpoint, region string) (aws.Config, error) {
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if endpoint != "" {
		options = append(options, awsconfig.WithBaseEndpoint(endpoint), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")))
	}
	return awsconfig.LoadDefaultConfig(ctx, options...)
}

func (r *DynamoRepository) EnsureTable(ctx context.Context) error {
	_, err := r.Client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(r.Table)})
	if err == nil {
		return nil
	}
	_, err = r.Client.CreateTable(ctx, &dynamodb.CreateTableInput{TableName: aws.String(r.Table), BillingMode: types.BillingModePayPerRequest, KeySchema: []types.KeySchemaElement{{AttributeName: aws.String("run_id"), KeyType: types.KeyTypeHash}}, AttributeDefinitions: []types.AttributeDefinition{{AttributeName: aws.String("run_id"), AttributeType: types.ScalarAttributeTypeS}, {AttributeName: aws.String("state"), AttributeType: types.ScalarAttributeTypeS}, {AttributeName: aws.String("lease_expires_at"), AttributeType: types.ScalarAttributeTypeS}}, GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{{IndexName: aws.String(r.GSI), KeySchema: []types.KeySchemaElement{{AttributeName: aws.String("state"), KeyType: types.KeyTypeHash}, {AttributeName: aws.String("lease_expires_at"), KeyType: types.KeyTypeRange}}, Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll}}}})
	if err != nil {
		return err
	}
	waiter := dynamodb.NewTableExistsWaiter(r.Client)
	return waiter.Wait(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(r.Table)}, 2*time.Minute)
}

func (r *DynamoRepository) CreateRun(ctx context.Context, repositoryURL, requestedRef, requestedPath string) (domain.AnalysisRun, error) {
	now := r.Clock.Now()
	run := domain.AnalysisRun{RunID: runtime.NewID(), RepositoryURL: repositoryURL, RequestedRef: requestedRef, RequestedPath: requestedPath, State: domain.StateQueued, CreatedAt: now, UpdatedAt: now}
	_, err := r.Client.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(r.Table), Item: item(run), ConditionExpression: aws.String("attribute_not_exists(run_id)"), ReturnConsumedCapacity: types.ReturnConsumedCapacityNone})
	return run, mapConditional(err)
}
func (r *DynamoRepository) GetRun(ctx context.Context, runID string) (domain.AnalysisRun, error) {
	result, err := r.Client.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(r.Table), Key: map[string]types.AttributeValue{"run_id": &types.AttributeValueMemberS{Value: runID}}, ConsistentRead: aws.Bool(true)})
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	if len(result.Item) == 0 {
		return domain.AnalysisRun{}, ErrNotFound
	}
	return decode(result.Item)
}
func (r *DynamoRepository) FindQueuedCandidates(ctx context.Context, limit int) ([]domain.AnalysisRun, error) {
	result, err := r.Client.Query(ctx, &dynamodb.QueryInput{TableName: aws.String(r.Table), IndexName: aws.String(r.GSI), KeyConditionExpression: aws.String("#state = :queued"), ExpressionAttributeNames: map[string]string{"#state": "state"}, ExpressionAttributeValues: map[string]types.AttributeValue{":queued": &types.AttributeValueMemberS{Value: string(domain.StateQueued)}}, Limit: aws.Int32(int32(limit))})
	if err != nil {
		return nil, err
	}
	return decodeItems(result.Items)
}
func (r *DynamoRepository) ClaimRun(ctx context.Context, runID, worker string, lease time.Duration) (domain.AnalysisRun, error) {
	now := r.Clock.Now()
	return r.updateConditional(ctx, runID,
		"SET #state=:claimed, attempt_no=:one, lease_owner=:owner, lease_acquired_at=:acquired, lease_expires_at=:expiry, updated_at=:updated",
		"#state = :queued AND attempt_no = :zero", map[string]string{"#state": "state"}, values(map[string]any{
			":claimed":  domain.StateClaimed,
			":queued":   domain.StateQueued,
			":zero":     0,
			":one":      1,
			":owner":    worker,
			":acquired": now,
			":expiry":   now.Add(lease),
			":updated":  now,
		}))
}
func (r *DynamoRepository) PinSourceIfAbsent(ctx context.Context, runID string, attempt int, worker, oid, resolved string, refType domain.RefType) (domain.AnalysisRun, error) {
	now := r.Clock.Now()
	return r.updateConditional(ctx, runID,
		"SET commit_oid=:oid, resolved_ref=:resolved, ref_type=:ref_type, updated_at=:updated",
		"attribute_not_exists(commit_oid) AND attempt_no = :attempt AND lease_owner = :owner AND #state = :state",
		map[string]string{"#state": "state"}, values(map[string]any{
			":oid":      oid,
			":resolved": resolved,
			":ref_type": refType,
			":updated":  now,
			":attempt":  attempt,
			":owner":    worker,
			":state":    domain.StateRetrieving,
		}))
}
func (r *DynamoRepository) RenewLease(ctx context.Context, runID string, attempt int, worker string, expected, next time.Time) (domain.AnalysisRun, error) {
	now := r.Clock.Now()
	return r.updateConditional(ctx, runID,
		"SET lease_expires_at=:next, updated_at=:updated",
		"attempt_no = :attempt AND lease_owner = :owner AND lease_expires_at = :expiry AND #state IN (:claimed, :retrieving, :validating, :reviewing, :evaluating, :persisting)",
		map[string]string{"#state": "state"}, values(map[string]any{
			":attempt":    attempt,
			":owner":      worker,
			":expiry":     expected,
			":next":       next,
			":updated":    now,
			":claimed":    domain.StateClaimed,
			":retrieving": domain.StateRetrieving,
			":validating": domain.StateValidating,
			":reviewing":  domain.StateReviewing,
			":evaluating": domain.StateEvaluating,
			":persisting": domain.StatePersisting,
		}))
}
func (r *DynamoRepository) FindReclaimCandidates(ctx context.Context, observer time.Time, limit int) ([]domain.AnalysisRun, error) {
	result := []domain.AnalysisRun{}
	for _, state := range []domain.RunState{domain.StateClaimed, domain.StateRetrieving, domain.StateValidating, domain.StateReviewing, domain.StateEvaluating, domain.StatePersisting} {
		response, err := r.Client.Query(ctx, &dynamodb.QueryInput{TableName: aws.String(r.Table), IndexName: aws.String(r.GSI), KeyConditionExpression: aws.String("#state = :state AND lease_expires_at <= :expiry"), ExpressionAttributeNames: map[string]string{"#state": "state"}, ExpressionAttributeValues: values(map[string]any{":state": state, ":expiry": encodeDynamoTime(observer)}), Limit: aws.Int32(int32(limit))})
		if err != nil {
			return nil, err
		}
		items, err := decodeItems(response.Items)
		if err != nil {
			return nil, err
		}
		result = append(result, items...)
		if len(result) >= limit {
			break
		}
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
func (r *DynamoRepository) ReclaimExpiredRun(ctx context.Context, runID string, expectedAttempt int, expectedOwner string, expectedExpiry time.Time, expectedState domain.RunState, newWorker string, observer time.Time, lease time.Duration) (domain.AnalysisRun, error) {
	condition := "attempt_no = :attempt AND lease_owner = :owner AND lease_expires_at = :expiry AND #state = :state AND lease_expires_at <= :now"
	return r.updateConditional(ctx, runID,
		"SET #state=:claimed, attempt_no=:next_attempt, lease_owner=:new_owner, lease_acquired_at=:acquired, lease_expires_at=:next_expiry, updated_at=:updated",
		condition, map[string]string{"#state": "state"}, values(map[string]any{
			":attempt":      expectedAttempt,
			":owner":        expectedOwner,
			":expiry":       expectedExpiry,
			":state":        expectedState,
			":now":          observer,
			":claimed":      domain.StateClaimed,
			":next_attempt": expectedAttempt + 1,
			":new_owner":    newWorker,
			":acquired":     observer,
			":next_expiry":  observer.Add(lease),
			":updated":      observer,
		}))
}
func (r *DynamoRepository) UpdatePhase(ctx context.Context, runID string, attempt int, worker string, expected, next domain.RunState) (domain.AnalysisRun, error) {
	now := r.Clock.Now()
	return r.updateConditional(ctx, runID, "SET #state=:next, updated_at=:updated", "attempt_no = :attempt AND lease_owner = :owner AND #state = :state", map[string]string{"#state": "state"}, values(map[string]any{":attempt": attempt, ":owner": worker, ":state": expected, ":next": next, ":updated": now}))
}
func (r *DynamoRepository) CompleteRun(ctx context.Context, runID string, attempt int, worker string, outcome domain.AnalysisOutcome, coverage domain.CoverageStatus, review domain.ReviewStatus, evaluation domain.EvaluationStatus, uri, hash string) (domain.AnalysisRun, error) {
	now := r.Clock.Now()
	return r.updateConditional(ctx, runID,
		"SET #state=:completed, analysis_outcome=:outcome, coverage_status=:coverage, review_status=:review, evaluation_status=:evaluation, winning_attempt=:attempt, manifest_uri=:uri, manifest_hash=:hash, updated_at=:updated REMOVE lease_owner, lease_acquired_at, lease_expires_at",
		"attempt_no = :attempt AND lease_owner = :owner AND #state = :state", map[string]string{"#state": "state"}, values(map[string]any{
			":attempt":    attempt,
			":owner":      worker,
			":state":      domain.StatePersisting,
			":completed":  domain.StateCompleted,
			":outcome":    outcome,
			":coverage":   coverage,
			":review":     review,
			":evaluation": evaluation,
			":uri":        uri,
			":hash":       hash,
			":updated":    now,
		}))
}
func (r *DynamoRepository) FailRun(ctx context.Context, runID string, attempt int, worker, code, message string) (domain.AnalysisRun, error) {
	condition := "attempt_no = :attempt AND lease_owner = :owner AND #state IN (:claimed, :retrieving, :validating, :reviewing, :evaluating, :persisting)"
	return r.updateConditional(ctx, runID,
		"SET #state=:failed, failure_code=:code, failure_message=:message, updated_at=:updated REMOVE lease_owner, lease_acquired_at, lease_expires_at",
		condition, map[string]string{"#state": "state"}, values(map[string]any{
			":attempt":    attempt,
			":owner":      worker,
			":claimed":    domain.StateClaimed,
			":retrieving": domain.StateRetrieving,
			":validating": domain.StateValidating,
			":reviewing":  domain.StateReviewing,
			":evaluating": domain.StateEvaluating,
			":persisting": domain.StatePersisting,
			":failed":     domain.StateFailed,
			":code":       code,
			":message":    truncate(message, 4096),
			":updated":    r.Clock.Now(),
		}))
}

func (r *DynamoRepository) updateConditional(ctx context.Context, runID, update, condition string, names map[string]string, vals map[string]types.AttributeValue) (domain.AnalysisRun, error) {
	input := &dynamodb.UpdateItemInput{TableName: aws.String(r.Table), Key: map[string]types.AttributeValue{"run_id": &types.AttributeValueMemberS{Value: runID}}, UpdateExpression: aws.String(update), ConditionExpression: aws.String(condition), ExpressionAttributeValues: vals, ReturnValues: types.ReturnValueAllNew}
	if len(names) > 0 {
		input.ExpressionAttributeNames = names
	}
	result, err := r.Client.UpdateItem(ctx, input)
	if err != nil {
		return domain.AnalysisRun{}, mapConditional(err)
	}
	return decode(result.Attributes)
}
func item(run domain.AnalysisRun) map[string]types.AttributeValue {
	payload, _ := json.Marshal(run)
	result := map[string]types.AttributeValue{
		"run_id":     &types.AttributeValueMemberS{Value: run.RunID},
		"state":      &types.AttributeValueMemberS{Value: string(run.State)},
		"attempt_no": &types.AttributeValueMemberN{Value: strconv.Itoa(run.AttemptNo)},
		"payload":    &types.AttributeValueMemberS{Value: string(payload)},
	}
	if run.LeaseExpiresAt != nil {
		result["lease_expires_at"] = &types.AttributeValueMemberS{Value: encodeDynamoTime(*run.LeaseExpiresAt)}
	} else if run.State == domain.StateQueued {
		result["lease_expires_at"] = &types.AttributeValueMemberS{Value: encodeDynamoTime(run.CreatedAt)}
	}
	putDynamoString(result, "repository_url", run.RepositoryURL)
	putDynamoString(result, "requested_ref", run.RequestedRef)
	putDynamoString(result, "resolved_ref", run.ResolvedRef)
	putDynamoString(result, "ref_type", string(run.RefType))
	putDynamoString(result, "commit_oid", run.CommitOID)
	putDynamoString(result, "requested_path", run.RequestedPath)
	putDynamoString(result, "lease_owner", run.LeaseOwner)
	putDynamoString(result, "lease_acquired_at", encodeTimeOrEmpty(run.LeaseAcquiredAt))
	putDynamoString(result, "analysis_outcome", stringValue(run.AnalysisOutcome))
	putDynamoString(result, "coverage_status", stringValue(run.CoverageStatus))
	putDynamoString(result, "review_status", stringValue(run.ReviewStatus))
	putDynamoString(result, "evaluation_status", stringValue(run.EvaluationStatus))
	putDynamoString(result, "manifest_uri", run.ManifestURI)
	putDynamoString(result, "manifest_hash", run.ManifestHash)
	putDynamoString(result, "failure_code", run.FailureCode)
	putDynamoString(result, "failure_message", run.FailureMessage)
	putDynamoString(result, "created_at", encodeDynamoTime(run.CreatedAt))
	putDynamoString(result, "updated_at", encodeDynamoTime(run.UpdatedAt))
	if run.WinningAttempt != nil {
		result["winning_attempt"] = &types.AttributeValueMemberN{Value: strconv.Itoa(*run.WinningAttempt)}
	}
	return result
}
func decode(item map[string]types.AttributeValue) (domain.AnalysisRun, error) {
	value, ok := item["payload"].(*types.AttributeValueMemberS)
	if !ok {
		return domain.AnalysisRun{}, errors.New("invalid run payload")
	}
	var run domain.AnalysisRun
	if err := json.Unmarshal([]byte(value.Value), &run); err != nil {
		return domain.AnalysisRun{}, err
	}
	applyDynamoFields(&run, item)
	return run, nil
}

func putDynamoString(item map[string]types.AttributeValue, key, value string) {
	if value != "" {
		item[key] = &types.AttributeValueMemberS{Value: value}
	}
}
func stringValue[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}
func applyDynamoFields(run *domain.AnalysisRun, item map[string]types.AttributeValue) {
	if value, ok := item["state"].(*types.AttributeValueMemberS); ok {
		run.State = domain.RunState(value.Value)
	}
	if value, ok := item["attempt_no"].(*types.AttributeValueMemberN); ok {
		run.AttemptNo, _ = strconv.Atoi(value.Value)
	}
	if value, ok := item["repository_url"].(*types.AttributeValueMemberS); ok {
		run.RepositoryURL = value.Value
	}
	if value, ok := item["requested_ref"].(*types.AttributeValueMemberS); ok {
		run.RequestedRef = value.Value
	}
	if value, ok := item["requested_path"].(*types.AttributeValueMemberS); ok {
		run.RequestedPath = value.Value
	}
	if value, ok := item["resolved_ref"].(*types.AttributeValueMemberS); ok {
		run.ResolvedRef = value.Value
	}
	if value, ok := item["ref_type"].(*types.AttributeValueMemberS); ok {
		run.RefType = domain.RefType(value.Value)
	}
	if value, ok := item["commit_oid"].(*types.AttributeValueMemberS); ok {
		run.CommitOID = value.Value
	}
	if value, ok := item["updated_at"].(*types.AttributeValueMemberS); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, value.Value); err == nil {
			run.UpdatedAt = parsed
		}
	}
	if value, ok := item["created_at"].(*types.AttributeValueMemberS); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, value.Value); err == nil {
			run.CreatedAt = parsed
		}
	}
	if value, ok := item["manifest_uri"].(*types.AttributeValueMemberS); ok {
		run.ManifestURI = value.Value
	}
	if value, ok := item["manifest_hash"].(*types.AttributeValueMemberS); ok {
		run.ManifestHash = value.Value
	}
	if value, ok := item["failure_code"].(*types.AttributeValueMemberS); ok {
		run.FailureCode = value.Value
	}
	if value, ok := item["failure_message"].(*types.AttributeValueMemberS); ok {
		run.FailureMessage = value.Value
	}
	if value, ok := item["winning_attempt"].(*types.AttributeValueMemberN); ok {
		if parsed, err := strconv.Atoi(value.Value); err == nil {
			run.WinningAttempt = &parsed
		}
	}
	if run.State.Active() {
		if value, ok := item["lease_owner"].(*types.AttributeValueMemberS); ok {
			run.LeaseOwner = value.Value
		}
		if value, ok := item["lease_acquired_at"].(*types.AttributeValueMemberS); ok {
			run.LeaseAcquiredAt = parseDynamoTime(value.Value)
		}
		if value, ok := item["lease_expires_at"].(*types.AttributeValueMemberS); ok {
			run.LeaseExpiresAt = parseDynamoTime(value.Value)
		}
	} else {
		run.LeaseOwner, run.LeaseAcquiredAt, run.LeaseExpiresAt = "", nil, nil
	}
	if value, ok := item["analysis_outcome"].(*types.AttributeValueMemberS); ok {
		parsed := domain.AnalysisOutcome(value.Value)
		run.AnalysisOutcome = &parsed
	}
	if value, ok := item["coverage_status"].(*types.AttributeValueMemberS); ok {
		parsed := domain.CoverageStatus(value.Value)
		run.CoverageStatus = &parsed
	}
	if value, ok := item["review_status"].(*types.AttributeValueMemberS); ok {
		parsed := domain.ReviewStatus(value.Value)
		run.ReviewStatus = &parsed
	}
	if value, ok := item["evaluation_status"].(*types.AttributeValueMemberS); ok {
		parsed := domain.EvaluationStatus(value.Value)
		run.EvaluationStatus = &parsed
	}
}
func parseDynamoTime(value string) *time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return &parsed
}
func decodeItems(items []map[string]types.AttributeValue) ([]domain.AnalysisRun, error) {
	result := []domain.AnalysisRun{}
	for _, item := range items {
		run, err := decode(item)
		if err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, nil
}
func values(input map[string]any) map[string]types.AttributeValue {
	result := map[string]types.AttributeValue{}
	for key, value := range input {
		switch value := value.(type) {
		case string:
			result[key] = &types.AttributeValueMemberS{Value: value}
		case int:
			result[key] = &types.AttributeValueMemberN{Value: strconv.Itoa(value)}
		case domain.RunState:
			result[key] = &types.AttributeValueMemberS{Value: string(value)}
		case domain.RefType:
			result[key] = &types.AttributeValueMemberS{Value: string(value)}
		case domain.AnalysisOutcome:
			result[key] = &types.AttributeValueMemberS{Value: string(value)}
		case domain.CoverageStatus:
			result[key] = &types.AttributeValueMemberS{Value: string(value)}
		case domain.ReviewStatus:
			result[key] = &types.AttributeValueMemberS{Value: string(value)}
		case domain.EvaluationStatus:
			result[key] = &types.AttributeValueMemberS{Value: string(value)}
		case time.Time:
			result[key] = &types.AttributeValueMemberS{Value: encodeDynamoTime(value)}
		}
	}
	return result
}
func ptr[T any](value T) *T                   { return &value }
func encodeDynamoTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
func encodeTimeOrEmpty(value *time.Time) string {
	if value == nil {
		return ""
	}
	return encodeDynamoTime(*value)
}
func mapConditional(err error) error {
	if err == nil {
		return nil
	}
	var conditional *types.ConditionalCheckFailedException
	if errors.As(err, &conditional) {
		return ErrConditional
	}
	return fmt.Errorf("dynamodb: %w", err)
}
