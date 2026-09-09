//go:build !windows

package checks

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunContextKillsChildProcess(t *testing.T) {
	t.Setenv("GO_WANT_HARNEST_TREE_HELPER", "parent")
	pidFile := t.TempDir() + "/child.pid"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunContext(ctx, t.TempDir(), Check{
			ID: "tree", Command: os.Args[0],
			Args: []string{"-test.run=TestRunnerTreeHelper", "--", pidFile}, Approved: true,
		}, nil)
	}()
	var pid int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		cancel()
		t.Fatal("child process did not start")
	}
	cancel()
	if err := <-done; err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancellation, got %v", err)
	}
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child process %d survived cancellation", pid)
}

func TestRunnerTreeHelper(t *testing.T) {
	switch os.Getenv("GO_WANT_HARNEST_TREE_HELPER") {
	case "parent":
		pidFile := os.Args[len(os.Args)-1]
		cmd := exec.Command(os.Args[0], "-test.run=TestRunnerTreeHelper")
		cmd.Env = replaceEnv(os.Environ(), "GO_WANT_HARNEST_TREE_HELPER", "child")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
			t.Fatal(err)
		}
		_ = cmd.Wait()
	case "child":
		time.Sleep(time.Minute)
	}
}
