package runtime

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
)

type Limits struct {
	MaxRepoBytes          int64 `json:"max_repo_bytes"`
	MaxFiles              int   `json:"max_files"`
	MaxTargets            int   `json:"max_targets"`
	MaxDiagnostics        int   `json:"max_diagnostics"`
	MaxRawOutputBytes     int   `json:"max_raw_output_bytes"`
	MaxSourceExcerptBytes int   `json:"max_source_excerpt_bytes"`
	MaxAgentContextBytes  int   `json:"max_agent_context_bytes"`
	MaxFindings           int   `json:"max_findings"`
	CommandTimeoutSeconds int   `json:"command_timeout_seconds"`
}

type Config struct {
	ServiceName      string `json:"service_name"`
	Version          string `json:"version"`
	DataDir          string `json:"data_dir"`
	DatabasePath     string `json:"database_path"`
	ArtifactDir      string `json:"artifact_dir"`
	WorkspaceDir     string `json:"workspace_dir"`
	SourceCacheDir   string `json:"source_cache_dir"`
	ToolchainPath    string `json:"toolchain_path"`
	WorkerID         string `json:"worker_id"`
	LeaseSeconds     int    `json:"lease_seconds"`
	HeartbeatSeconds int    `json:"heartbeat_seconds"`
	AWSEndpointURL   string `json:"aws_endpoint_url,omitempty"`
	AWSRegion        string `json:"aws_region"`
	DynamoTable      string `json:"dynamodb_table"`
	DynamoGSI        string `json:"dynamodb_gsi"`
	S3Bucket         string `json:"s3_bucket"`
	AllowLocalGit    bool   `json:"allow_local_git"`
	Limits           Limits `json:"limits"`
}

func DefaultConfig() Config {
	return Config{
		ServiceName: "platformlens", Version: "0.1.0", DataDir: ".platformlens",
		DatabasePath: ".platformlens/platformlens.sqlite3", ArtifactDir: ".platformlens/artifacts",
		WorkspaceDir: ".platformlens/workspaces", SourceCacheDir: ".platformlens/source-cache",
		ToolchainPath: "toolchain.lock", WorkerID: "local-worker", LeaseSeconds: 60,
		HeartbeatSeconds: 15, AWSRegion: "us-east-1", DynamoTable: "platformlens-runs",
		DynamoGSI: "candidate-index", S3Bucket: "platformlens-artifacts", AllowLocalGit: true,
		Limits: Limits{MaxRepoBytes: 512 * 1024 * 1024, MaxFiles: 50000, MaxTargets: 128,
			MaxDiagnostics: 10000, MaxRawOutputBytes: 2 * 1024 * 1024, MaxSourceExcerptBytes: 32 * 1024,
			MaxAgentContextBytes: 128 * 1024, MaxFindings: 1000, CommandTimeoutSeconds: 300},
	}
}

func LoadConfig() Config {
	c := DefaultConfig()
	setString(&c.DataDir, "PLATFORMLENS_DATA_DIR")
	setString(&c.DatabasePath, "PLATFORMLENS_DATABASE_PATH")
	setString(&c.ArtifactDir, "PLATFORMLENS_ARTIFACT_DIR")
	setString(&c.WorkspaceDir, "PLATFORMLENS_WORKSPACE_DIR")
	setString(&c.SourceCacheDir, "PLATFORMLENS_SOURCE_CACHE_DIR")
	setString(&c.ToolchainPath, "PLATFORMLENS_TOOLCHAIN_PATH")
	setString(&c.WorkerID, "PLATFORMLENS_WORKER_ID")
	setString(&c.AWSEndpointURL, "AWS_ENDPOINT_URL")
	setString(&c.AWSRegion, "AWS_REGION")
	setString(&c.DynamoTable, "PLATFORMLENS_DYNAMODB_TABLE")
	setString(&c.S3Bucket, "PLATFORMLENS_S3_BUCKET")
	if value := os.Getenv("PLATFORMLENS_LEASE_SECONDS"); value != "" {
		c.LeaseSeconds = parseInt(value, c.LeaseSeconds)
	}
	if value := os.Getenv("PLATFORMLENS_HEARTBEAT_SECONDS"); value != "" {
		c.HeartbeatSeconds = parseInt(value, c.HeartbeatSeconds)
	}
	if c.DataDir != ".platformlens" {
		if c.DatabasePath == DefaultConfig().DatabasePath {
			c.DatabasePath = filepath.Join(c.DataDir, "platformlens.sqlite3")
		}
		if c.ArtifactDir == DefaultConfig().ArtifactDir {
			c.ArtifactDir = filepath.Join(c.DataDir, "artifacts")
		}
		if c.WorkspaceDir == DefaultConfig().WorkspaceDir {
			c.WorkspaceDir = filepath.Join(c.DataDir, "workspaces")
		}
		if c.SourceCacheDir == DefaultConfig().SourceCacheDir {
			c.SourceCacheDir = filepath.Join(c.DataDir, "source-cache")
		}
	}
	return c
}

func (c Config) Prepare() error {
	for _, path := range []string{c.DataDir, c.ArtifactDir, c.WorkspaceDir, c.SourceCacheDir, filepath.Dir(c.DatabasePath)} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func (c Config) Validate() error {
	if c.LeaseSeconds <= 0 || c.HeartbeatSeconds <= 0 {
		return errors.New("lease and heartbeat seconds must be positive")
	}
	for name, value := range map[string]int64{"max_repo_bytes": c.Limits.MaxRepoBytes, "max_files": int64(c.Limits.MaxFiles), "max_targets": int64(c.Limits.MaxTargets), "max_diagnostics": int64(c.Limits.MaxDiagnostics), "max_raw_output_bytes": int64(c.Limits.MaxRawOutputBytes), "max_source_excerpt_bytes": int64(c.Limits.MaxSourceExcerptBytes), "max_agent_context_bytes": int64(c.Limits.MaxAgentContextBytes), "max_findings": int64(c.Limits.MaxFindings), "command_timeout_seconds": int64(c.Limits.CommandTimeoutSeconds)} {
		if value <= 0 {
			return errors.New("resource limit must be positive: " + name)
		}
	}
	return nil
}

func (c Config) JSON() ([]byte, error) { return json.Marshal(c) }

func setString(target *string, name string) {
	if value := os.Getenv(name); value != "" {
		*target = value
	}
}
func parseInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
