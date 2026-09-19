package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

type lockOwner struct {
	PID      int       `json:"pid"`
	Hostname string    `json:"hostname"`
	Started  time.Time `json:"started"`
}

func AcquireFileLock(path string, timeout time.Duration) (*FileLock, error) {
	deadline := time.Now().Add(timeout)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	for {
		handle, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			owner, _ := json.Marshal(lockOwner{PID: os.Getpid(), Hostname: hostname(), Started: time.Now().UTC()})
			if _, writeErr := handle.Write(owner); writeErr != nil {
				_ = handle.Close()
				_ = os.Remove(path)
				return nil, writeErr
			}
			return &FileLock{path: path, handle: handle}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if stale, ok := staleLock(path); ok && stale {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("repository lock timeout: %s", path)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func hostname() string {
	value, err := os.Hostname()
	if err != nil {
		return "unknown-host"
	}
	return value
}

func staleLock(path string) (bool, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, false
	}
	var owner lockOwner
	if json.Unmarshal(data, &owner) != nil || owner.PID <= 0 {
		return false, false
	}
	if owner.Hostname != "" && owner.Hostname != hostname() {
		return false, true
	}
	return !processAlive(owner.PID), true
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
	if strings.Contains(value, "\x00") || scpLike(value) {
		return "", fmt.Errorf("only HTTPS repositories are supported")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if parsed.User != nil {
		return "", fmt.Errorf("credential-bearing repository URL is rejected")
	}
	if strings.EqualFold(parsed.Scheme, "https") {
		parsed.Scheme = "https"
		parsed.Host = strings.ToLower(parsed.Host)
		if parsed.Host == "" {
			return "", fmt.Errorf("HTTPS repository URL must include a host")
		}
		for key := range parsed.Query() {
			key = strings.ToLower(key)
			if strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "password") || strings.Contains(key, "credential") || strings.Contains(key, "api_key") {
				return "", fmt.Errorf("credential-bearing repository URL is rejected")
			}
		}
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String(), nil
	}
	if allowLocal && parsed.Scheme == "" && parsed.Host == "" && !strings.Contains(value, ":") {
		return value, nil
	}
	return "", fmt.Errorf("only HTTPS repositories are supported")
}

func scpLike(value string) bool {
	colon := strings.IndexByte(value, ':')
	if colon <= 0 || strings.Contains(value[:colon], "/") || strings.Contains(value[:colon], "\\") {
		return false
	}
	return strings.Contains(value[:colon], "@")
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

type refCandidate struct {
	name string
	kind domain.RefType
}

func (r *Runtime) sourceCachePath(repositoryURL string) (string, string, error) {
	canonical, err := CanonicalURL(repositoryURL, r.Config.AllowLocalGit)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(canonical))
	cache := filepath.Join(r.Config.SourceCacheDir, hex.EncodeToString(sum[:])[:32])
	return canonical, cache, nil
}

func (r *Runtime) Acquire(ctx context.Context, repositoryURL, requestedRef string) (AcquiredSource, error) {
	canonical, cache, err := r.sourceCachePath(repositoryURL)
	if err != nil {
		return AcquiredSource{}, err
	}
	ref, err := NormalizeRef(requestedRef)
	if err != nil {
		return AcquiredSource{}, err
	}
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

// AcquirePinned materializes an already recorded commit without consulting the
// requested branch or tag again. This is the recovery/replay path.
func (r *Runtime) AcquirePinned(ctx context.Context, repositoryURL, commitOID, resolved string, refType domain.RefType) (AcquiredSource, error) {
	canonical, cache, err := r.sourceCachePath(repositoryURL)
	if err != nil {
		return AcquiredSource{}, err
	}
	if !fullOID.MatchString(commitOID) {
		return AcquiredSource{}, ErrShortOID
	}
	lock, err := AcquireFileLock(cache+".lock", time.Minute)
	if err != nil {
		return AcquiredSource{}, err
	}
	defer lock.Close()
	if err := r.ensureCache(ctx, cache, canonical); err != nil {
		return AcquiredSource{}, err
	}
	if _, err := r.rev(ctx, cache, commitOID+"^{commit}"); err != nil {
		if _, fetchErr := r.gitChecked(ctx, cache, "fetch", "--prune", "origin", commitOID); fetchErr != nil {
			return AcquiredSource{}, fetchErr
		}
	}
	oid, err := r.rev(ctx, cache, commitOID+"^{commit}")
	if err != nil {
		return AcquiredSource{}, err
	}
	if _, _, _, err := r.verify(ctx, cache, oid, resolved, refType); err != nil {
		return AcquiredSource{}, err
	}
	return AcquiredSource{CommitOID: oid, ResolvedRef: resolved, RefType: refType, CachePath: cache}, nil
}

func (r *Runtime) env() map[string]string {
	return map[string]string{"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": r.EmptyConfig, "GIT_CONFIG_SYSTEM": r.EmptyConfig, "GIT_TERMINAL_PROMPT": "0", "GIT_ASKPASS": "", "GIT_CONFIG_COUNT": "0", "HOME": r.Home, "XDG_CONFIG_HOME": r.XDG, "GIT_TEMPLATE_DIR": r.EmptyHooks}
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
		remoteHead, err := r.remoteHead(ctx, cache)
		if err != nil {
			return err
		}
		_, err = r.gitChecked(ctx, cache, "fetch", "--prune", "origin", "+"+remoteHead+":"+remoteHead)
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
		remoteHead, err := r.remoteHead(ctx, cache)
		if err != nil {
			return "", "", "", err
		}
		oid, err := r.rev(ctx, cache, remoteHead+"^{commit}")
		if err != nil {
			return "", "", "", err
		}
		return r.verify(ctx, cache, oid, remoteHead, domain.RefHead)
	}
	refs := refCandidates(ref)
	existing := make([]refCandidate, 0, len(refs))
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

func refCandidates(ref string) []refCandidate {
	if strings.HasPrefix(ref, "refs/heads/") {
		return []refCandidate{{name: ref, kind: domain.RefBranch}}
	}
	if strings.HasPrefix(ref, "refs/tags/") {
		return []refCandidate{{name: ref, kind: domain.RefTag}}
	}
	return []refCandidate{
		{name: "refs/heads/" + ref, kind: domain.RefBranch},
		{name: "refs/tags/" + ref, kind: domain.RefTag},
	}
}

func (r *Runtime) remoteHead(ctx context.Context, cache string) (string, error) {
	result, err := r.gitChecked(ctx, cache, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(result.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "ref:" && fields[2] == "HEAD" && strings.HasPrefix(fields[1], "refs/heads/") {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("remote HEAD does not advertise a default branch")
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

func (r *Runtime) CreateWorkspace(ctx context.Context, cache, oid, runID string, attempt int) (string, error) {
	lock, err := AcquireFileLock(cache+".lock", time.Minute)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	workspace := filepath.Join(r.Config.WorkspaceDir, fmt.Sprintf("attempt-%d-%s-%s", attempt, oid[:12], runtime.NewID()))
	if err := os.MkdirAll(filepath.Dir(workspace), 0o700); err != nil {
		return "", err
	}
	if _, err := r.gitChecked(ctx, cache, "-c", "core.hooksPath="+r.EmptyHooks, "-c", "credential.helper=", "worktree", "add", "--detach", workspace, oid); err != nil {
		return "", err
	}
	marker := WorkspaceMarker{RunID: runID, AttemptNo: attempt, CommitOID: oid, CreatedAt: time.Now().UTC()}
	data, _ := json.Marshal(marker)
	if err := os.WriteFile(filepath.Join(workspace, ".platformlens-workspace.json"), data, 0o600); err != nil {
		_, _ = r.gitChecked(ctx, cache, "worktree", "remove", "--force", workspace)
		return "", err
	}
	return workspace, nil
}
func (r *Runtime) CleanupWorkspace(ctx context.Context, cache, workspace string) error {
	lock, err := AcquireFileLock(cache+".lock", time.Minute)
	if err != nil {
		return err
	}
	defer lock.Close()
	_, _ = r.gitChecked(ctx, cache, "-c", "core.hooksPath="+r.EmptyHooks, "-c", "credential.helper=", "worktree", "remove", "--force", workspace)
	return os.RemoveAll(workspace)
}

type WorkspaceMarker struct {
	RunID     string    `json:"run_id"`
	AttemptNo int       `json:"attempt_no"`
	CommitOID string    `json:"commit_oid"`
	CreatedAt time.Time `json:"created_at"`
}

// SweepWorkspaces is conservative by default. Callers must supply ownership
// knowledge before any old workspace is removed.
func SweepWorkspaces(root string, maxAge time.Duration, protected ...func(WorkspaceMarker) bool) ([]string, error) {
	if len(protected) == 0 {
		return nil, nil
	}
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
		data, err := os.ReadFile(filepath.Join(path, ".platformlens-workspace.json"))
		if err != nil {
			continue
		}
		var marker WorkspaceMarker
		if json.Unmarshal(data, &marker) != nil || protected[0](marker) {
			continue
		}
		if err := os.RemoveAll(path); err == nil {
			removed = append(removed, path)
		}
	}
	return removed, nil
}

// Keep this package independent from external Git libraries; all behavior is delegated to Git CLI.
