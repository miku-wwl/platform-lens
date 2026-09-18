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
	run, err := r.GetRun(ctx, runID)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	now := r.Clock.Now()
	run.State, run.AttemptNo, run.LeaseOwner, run.LeaseAcquiredAt, run.LeaseExpiresAt, run.UpdatedAt = domain.StateClaimed, 1, worker, ptr(now), ptr(now.Add(lease)), now
	return r.putConditional(ctx, run, "#state = :queued AND attempt_no = :zero", map[string]string{"#state": "state"}, values(map[string]any{":queued": domain.StateQueued, ":zero": 0}))
}
func (r *DynamoRepository) PinSourceIfAbsent(ctx context.Context, runID string, attempt int, worker, oid, resolved string, refType domain.RefType) (domain.AnalysisRun, error) {
	run, err := r.GetRun(ctx, runID)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	if run.CommitOID != "" {
		return domain.AnalysisRun{}, ErrConditional
	}
	run.CommitOID, run.ResolvedRef, run.RefType, run.UpdatedAt = oid, resolved, refType, r.Clock.Now()
	return r.putConditional(ctx, run, "attribute_not_exists(commit_oid) AND attempt_no = :attempt AND lease_owner = :owner AND #state = :state", map[string]string{"#state": "state"}, values(map[string]any{":attempt": attempt, ":owner": worker, ":state": domain.StateRetrieving}))
}
func (r *DynamoRepository) RenewLease(ctx context.Context, runID string, attempt int, worker string, expected, next time.Time) (domain.AnalysisRun, error) {
	run, err := r.GetRun(ctx, runID)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	run.LeaseExpiresAt, run.UpdatedAt = ptr(next), r.Clock.Now()
	return r.putConditional(ctx, run, "attempt_no = :attempt AND lease_owner = :owner AND lease_expires_at = :expiry", nil, values(map[string]any{":attempt": attempt, ":owner": worker, ":expiry": encodeDynamoTime(expected)}))
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
	run, err := r.GetRun(ctx, runID)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	run.State, run.AttemptNo, run.LeaseOwner, run.LeaseAcquiredAt, run.LeaseExpiresAt, run.UpdatedAt = domain.StateClaimed, expectedAttempt+1, newWorker, ptr(observer), ptr(observer.Add(lease)), observer
	condition := "attempt_no = :attempt AND lease_owner = :owner AND lease_expires_at = :expiry AND #state = :state AND lease_expires_at <= :now"
	return r.putConditional(ctx, run, condition, map[string]string{"#state": "state"}, values(map[string]any{":attempt": expectedAttempt, ":owner": expectedOwner, ":expiry": encodeDynamoTime(expectedExpiry), ":state": expectedState, ":now": encodeDynamoTime(observer)}))
}
func (r *DynamoRepository) UpdatePhase(ctx context.Context, runID string, attempt int, worker string, expected, next domain.RunState) (domain.AnalysisRun, error) {
	run, err := r.GetRun(ctx, runID)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	run.State, run.UpdatedAt = next, r.Clock.Now()
	return r.putConditional(ctx, run, "attempt_no = :attempt AND lease_owner = :owner AND #state = :state", map[string]string{"#state": "state"}, values(map[string]any{":attempt": attempt, ":owner": worker, ":state": expected}))
}
func (r *DynamoRepository) CompleteRun(ctx context.Context, runID string, attempt int, worker string, outcome domain.AnalysisOutcome, coverage domain.CoverageStatus, review domain.ReviewStatus, evaluation domain.EvaluationStatus, uri, hash string) (domain.AnalysisRun, error) {
	run, err := r.GetRun(ctx, runID)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	run.State, run.AnalysisOutcome, run.CoverageStatus, run.ReviewStatus, run.EvaluationStatus = domain.StateCompleted, &outcome, &coverage, &review, &evaluation
	run.ManifestURI, run.ManifestHash, run.WinningAttempt, run.LeaseOwner, run.LeaseAcquiredAt, run.LeaseExpiresAt, run.UpdatedAt = uri, hash, ptr(attempt), "", nil, nil, r.Clock.Now()
	return r.putConditional(ctx, run, "attempt_no = :attempt AND lease_owner = :owner AND #state = :state", map[string]string{"#state": "state"}, values(map[string]any{":attempt": attempt, ":owner": worker, ":state": domain.StatePersisting}))
}
func (r *DynamoRepository) FailRun(ctx context.Context, runID string, attempt int, worker, code, message string) (domain.AnalysisRun, error) {
	run, err := r.GetRun(ctx, runID)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	run.State, run.FailureCode, run.FailureMessage, run.LeaseOwner, run.LeaseAcquiredAt, run.LeaseExpiresAt, run.UpdatedAt = domain.StateFailed, code, message, "", nil, nil, r.Clock.Now()
	condition := "attempt_no = :attempt AND lease_owner = :owner AND #state IN (:claimed, :retrieving, :validating, :reviewing, :evaluating, :persisting)"
	return r.putConditional(ctx, run, condition, map[string]string{"#state": "state"}, values(map[string]any{
		":attempt":    attempt,
		":owner":      worker,
		":claimed":    domain.StateClaimed,
		":retrieving": domain.StateRetrieving,
		":validating": domain.StateValidating,
		":reviewing":  domain.StateReviewing,
		":evaluating": domain.StateEvaluating,
		":persisting": domain.StatePersisting,
	}))
}

func (r *DynamoRepository) putConditional(ctx context.Context, run domain.AnalysisRun, condition string, names map[string]string, vals map[string]types.AttributeValue) (domain.AnalysisRun, error) {
	input := &dynamodb.PutItemInput{TableName: aws.String(r.Table), Item: item(run), ConditionExpression: aws.String(condition), ExpressionAttributeValues: vals}
	if len(names) > 0 {
		input.ExpressionAttributeNames = names
	}
	_, err := r.Client.PutItem(ctx, input)
	if err != nil {
		return domain.AnalysisRun{}, mapConditional(err)
	}
	return run, nil
}
func item(run domain.AnalysisRun) map[string]types.AttributeValue {
	payload, _ := json.Marshal(run)
	leaseExpiry := encodeTimeOrEmpty(run.LeaseExpiresAt)
	if leaseExpiry == "" {
		leaseExpiry = encodeDynamoTime(run.CreatedAt)
	}
	result := map[string]types.AttributeValue{"run_id": &types.AttributeValueMemberS{Value: run.RunID}, "state": &types.AttributeValueMemberS{Value: string(run.State)}, "attempt_no": &types.AttributeValueMemberN{Value: strconv.Itoa(run.AttemptNo)}, "lease_expires_at": &types.AttributeValueMemberS{Value: leaseExpiry}, "payload": &types.AttributeValueMemberS{Value: string(payload)}}
	if run.LeaseOwner != "" {
		result["lease_owner"] = &types.AttributeValueMemberS{Value: run.LeaseOwner}
	}
	if run.CommitOID != "" {
		result["commit_oid"] = &types.AttributeValueMemberS{Value: run.CommitOID}
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
	return run, nil
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
