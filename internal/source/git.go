package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/miku-wwl/platform-lens/internal/domain"
	"github.com/miku-wwl/platform-lens/internal/execution"
	"github.com/miku-wwl/platform-lens/internal/runtime"
)

var (
	ErrSource       = errors.New("source acquisition failed")
	ErrShortOID     = errors.New("short commit OID is rejected")
	ErrAmbiguousRef = errors.New("branch/tag reference is ambiguous")
	ErrInvalidRef   = errors.New("invalid Git reference")
)

var fullOID = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
var shortOID = regexp.MustCompile(`^[0-9a-fA-F]{4,39}$`)

type FileLock struct {
	path   string
	handle *os.File
}

func AcquireFileLock(path string, timeout time.Duration) (*FileLock, error) {
	deadline := time.Now().Add(timeout)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	for {
		handle, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			return &FileLock{path: path, handle: handle}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("repository lock timeout: %s", path)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (l *FileLock) Close() error {
	if l == nil {
		return nil
	}
	if l.handle != nil {
		_ = l.handle.Close()
	}
	return os.Remove(l.path)
}

type Runtime struct {
	Config      runtime.Config
	Runner      *execution.CommandRunner
	EmptyConfig string
	EmptyHooks  string
	Home        string
	XDG         string
}

func NewRuntime(config runtime.Config, runner *execution.CommandRunner) (*Runtime, error) {
	root := filepath.Join(config.DataDir, "git-runtime")
	for _, path := range []string{root, filepath.Join(root, "empty-hooks"), filepath.Join(root, "home"), filepath.Join(root, "xdg")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, err
		}
	}
	emptyConfig := filepath.Join(root, "empty-git-config")
	if file, err := os.OpenFile(emptyConfig, os.O_CREATE|os.O_RDWR, 0o600); err != nil {
		return nil, err
	} else {
		_ = file.Close()
	}
	return &Runtime{Config: config, Runner: runner, EmptyConfig: emptyConfig, EmptyHooks: filepath.Join(root, "empty-hooks"), Home: filepath.Join(root, "home"), XDG: filepath.Join(root, "xdg")}, nil
}

func CanonicalURL(value string, allowLocal bool) (string, error) {
	value = strings.TrimSpace(value)
	if allowLocal && filepath.IsAbs(value) {
		return value, nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if parsed.User != nil {
		return "", fmt.Errorf("credential-bearing repository URL is rejected")
	}
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		parsed.Host = strings.ToLower(parsed.Host)
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String(), nil
	}
	if allowLocal && parsed.Scheme == "" {
		return value, nil
	}
	return "", fmt.Errorf("only HTTPS repositories are supported")
}

func NormalizeRef(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "-") {
		return "", ErrInvalidRef
	}
	if shortOID.MatchString(value) && !fullOID.MatchString(value) {
		return "", ErrShortOID
	}
	if strings.HasPrefix(value, "refs/") && !strings.HasPrefix(value, "refs/heads/") && !strings.HasPrefix(value, "refs/tags/") {
		return "", ErrInvalidRef
	}
	return value, nil
}

type AcquiredSource struct {
	CommitOID   string
	ResolvedRef string
	RefType     domain.RefType
	CachePath   string
}

func (r *Runtime) Acquire(ctx context.Context, repositoryURL, requestedRef string) (AcquiredSource, error) {
	canonical, err := CanonicalURL(repositoryURL, r.Config.AllowLocalGit)
	if err != nil {
		return AcquiredSource{}, err
	}
	ref, err := NormalizeRef(requestedRef)
	if err != nil {
		return AcquiredSource{}, err
	}
	sum := sha256.Sum256([]byte(canonical))
	cache := filepath.Join(r.Config.SourceCacheDir, hex.EncodeToString(sum[:])[:32])
	lock, err := AcquireFileLock(cache+".lock", time.Minute)
	if err != nil {
		return AcquiredSource{}, err
	}
	defer lock.Close()
	if err := r.ensureCache(ctx, cache, canonical); err != nil {
		return AcquiredSource{}, err
	}
	if err := r.fetch(ctx, cache, ref); err != nil {
		return AcquiredSource{}, err
	}
	oid, resolved, kind, err := r.resolve(ctx, cache, ref)
	if err != nil {
		return AcquiredSource{}, err
	}
	return AcquiredSource{CommitOID: oid, ResolvedRef: resolved, RefType: kind, CachePath: cache}, nil
}

func (r *Runtime) env() map[string]string {
	return map[string]string{"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": r.EmptyConfig, "GIT_CONFIG_SYSTEM": r.EmptyConfig, "GIT_TERMINAL_PROMPT": "0", "GIT_ASKPASS": "", "HOME": r.Home, "XDG_CONFIG_HOME": r.XDG}
}

func (r *Runtime) IsolatedEnvironmentForTest() map[string]string { return r.env() }

func (r *Runtime) git(ctx context.Context, cwd string, args ...string) execution.CommandResult {
	return r.Runner.Run(ctx, execution.CommandSpec{Executable: "git", Args: args, WorkingDirectory: cwd, Environment: r.env(), AllowedExecutables: map[string]bool{"git": true}, Timeout: time.Duration(r.Config.Limits.CommandTimeoutSeconds) * time.Second, StdoutCap: r.Config.Limits.MaxRawOutputBytes, StderrCap: r.Config.Limits.MaxRawOutputBytes})
}

func (r *Runtime) gitChecked(ctx context.Context, cwd string, args ...string) (execution.CommandResult, error) {
	result := r.git(ctx, cwd, args...)
	if result.StartError != "" || result.ExitCode == nil || *result.ExitCode != 0 {
		return result, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), ErrSource, string(result.Stderr))
	}
	return result, nil
}

