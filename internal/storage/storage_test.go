package storage

import (
	"errors"
	"net/http"
	"testing"

	smithyhttp "github.com/aws/smithy-go/transport/http"
)

func TestClassifyExistsError(t *testing.T) {
	missing := &smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: http.StatusNotFound}}, Err: errors.New("missing")}
	denied := &smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: http.StatusForbidden}}, Err: errors.New("denied")}
	retryable := &smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: http.StatusServiceUnavailable}}, Err: errors.New("unavailable")}
	if exists, err := classifyExistsError(nil); !exists || err != nil {
		t.Fatalf("successful head classified incorrectly: %v %v", exists, err)
	}
	if exists, err := classifyExistsError(missing); exists || err != nil {
		t.Fatalf("missing object classified incorrectly: %v %v", exists, err)
	}
	for name, input := range map[string]error{"access denied": denied, "service failure": retryable} {
		t.Run(name, func(t *testing.T) {
			if exists, err := classifyExistsError(input); exists || err == nil {
				t.Fatalf("meaningful cloud error was swallowed: %v %v", exists, err)
			}
		})
	}
}
