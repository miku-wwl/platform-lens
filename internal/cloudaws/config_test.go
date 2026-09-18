package cloudaws

import (
	"context"
	"errors"
	"net/http"
	"testing"

	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

func TestLoadUsesSharedEndpointAndBoundedRetryPolicy(t *testing.T) {
	loaded, err := Load(context.Background(), Options{Region: "us-east-1", Endpoint: "http://localhost:4566", MaxAttempts: 4})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Endpoint != "http://localhost:4566" || loaded.AWS.BaseEndpoint == nil || *loaded.AWS.BaseEndpoint != loaded.Endpoint {
		t.Fatalf("endpoint policy was not shared: %+v", loaded)
	}
	if loaded.AWS.Retryer().MaxAttempts() != 4 {
		t.Fatalf("unexpected retry max attempts: %d", loaded.AWS.Retryer().MaxAttempts())
	}
}

func TestClassifyAWSFailures(t *testing.T) {
	response := func(status int) error {
		return &smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: status}}, Err: errors.New("response")}
	}
	cases := []struct {
		name string
		err  error
		want ErrorClass
	}{
		{name: "conditional", err: &smithy.GenericAPIError{Code: "ConditionalCheckFailedException"}, want: ErrorConditional},
		{name: "not found", err: response(http.StatusNotFound), want: ErrorNotFound},
		{name: "access denied", err: response(http.StatusForbidden), want: ErrorAccess},
		{name: "throttled", err: &smithy.GenericAPIError{Code: "ProvisionedThroughputExceededException"}, want: ErrorRetryable},
		{name: "service failure", err: response(http.StatusBadGateway), want: ErrorRetryable},
		{name: "unknown", err: errors.New("invalid request"), want: ErrorUnknown},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := Classify(test.err); got != test.want {
				t.Fatalf("Classify()=%s, want %s", got, test.want)
			}
		})
	}
}