func (r *Runtime) ensureCache(ctx context.Context, cache, url string) error {
	if _, err := os.Stat(filepath.Join(cache, "HEAD")); errors.Is(err, os.ErrNotExist) {
		if _, err := r.gitChecked(ctx, "", "init", "--bare", cache); err != nil {
			return err
		}
		if _, err := r.gitChecked(ctx, cache, "remote", "add", "origin", url); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if _, err := r.gitChecked(ctx, cache, "remote", "set-url", "origin", url); err != nil {
		return err
	}
	return nil
}

func (r *Runtime) fetch(ctx context.Context, cache, ref string) error {
	if fullOID.MatchString(ref) {
		_, err := r.gitChecked(ctx, cache, "fetch", "--prune", "origin", ref)
		return err
	}
	if ref == "HEAD" {
		_, err := r.gitChecked(ctx, cache, "fetch", "--prune", "origin", "+refs/heads/*:refs/heads/*")
		return err
	}
	normalized := strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/tags/")
	refs := []string{ref}
	if !strings.HasPrefix(ref, "refs/") {
		refs = []string{"refs/heads/" + normalized, "refs/tags/" + normalized}
	}
	var successful bool
	var last error
	for _, item := range refs {
		_, err := r.gitChecked(ctx, cache, "fetch", "--prune", "origin", "+"+item+":"+item)
		if err == nil {
			successful = true
		} else {
			last = err
		}
	}
	if !successful {
		return last
	}
	return nil
}

func (r *Runtime) resolve(ctx context.Context, cache, ref string) (string, string, domain.RefType, error) {
	if shortOID.MatchString(ref) && !fullOID.MatchString(ref) {
		return "", "", "", ErrShortOID
	}
	if fullOID.MatchString(ref) {
		oid, err := r.rev(ctx, cache, ref+"^{commit}")
		if err != nil {
			return "", "", "", err
		}
		return r.verify(ctx, cache, oid, ref, domain.RefCommit)
	}
	if ref == "HEAD" {
		oid, err := r.rev(ctx, cache, "HEAD^{commit}")
		if err != nil {
			return "", "", "", err
		}
		resolved := "HEAD"
		if result, e := r.gitChecked(ctx, cache, "symbolic-ref", "-q", "HEAD"); e == nil {
			resolved = strings.TrimSpace(string(result.Stdout))
		}
		return r.verify(ctx, cache, oid, resolved, domain.RefHead)
	}
	refs := []struct {
		name string
		kind domain.RefType
	}{}
	if strings.HasPrefix(ref, "refs/heads/") {
		refs = append(refs, struct {
			name string
			kind domain.RefType
		}{ref, domain.RefBranch})
	} else if strings.HasPrefix(ref, "refs/tags/") {
		refs = append(refs, struct {
			name string
			kind domain.RefType
		}{ref, domain.RefTag})
	} else {
		refs = append(refs, struct {
			name string
			kind domain.RefType
		}{"refs/heads/" + ref, domain.RefBranch}, struct {
			name string
			kind domain.RefType
		}{"refs/tags/" + ref, domain.RefTag})
	}
	existing := make([]struct {
		name string
		kind domain.RefType
	}, 0, 2)
	for _, candidate := range refs {
		result := r.git(ctx, cache, "show-ref", "--verify", "--quiet", candidate.name)
		if result.ExitCode != nil && *result.ExitCode == 0 {
			existing = append(existing, candidate)
		}
	}
	if len(existing) > 1 {
		return "", "", "", ErrAmbiguousRef
	}
	if len(existing) == 0 {
		return "", "", "", fmt.Errorf("reference not found: %s", ref)
	}
	oid, err := r.rev(ctx, cache, existing[0].name+"^{commit}")
	if err != nil {
		return "", "", "", err
	}
	return r.verify(ctx, cache, oid, existing[0].name, existing[0].kind)
}

func (r *Runtime) rev(ctx context.Context, cache, ref string) (string, error) {
	result, err := r.gitChecked(ctx, cache, "rev-parse", "--verify", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}
func (r *Runtime) verify(ctx context.Context, cache, oid, resolved string, kind domain.RefType) (string, string, domain.RefType, error) {
	result, err := r.gitChecked(ctx, cache, "cat-file", "-t", oid)
	if err != nil || strings.TrimSpace(string(result.Stdout)) != "commit" || !fullOID.MatchString(oid) {
		return "", "", "", fmt.Errorf("resolved object is not a commit")
	}
	return oid, resolved, kind, nil
}

func (r *Runtime) CreateWorkspace(ctx context.Context, cache, oid string, attempt int) (string, error) {
	workspace := filepath.Join(r.Config.WorkspaceDir, fmt.Sprintf("attempt-%d-%s-%s", attempt, oid[:12], runtime.NewID()))
	if err := os.MkdirAll(filepath.Dir(workspace), 0o700); err != nil {
		return "", err
	}
	if _, err := r.gitChecked(ctx, cache, "-c", "core.hooksPath="+r.EmptyHooks, "-c", "credential.helper=", "worktree", "add", "--detach", workspace, oid); err != nil {
		return "", err
	}
	return workspace, nil
}
func (r *Runtime) CleanupWorkspace(ctx context.Context, cache, workspace string) error {
	_, _ = r.gitChecked(ctx, cache, "-c", "core.hooksPath="+r.EmptyHooks, "-c", "credential.helper=", "worktree", "remove", "--force", workspace)
	return os.RemoveAll(workspace)
}

func SweepWorkspaces(root string, maxAge time.Duration) ([]string, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := time.Now()
	removed := []string{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if err := os.RemoveAll(path); err == nil {
			removed = append(removed, path)
		}
	}
	return removed, nil
}

// Keep this package independent from external Git libraries; all behavior is delegated to Git CLI.
