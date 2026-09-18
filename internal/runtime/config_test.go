package runtime

import "testing"

func TestBackendModeValidation(t *testing.T) {
	local := DefaultConfig()
	if err := local.Validate(); err != nil {
		t.Fatal(err)
	}
	local.BackendMode = BackendLocal
	local.AWSEndpointURL = "http://localhost:4566"
	if err := local.Validate(); err == nil {
		t.Fatal("local backend accepted an AWS endpoint")
	}
	aws := DefaultConfig()
	aws.BackendMode = BackendAWS
	aws.AWSEndpointURL = ""
	if err := aws.Validate(); err != nil {
		t.Fatalf("aws backend without endpoint should support Stage 2: %v", err)
	}
	aws.BackendMode = "invalid"
	if err := aws.Validate(); err == nil {
		t.Fatal("invalid backend mode accepted")
	}
}

func TestLoadConfigBackendAndRetrySettings(t *testing.T) {
	t.Setenv("PLATFORMLENS_BACKEND", BackendAWS)
	t.Setenv("PLATFORMLENS_AWS_MAX_ATTEMPTS", "5")
	config := LoadConfig()
	if config.BackendMode != BackendAWS || config.AWSMaxAttempts != 5 {
		t.Fatalf("environment settings not loaded: %+v", config)
	}
}
