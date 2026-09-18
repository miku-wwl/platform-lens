package validation

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/miku-wwl/platform-lens/internal/domain"
)

func BuildProvenance(workspace string, target domain.TerraformTarget) domain.TerraformDependencyProvenance {
	root := filepath.Join(workspace, filepath.FromSlash(target.RootPath))
	lockfile := filepath.Join(root, ".terraform.lock.hcl")
	data, err := os.ReadFile(lockfile)
	source := err == nil
	hash := ""
	if source {
		hash = hashBytes(data)
	}
	providers := parseProviders(string(data))
	effectiveHash := hash
	status := "PARTIAL"
	if source && len(providers) > 0 {
		status = "COMPLETE"
	}
	return domain.TerraformDependencyProvenance{TargetID: target.TargetID, SourceLockfilePresent: source, SourceLockfileHash: target.SourceLockfileHash, EffectiveLockfileHash: effectiveHash, LockfileOrigin: map[bool]string{true: "SOURCE", false: "GENERATED"}[source], ProviderProvenanceStatus: status, Providers: providers, ModuleProvenanceStatus: "PARTIAL", Modules: []domain.ModuleProvenance{}}
}

func parseProviders(content string) []domain.ProviderSelection {
	blocks := regexp.MustCompile(`provider\s+"([^"]+)"\s*\{(?s)(.*?)\n\}`).FindAllStringSubmatch(content, -1)
	result := []domain.ProviderSelection{}
	for _, block := range blocks {
		version := firstMatch(`version\s*=\s*"([^"]+)"`, block[2])
		constraints := firstMatch(`constraints\s*=\s*"([^"]+)"`, block[2])
		hashes := regexp.MustCompile(`"(h1:[^"]+|zh:[^"]+)"`).FindAllStringSubmatch(block[2], -1)
		packageHashes := []string{}
		for _, match := range hashes {
			packageHashes = append(packageHashes, match[1])
		}
		sort.Strings(packageHashes)
		result = append(result, domain.ProviderSelection{SourceAddress: block[1], SelectedVersion: version, Constraints: constraints, PackageHashes: packageHashes})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SourceAddress < result[j].SourceAddress })
	return result
}
func firstMatch(pattern, value string) string {
	match := regexp.MustCompile(pattern).FindStringSubmatch(value)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func CanonicalModuleTreeHash(root string) (string, error) {
	entries := []string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		for _, part := range parts {
			if part == ".git" || part == ".terraform" || part == "__pycache__" {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		relative = filepath.ToSlash(relative)
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entries = append(entries, "symlink\x00"+relative+"\x00"+target+"\n")
			return nil
		}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			entries = append(entries, "file\x00"+relative+"\x00"+string(data)+"\n")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(entries)
	sum := sha256.Sum256([]byte(strings.Join(entries, "")))
	return hex.EncodeToString(sum[:]), nil
}
func hashBytes(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
