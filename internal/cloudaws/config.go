package cloudaws

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const DefaultMaxAttempts = 3

type Options struct {
	Region      string
	Endpoint    string
	MaxAttempts int
}

type Config struct {
	AWS         aws.Config
	Endpoint    string
	MaxAttempts int
}

func Load(ctx context.Context, options Options) (Config, error) {
	if strings.TrimSpace(options.Region) == "" {
		return Config{}, errors.New("AWS region is required")
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = DefaultMaxAttempts
	}
	loaded, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(options.Region),
		awsconfig.WithRetryer(func() aws.Retryer {
			return retry.NewStandard(func(settings *retry.StandardOptions) {
				settings.MaxAttempts = options.MaxAttempts
			})
		}),
	)
	if err != nil {
		return Config{}, err
	}
	if options.Endpoint != "" {
		loaded.BaseEndpoint = aws.String(options.Endpoint)
	}
	return Config{AWS: loaded, Endpoint: options.Endpoint, MaxAttempts: options.MaxAttempts}, nil
}

type ErrorClass string

const (
	ErrorUnknown     ErrorClass = "UNKNOWN"
	ErrorConditional ErrorClass = "CONDITIONAL"
	ErrorNotFound    ErrorClass = "NOT_FOUND"
	ErrorAccess      ErrorClass = "ACCESS_DENIED"
	ErrorRetryable   ErrorClass = "RETRYABLE"
)

func Classify(err error) ErrorClass {
	if err == nil {
		return ""
	}
	var apiErr smithy.APIError
	_ = errors.As(err, &apiErr)
	code := ""
	if apiErr != nil {
		code = strings.ToLower(apiErr.ErrorCode())
	}
	if strings.Contains(code, "conditionalcheckfailed") || code == "conditionalcheckfailed" {
		return ErrorConditional
	}
	status := 0
	var responseErr *smithyhttp.ResponseError
	if errors.As(err, &responseErr) {
		status = responseErr.HTTPStatusCode()
	}
	if status == 403 || strings.Contains(code, "accessdenied") || strings.Contains(code, "unauthorized") {
		return ErrorAccess
	}
	if status == 404 || code == "notfound" || code == "nosuchkey" || code == "resourcenotfoundexception" {
		return ErrorNotFound
	}
	if status == 429 || status >= 500 || strings.Contains(code, "throttl") || strings.Contains(code, "provisionedthroughput") || strings.Contains(code, "requestlimit") || strings.Contains(code, "internalserver") || strings.Contains(code, "serviceunavailable") || code == "slowdown" {
		return ErrorRetryable
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) && (networkErr.Timeout() || networkErr.Temporary()) {
		return ErrorRetryable
	}
	return ErrorUnknown
}

func IsConditional(err error) bool { return Classify(err) == ErrorConditional }
func IsNotFound(err error) bool    { return Classify(err) == ErrorNotFound }
func IsAccessDenied(err error) bool {
	return Classify(err) == ErrorAccess
}
func IsRetryable(err error) bool { return Classify(err) == ErrorRetryable }
