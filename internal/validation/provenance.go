package validation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/security"
)

type ModuleMetadata struct {
	ModuleKey            string
	DeclaredSource       string
	DeclaredVersionOrRef string
	ResolvedVersion      string
	ResolvedVCSRevision  string
	ResolvedLocalPath    string
}

// BuildProvenance consumes public `terraform modules -json` metadata. The
// caller supplies whether that command completed with valid JSON so an absent
// module graph is represented as PARTIAL rather than inferred from Terraform's
// internal cache files.
func BuildProvenance(workspace string, target domain.TerraformTarget, metadata []ModuleMetadata, metadataAvailable bool) domain.TerraformDependencyProvenance {
	root := filepath.Join(workspace, filepath.FromSlash(target.RootPath))
	lockfile := filepath.Join(root, ".terraform.lock.hcl")
	data, err := os.ReadFile(lockfile)
	effectivePresent := err == nil
	hash := ""
	if effectivePresent {
		hash = hashBytes(data)
	}
	providers := parseProviders(string(data))
	status := "PARTIAL"
	if target.LockfilePresent && len(providers) > 0 {
		status = "COMPLETE"
	}
	modules, moduleStatus := resolveModules(workspace, target.TargetID, metadata, metadataAvailable)
	origin := "GENERATED"
	if target.LockfilePresent {
		origin = "SOURCE"
	}
	return domain.TerraformDependencyProvenance{TargetID: target.TargetID, SourceLockfilePresent: target.LockfilePresent, SourceLockfileHash: target.SourceLockfileHash, EffectiveLockfileHash: hash, LockfileOrigin: origin, ProviderProvenanceStatus: status, Providers: providers, ModuleProvenanceStatus: moduleStatus, Modules: modules}
}

func resolveModules(workspace, targetID string, metadata []ModuleMetadata, metadataAvailable bool) ([]domain.ModuleProvenance, string) {
	if !metadataAvailable {
		return []domain.ModuleProvenance{}, "PARTIAL"
	}
	result := make([]domain.ModuleProvenance, 0, len(metadata))
	complete := true
	for _, module := range metadata {
		item := domain.ModuleProvenance{TargetID: targetID, ModuleKey: module.ModuleKey, DeclaredSource: module.DeclaredSource, DeclaredVersionOrRef: module.DeclaredVersionOrRef, ResolvedVersion: module.ResolvedVersion, ResolvedVCSRevision: module.ResolvedVCSRevision, HashVersion: 1}
		if item.ModuleKey == "" || item.DeclaredSource == "" {
			complete = false
		}
		if module.ResolvedLocalPath != "" {
			if path, err := security.ResolveExistingWithin(workspace, module.ResolvedLocalPath); err == nil {
				if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
					item.ResolvedLocalPath = filepath.ToSlash(module.ResolvedLocalPath)
					item.ContentTreeHash, _ = CanonicalModuleTreeHash(path)
				}
			}
		}
		if item.ContentTreeHash == "" {
			complete = false
		}
		result = append(result, item)
	}
	if !complete {
		return result, "PARTIAL"
	}
	return result, "COMPLETE"
}

// ParseModulesJSON accepts the public Terraform CLI JSON envelope and keeps
// only metadata fields. It intentionally does not read .terraform internals.
func ParseModulesJSON(data []byte) ([]ModuleMetadata, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	result := []ModuleMetadata{}
	seen := map[string]bool{}
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case []any:
			for _, item := range value {
				visit(item)
			}
		case map[string]any:
			if nested, ok := value["modules"]; ok {
				visit(nested)
			}
			if nested, ok := value["Modules"]; ok {
				visit(nested)
			}
			item := ModuleMetadata{
				ModuleKey:            firstJSONString(value, "key", "Key", "module_key", "ModuleKey"),
				DeclaredSource:       firstJSONString(value, "source", "Source", "declared_source", "DeclaredSource"),
				DeclaredVersionOrRef: firstJSONString(value, "version", "Version", "ref", "Ref", "declared_version_or_ref", "DeclaredVersionOrRef"),
				ResolvedVersion:      firstJSONString(value, "resolved_version", "ResolvedVersion", "version", "Version"),
				ResolvedVCSRevision:  firstJSONString(value, "revision", "Revision", "vcs_revision", "VCSRevision", "resolved_vcs_revision", "ResolvedVCSRevision"),
				ResolvedLocalPath:    firstJSONString(value, "dir", "Dir", "path", "Path", "directory", "Directory", "resolved_local_path", "ResolvedLocalPath"),
			}
			if item.ModuleKey != "" || item.DeclaredSource != "" || item.ResolvedLocalPath != "" {
				identity := item.ModuleKey + "\x00" + item.DeclaredSource + "\x00" + item.ResolvedLocalPath
				if !seen[identity] {
					seen[identity] = true
					result = append(result, item)
				}
			}
			for key, nested := range value {
				if key != "modules" && key != "Modules" {
					visit(nested)
				}
			}
		}
	}
	visit(root)
	sort.Slice(result, func(i, j int) bool { return result[i].ModuleKey < result[j].ModuleKey })
	return result, nil
}

func firstJSONString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if item, ok := value[key].(string); ok {
			return item
		}
	}
	return ""
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
