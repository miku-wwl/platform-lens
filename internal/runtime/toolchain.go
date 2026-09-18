package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

type ToolVersion struct {
	Version string `json:"version"`
}

type ToolchainLock struct {
	SchemaVersion int         `json:"schema_version"`
	Go            ToolVersion `json:"go"`
	Terraform     ToolVersion `json:"terraform"`
	TFLint        ToolVersion `json:"tflint"`
	Kubeconform   struct {
		ToolVersion
		KubernetesVersion      string `json:"kubernetes_version"`
		SchemaRepository       string `json:"schema_repository"`
		SchemaRepositoryCommit string `json:"schema_repository_commit"`
		SchemaLocationTemplate string `json:"schema_location_template"`
	} `json:"kubeconform"`
}

type Toolchain struct {
	Lock ToolchainLock
	Hash string
	Raw  []byte
}

func LoadToolchain(path string) (Toolchain, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Toolchain{}, err
	}
	var lock ToolchainLock
	if err := json.Unmarshal(raw, &lock); err != nil {
		return Toolchain{}, err
	}
	sum := sha256.Sum256(raw)
	return Toolchain{Lock: lock, Hash: hex.EncodeToString(sum[:]), Raw: raw}, nil
}

func (t Toolchain) Expected(tool string) string {
	switch tool {
	case "go":
		return t.Lock.Go.Version
	case "terraform":
		return t.Lock.Terraform.Version
	case "tflint":
		return t.Lock.TFLint.Version
	case "kubeconform":
		return t.Lock.Kubeconform.Version
	}
	return ""
}

func (t Toolchain) Actual(tool string) string {
	name := tool
	args := []string{"--version"}
	if tool == "terraform" {
		args = []string{"version"}
	} else if tool == "kubeconform" {
		args = []string{"-v"}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	out, err := exec.Command(path, args...).CombinedOutput()
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`(?:v|version\s+)?(\d+\.\d+\.\d+)`)
	match := re.FindStringSubmatch(string(out))
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func (t Toolchain) Check(tool string) (bool, string, string) {
	expected := t.Expected(tool)
	actual := t.Actual(tool)
	return actual == expected, actual, expected
}

func (t Toolchain) Verify(tool string, allowOverride bool) (string, error) {
	expected := t.Expected(tool)
	actual := t.Actual(tool)
	if actual == "" {
		return "", fmt.Errorf("%s executable is missing or version output is malformed", tool)
	}
	if actual != expected && !allowOverride {
		return actual, fmt.Errorf("%s version mismatch: expected %s, observed %s", tool, expected, actual)
	}
	return actual, nil
}

func (t Toolchain) Report() map[string]any {
	result := map[string]any{"lock_hash": t.Hash, "go_runtime": runtime.Version(), "tools": map[string]any{}}
	tools := result["tools"].(map[string]any)
	for _, tool := range []string{"terraform", "tflint", "kubeconform"} {
		ok, actual, expected := t.Check(tool)
		tools[tool] = map[string]any{"expected": expected, "actual": actual, "match": ok}
	}
	return result
}

func (t Toolchain) ValidateTerraformBaseline() error {
	actual := t.Actual("terraform")
	if actual == "" {
		return fmt.Errorf("terraform is not installed")
	}
	parts := strings.Split(actual, ".")
	if len(parts) < 2 || parts[0] < "1" || (parts[0] == "1" && parts[1] < "10") {
		return fmt.Errorf("terraform %s is below 1.10", actual)
	}
	return nil
}
