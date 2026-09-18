package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/runtime"
	"github.com/miku-wwl/platform-lens/internal/security"
	"gopkg.in/yaml.v3"
)

type Discovery struct{ Limits runtime.Limits }

func (d Discovery) Terraform(workspace, requestedPath string) ([]domain.TerraformTarget, error) {
	root, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	candidates := []string{}
	if requestedPath != "" {
		target, err := security.ResolveExistingWithin(root, requestedPath)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(target)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			target = filepath.Dir(target)
		}
		if hasTerraformFiles(target) {
			candidates = append(candidates, target)
		}
	} else {
		seen := map[string]bool{}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == ".terraform" || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if !entry.IsDir() || !hasTerraformFiles(path) || seen[path] {
				return nil
			}
			for ancestor := filepath.Dir(path); ancestor != root && ancestor != "."; {
				if seen[ancestor] {
					return nil
				}
				parent := filepath.Dir(ancestor)
				if parent == ancestor {
					break
				}
				ancestor = parent
			}
			seen[path] = true
			candidates = append(candidates, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(candidates)
	targets := []domain.TerraformTarget{}
	for index, candidate := range candidates {
		if len(targets) >= d.Limits.MaxTargets {
			break
		}
		files, _ := filepath.Glob(filepath.Join(candidate, "*.tf"))
		relative, _ := filepath.Rel(root, candidate)
		if relative == "." {
			relative = "."
		}
		fileNames := make([]string, 0, len(files))
		for _, file := range files {
			rel, _ := filepath.Rel(root, file)
			fileNames = append(fileNames, filepath.ToSlash(rel))
		}
		sort.Strings(fileNames)
		lockfile := filepath.Join(candidate, ".terraform.lock.hcl")
		hash := ""
		if data, err := os.ReadFile(lockfile); err == nil {
			hash = hashBytes(data)
		}
		targets = append(targets, domain.TerraformTarget{TargetID: "terraform-" + itoa(index+1), RootPath: filepath.ToSlash(relative), DiscoveryMethod: "CONSERVATIVE_ROOT_DISCOVERY", Files: fileNames, LockfilePresent: hash != "", SourceLockfileHash: hash})
	}
	return targets, nil
}

func hasTerraformFiles(path string) bool {
	files, _ := filepath.Glob(filepath.Join(path, "*.tf"))
	return len(files) > 0
}

func (d Discovery) Kubernetes(workspace string) ([]domain.ResourceIdentity, []map[string]string, error) {
	resources := []domain.ResourceIdentity{}
	skipped := []map[string]string{}
	err := filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		relative, _ := filepath.Rel(workspace, path)
		relative = filepath.ToSlash(relative)
		lower := strings.ToLower(relative)
		if strings.Contains(lower, "/templates/") || strings.Contains(lower, "/overlays/") || strings.Contains(lower, "/base/") {
			skipped = append(skipped, map[string]string{"file": relative, "reason": string(domain.ValidationSkippedUnsupported)})
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			skipped = append(skipped, map[string]string{"file": relative, "reason": string(domain.ValidationError)})
			return nil
		}
		if strings.Contains(string(data), "{{") {
			skipped = append(skipped, map[string]string{"file": relative, "reason": string(domain.ValidationSkippedUnsupported)})
			return nil
		}
		decoder := yaml.NewDecoder(strings.NewReader(string(data)))
		index := 0
		for {
			var document map[string]any
			err := decoder.Decode(&document)
			if errors.Is(err, fs.ErrClosed) || errors.Is(err, os.ErrClosed) {
				return nil
			}
			if err != nil {
				if err.Error() == "EOF" {
					break
				}
				skipped = append(skipped, map[string]string{"file": relative, "reason": string(domain.ValidationError)})
				break
			}
			if len(document) == 0 {
				index++
				continue
			}
			apiVersion, _ := document["apiVersion"].(string)
			kind, _ := document["kind"].(string)
			metadata, _ := document["metadata"].(map[string]any)
			if apiVersion != "" && kind != "" {
				resource := domain.ResourceIdentity{APIVersion: apiVersion, Kind: kind, File: relative, DocumentIndex: index}
				if metadata != nil {
					resource.Namespace, _ = metadata["namespace"].(string)
					resource.Name, _ = metadata["name"].(string)
				}
				resources = append(resources, resource)
			}
			index++
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return resources, skipped, nil
}

func hashBytes(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	return result
}
