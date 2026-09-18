package tests

import (
	"context"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/miku-wwl/platform-lens/internal/cloudaws"
	"github.com/miku-wwl/platform-lens/internal/runtime"
)

func TestLocalStackIAMWorkerPolicyAndNegativeChecks(t *testing.T) {
	endpoint := os.Getenv("PLATFORMLENS_LOCALSTACK_ENDPOINT")
	if endpoint == "" {
		t.Skip("BLOCKED: set PLATFORMLENS_LOCALSTACK_ENDPOINT for IAM acceptance")
	}
	if os.Getenv("PLATFORMLENS_LOCALSTACK_IAM_ENFORCE") != "1" {
		t.Skip("BLOCKED: LocalStack IAM enforcement is not enabled; set PLATFORMLENS_LOCALSTACK_IAM_ENFORCE=1")
	}
	accessKey := os.Getenv("PLATFORMLENS_LOCALSTACK_WORKER_ACCESS_KEY_ID")
	secretKey := os.Getenv("PLATFORMLENS_LOCALSTACK_WORKER_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		t.Skip("BLOCKED: Terraform worker access-key environment is missing")
	}
	t.Setenv("AWS_ACCESS_KEY_ID", accessKey)
	t.Setenv("AWS_SECRET_ACCESS_KEY", secretKey)
	t.Setenv("AWS_REGION", "us-east-1")
	config, err := cloudaws.Load(context.Background(), cloudaws.Options{Region: "us-east-1", Endpoint: endpoint, MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	dynamoClient := dynamodb.NewFromConfig(config.AWS)
	if _, err := dynamoClient.DescribeTable(context.Background(), &dynamodb.DescribeTableInput{TableName: aws.String("platformlens-runs")}); err != nil {
		t.Fatalf("worker cannot perform allowed DescribeTable: %v", err)
	}
	s3Client := s3.NewFromConfig(config.AWS, func(options *s3.Options) { options.UsePathStyle = true })
	if _, err := s3Client.HeadBucket(context.Background(), &s3.HeadBucketInput{Bucket: aws.String("platformlens-artifacts")}); err != nil {
		t.Fatalf("worker cannot perform allowed HeadBucket/ListBucket: %v", err)
	}

	deniedTable := "platformlens-iam-denied-" + runtime.NewID()[:8]
	created, err := dynamoClient.CreateTable(context.Background(), &dynamodb.CreateTableInput{
		TableName:            aws.String(deniedTable),
		BillingMode:          types.BillingModePayPerRequest,
		AttributeDefinitions: []types.AttributeDefinition{{AttributeName: aws.String("run_id"), AttributeType: types.ScalarAttributeTypeS}},
		KeySchema:            []types.KeySchemaElement{{AttributeName: aws.String("run_id"), KeyType: types.KeyTypeHash}},
	})
	if err == nil {
		_, _ = dynamoClient.DeleteTable(context.Background(), &dynamodb.DeleteTableInput{TableName: aws.String(deniedTable)})
		t.Fatalf("worker unexpectedly created forbidden table: %v", created.TableDescription)
	}
	if !cloudaws.IsAccessDenied(err) {
		t.Fatalf("forbidden CreateTable returned non-access error: %v", err)
	}

	deniedBucket := "platformlens-iam-denied-" + runtime.NewID()[:8]
	if _, err := s3Client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String(deniedBucket)}); err == nil {
		_, _ = s3Client.DeleteBucket(context.Background(), &s3.DeleteBucketInput{Bucket: aws.String(deniedBucket)})
		t.Fatalf("worker unexpectedly created forbidden bucket %q", deniedBucket)
	} else if !cloudaws.IsAccessDenied(err) {
		t.Fatalf("forbidden CreateBucket returned non-access error: %v", err)
	}
}
