package execution

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type CommandSpec struct {
	Executable         string
	Args               []string
	WorkingDirectory   string
	Environment        map[string]string
	AllowedExecutables map[string]bool
	Timeout            time.Duration
	StdoutCap          int
	StderrCap          int
}

type CommandResult struct {
	Argv            []string
	ExitCode        *int
	Stdout          []byte
	Stderr          []byte
	Duration        time.Duration
	TimedOut        bool
	Cancelled       bool
	StdoutTruncated bool
	StderrTruncated bool
	StartError      string
}

type CommandRunner struct{ BaseEnvironment map[string]string }

func NewCommandRunner() *CommandRunner {
	base := map[string]string{}
	for _, item := range os.Environ() {
		if key, value, ok := strings.Cut(item, "="); ok {
			base[key] = value
		}
	}
	return &CommandRunner{BaseEnvironment: base}
}

func (r *CommandRunner) Run(ctx context.Context, spec CommandSpec) CommandResult {
	started := time.Now()
	argv := append([]string{spec.Executable}, spec.Args...)
	result := CommandResult{Argv: argv}
	if len(spec.AllowedExecutables) > 0 && !spec.AllowedExecutables[filepath.Base(spec.Executable)] {
		result.StartError = "executable is not allowlisted: " + spec.Executable
		result.Duration = time.Since(started)
		return result
	}
	if spec.WorkingDirectory != "" {
		absolute, err := filepath.Abs(spec.WorkingDirectory)
		if err != nil || !directoryExists(absolute) {
			result.StartError = "working directory is not usable: " + spec.WorkingDirectory
			result.Duration = time.Since(started)
			return result
		}
	}
	commandContext, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(commandContext, spec.Executable, spec.Args...)
	cmd.Dir = spec.WorkingDirectory
	cmd.Env = allowlistedEnvironment(r.BaseEnvironment, spec.Environment)
	configureProcess(cmd)
	stdout := &cappedBuffer{cap: spec.StdoutCap}
	stderr := &cappedBuffer{cap: spec.StderrCap}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		result.StartError = err.Error()
		result.Duration = time.Since(started)
		return result
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		result.Cancelled = true
		terminateProcessTree(cmd, false)
		cancel()
		waitErr = waitWithGrace(done, 3*time.Second, cmd)
	case <-timer.C:
		result.TimedOut = true
		terminateProcessTree(cmd, false)
		cancel()
		waitErr = waitWithGrace(done, 3*time.Second, cmd)
	}
	if waitErr != nil {
		if exitError, ok := waitErr.(*exec.ExitError); ok {
			code := exitError.ExitCode()
			result.ExitCode = &code
		}
	} else if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		result.ExitCode = &code
	}
	result.Stdout, result.StdoutTruncated = stdout.Bytes()
	result.Stderr, result.StderrTruncated = stderr.Bytes()
	result.Duration = time.Since(started)
	return result
}

func waitWithGrace(done <-chan error, grace time.Duration, cmd *exec.Cmd) error {
	select {
	case err := <-done:
		return err
	case <-time.After(grace):
		terminateProcessTree(cmd, true)
		return <-done
	}
}

func allowlistedEnvironment(base, overrides map[string]string) []string {
	allowed := map[string]bool{"PATH": true, "Path": true, "SYSTEMROOT": true, "SystemRoot": true, "WINDIR": true, "TEMP": true, "TMP": true, "HOME": true, "USERPROFILE": true, "LANG": true, "LC_ALL": true, "GIT_CONFIG_NOSYSTEM": true, "GIT_CONFIG_GLOBAL": true, "GIT_TERMINAL_PROMPT": true, "XDG_CONFIG_HOME": true, "GIT_ASKPASS": true, "TF_IN_AUTOMATION": true, "TF_INPUT": true, "TF_DATA_DIR": true, "TF_CLI_CONFIG_FILE": true, "AWS_REGION": true, "AWS_DEFAULT_REGION": true, "AWS_ENDPOINT_URL": true}
	values := map[string]string{}
	for key, value := range base {
		if allowed[key] {
			values[key] = value
		}
	}
	for key, value := range overrides {
		values[key] = value
	}
	out := make([]string, 0, len(values))
	for key, value := range values {
		out = append(out, key+"="+value)
	}
	return out
}

func directoryExists(path string) bool { info, err := os.Stat(path); return err == nil && info.IsDir() }

type cappedBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	cap       int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cap <= 0 {
		b.cap = 2 * 1024 * 1024
	}
	remaining := b.cap - b.buffer.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.buffer.Write(p[:remaining])
			b.truncated = true
		} else {
			_, _ = b.buffer.Write(p)
		}
	} else {
		b.truncated = true
	}
	return len(p), nil
}

func (b *cappedBuffer) Bytes() ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buffer.Bytes()...), b.truncated
}

var _ io.Writer = (*cappedBuffer)(nil)
