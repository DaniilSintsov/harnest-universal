// Package checks loads and runs explicitly approved verification commands.
package checks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	goyaml "gopkg.in/yaml.v3"
)

const (
	defaultTimeout = 60 * time.Second
	outputLimit    = 64 << 10
)

var errOutputLimit = errors.New("check output limit exceeded")

type Check struct {
	ID             string   `yaml:"id"`
	Command        string   `yaml:"command"`
	Args           []string `yaml:"args,omitempty"`
	Sources        []string `yaml:"sources,omitempty"`
	Approved       bool     `yaml:"approved"`
	TimeoutSeconds int      `yaml:"timeout_seconds,omitempty"`
}

func (c *Check) UnmarshalYAML(node *goyaml.Node) error {
	type plain Check
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "timeout_seconds" && decoded.TimeoutSeconds <= 0 {
			return fmt.Errorf("timeout_seconds must be positive")
		}
	}
	*c = Check(decoded)
	return nil
}

func Load(projectDir, root, id string) (Check, error) {
	if id == "" || strings.ContainsAny(id, "/\\") {
		return Check{}, fmt.Errorf("invalid check id %q", id)
	}
	dir := root
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(projectDir, root)
	}
	path := filepath.Join(dir, id+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return Check{}, fmt.Errorf("reading check %q: %w", id, err)
	}
	var check Check
	if err := goyaml.Unmarshal(data, &check); err != nil {
		return Check{}, fmt.Errorf("parsing check %q: %w", id, err)
	}
	if check.ID != id || check.Command == "" {
		return Check{}, fmt.Errorf("invalid check %q", id)
	}
	return check, nil
}

// FailureError means the check ran and returned a non-zero status.
type FailureError struct {
	id     string
	output []byte
}

func (e *FailureError) Error() string { return fmt.Sprintf("check %q failed", e.id) }

func IsFailure(err error) bool {
	var failure *FailureError
	return errors.As(err, &failure)
}

// CapturedOutput returns bounded combined output without including it in error text.
func CapturedOutput(err error) []byte {
	var captured interface{ capturedOutput() []byte }
	if !errors.As(err, &captured) {
		return nil
	}
	return append([]byte(nil), captured.capturedOutput()...)
}

func (e *FailureError) capturedOutput() []byte { return e.output }

type evaluationError struct {
	id, reason string
	output     []byte
}

func (e *evaluationError) Error() string {
	return fmt.Sprintf("check %q %s", e.id, e.reason)
}

func (e *evaluationError) capturedOutput() []byte { return e.output }

func Run(projectDir string, check Check, changed []string) error {
	output, err := runContext(context.Background(), projectDir, check, changed)
	_, _ = os.Stdout.Write(output)
	return err
}

func RunContext(ctx context.Context, projectDir string, check Check, changed []string) error {
	_, err := runContext(ctx, projectDir, check, changed)
	return err
}

func runContext(ctx context.Context, projectDir string, check Check, changed []string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("check %q was canceled", check.ID)
	}
	if !check.Approved {
		return nil, fmt.Errorf("check %q is not approved", check.ID)
	}
	for _, name := range changed {
		if strings.ContainsAny(name, "\x00\r\n") {
			return nil, fmt.Errorf("check %q has a changed path not representable in HARNEST_CHANGED_FILES", check.ID)
		}
	}
	if check.TimeoutSeconds < 0 || int64(check.TimeoutSeconds) > int64((time.Duration(1<<63-1))/time.Second) {
		return nil, fmt.Errorf("check %q has invalid timeout", check.ID)
	}
	timeout := defaultTimeout
	if check.TimeoutSeconds > 0 {
		timeout = time.Duration(check.TimeoutSeconds) * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command(check.Command, check.Args...)
	cmd.Dir = projectDir
	cmd.Env = replaceEnv(os.Environ(), "HARNEST_CHANGED_FILES", strings.Join(changed, "\n"))
	cmd.WaitDelay = time.Second
	configureProcess(cmd)
	var exceeded atomic.Bool
	output := &limitedWriter{remaining: outputLimit, exceeded: &exceeded, cancel: cancel}
	cmd.Stdout = output
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("check %q could not start", check.ID)
	}

	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	var runErr error
	var cleanupErr error
	select {
	case runErr = <-wait:
	case <-runCtx.Done():
		cleanupErr = killProcessTree(cmd)
		if cleanupErr != nil && cmd.Process != nil {
			cleanupErr = cmd.Process.Kill()
		}
		select {
		case runErr = <-wait:
		case <-time.After(2 * time.Second):
			return output.Bytes(), &evaluationError{id: check.ID, reason: "process cleanup timed out", output: output.Bytes()}
		}
	}
	if exceeded.Load() {
		return output.Bytes(), &evaluationError{id: check.ID, reason: "exceeded output limit", output: output.Bytes()}
	}
	if runCtx.Err() != nil {
		if cleanupErr != nil {
			return output.Bytes(), &evaluationError{id: check.ID, reason: "process cleanup failed", output: output.Bytes()}
		}
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return output.Bytes(), &evaluationError{id: check.ID, reason: "timed out", output: output.Bytes()}
		}
		return output.Bytes(), &evaluationError{id: check.ID, reason: "was canceled", output: output.Bytes()}
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return output.Bytes(), &FailureError{id: check.ID, output: output.Bytes()}
		}
		return output.Bytes(), &evaluationError{id: check.ID, reason: "could not be evaluated", output: output.Bytes()}
	}
	return output.Bytes(), nil
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

type limitedWriter struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	remaining int64
	exceeded  *atomic.Bool
	cancel    context.CancelFunc
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if int64(len(p)) > w.remaining {
		_, _ = w.buffer.Write(p[:w.remaining])
		w.exceeded.Store(true)
		w.cancel()
		return 0, errOutputLimit
	}
	w.remaining -= int64(len(p))
	return w.buffer.Write(p)
}

func (w *limitedWriter) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buffer.Bytes()...)
}

var _ io.Writer = (*limitedWriter)(nil)
