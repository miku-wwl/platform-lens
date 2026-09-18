package runtime

import (
	"runtime"
	"time"
)

var (
	ServiceVersion = "0.1.0"
	GitCommit      = "unknown"
	BuildID        = "local"
	ImageTag       = "local"
)

func BuildInfo(toolchainHash string) map[string]string {
	return map[string]string{"service": "platformlens", "version": ServiceVersion, "git_commit": GitCommit, "build_id": BuildID, "image_tag": ImageTag, "go_version": runtime.Version(), "toolchain_lock_hash": toolchainHash, "built_at": time.Now().UTC().Format(time.RFC3339)}
}
