package checks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	goyaml "gopkg.in/yaml.v3"
)

func TestRunRejectsUnapprovedCommand(t *testing.T) {
	err := Run(t.TempDir(), Check{ID: "dangerous", Command: "false"}, nil)
	if err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("expected approval error, got %v", err)
	}
}

func TestRunPassesChangedFiles(t *testing.T) {
	t.Setenv("GO_WANT_HARNEST_CHECK_HELPER", "1")
	output := filepath.Join(t.TempDir(), "changed.txt")
	check := Check{
		ID:       "helper",
		Command:  os.Args[0],
		Args:     []string{"-test.run=TestCheckHelper", "--", output},
		Approved: true,
	}
	if err := Run(t.TempDir(), check, []string{"admin/a.ts", "backend/b.go"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "admin/a.ts\nbackend/b.go"; got != want {
		t.Fatalf("changed files = %q, want %q", got, want)
	}
}

func TestCheckHelper(t *testing.T) {
	if os.Getenv("GO_WANT_HARNEST_CHECK_HELPER") != "1" {
		return
	}
	output := os.Args[len(os.Args)-1]
	if err := os.WriteFile(output, []byte(os.Getenv("HARNEST_CHANGED_FILES")), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunContextPreservesArgvCWDAndReplacesEnvironment(t *testing.T) {
	t.Setenv("GO_WANT_HARNEST_RUNNER_HELPER", "inspect")
	t.Setenv("HARNEST_CHANGED_FILES", "stale")
	dir := t.TempDir()
	output := filepath.Join(t.TempDir(), "result.txt")
	check := Check{ID: "inspect", Command: os.Args[0], Args: []string{
		"-test.run=TestRunnerHelper", "--", output, "one arg", "*.go",
	}, Approved: true}
	if err := RunContext(context.Background(), dir, check, []string{"a file.go"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	canonicalDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{canonicalDir, "a file.go", "one arg", "*.go"}, "\n")
	if string(data) != want {
		t.Fatalf("helper result = %q, want %q", data, want)
	}
}

func TestRunContextClassifiesFailure(t *testing.T) {
	t.Setenv("GO_WANT_HARNEST_RUNNER_HELPER", "fail")
	err := RunContext(context.Background(), t.TempDir(), Check{
		ID: "fail", Command: os.Args[0], Args: []string{"-test.run=TestRunnerHelper"}, Approved: true,
	}, nil)
	if !IsFailure(err) {
		t.Fatalf("expected check failure, got %v", err)
	}
	if got := string(CapturedOutput(err)); got != "failure output\n" {
		t.Fatalf("captured output = %q", got)
	}
	if strings.Contains(err.Error(), "failure output") {
		t.Fatalf("output leaked into error: %v", err)
	}
}

func TestRunPreservesSuccessfulOutput(t *testing.T) {
	t.Setenv("GO_WANT_HARNEST_RUNNER_HELPER", "success-output")
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	err = Run(t.TempDir(), Check{
		ID: "output", Command: os.Args[0], Args: []string{"-test.run=TestRunnerHelper"}, Approved: true,
	}, nil)
	os.Stdout = original
	_ = writer.Close()
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 32)
	n, readErr := reader.Read(buf)
	if readErr != nil {
		t.Fatal(readErr)
	}
	_ = reader.Close()
	if got := string(buf[:n]); !strings.Contains(got, "success output\n") {
		t.Fatalf("Run output = %q", got)
	}
}

func TestRunContextRejectsAmbiguousChangedPath(t *testing.T) {
	err := RunContext(context.Background(), t.TempDir(), Check{
		ID: "noop", Command: os.Args[0], Approved: true,
	}, []string{"first\nsecond"})
	if err == nil || IsFailure(err) {
		t.Fatalf("expected evaluation error, got %v", err)
	}
}

func TestRunContextRejectsInvalidTimeout(t *testing.T) {
	for _, seconds := range []int{-1, int((time.Duration(1<<63-1) / time.Second) + 1)} {
		err := RunContext(context.Background(), t.TempDir(), Check{
			ID: "noop", Command: os.Args[0], Approved: true, TimeoutSeconds: seconds,
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "invalid timeout") {
			t.Fatalf("timeout %d: got %v", seconds, err)
		}
	}
}

func TestRunContextDoesNotStartWithCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RunContext(ctx, t.TempDir(), Check{ID: "noop", Command: os.Args[0], Approved: true}, nil)
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestRunContextBoundsTimeAndOutput(t *testing.T) {
	for _, tc := range []struct {
		name, mode, message string
	}{
		{"timeout", "sleep", "timed out"},
		{"output", "output", "output limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GO_WANT_HARNEST_RUNNER_HELPER", tc.mode)
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			err := RunContext(ctx, t.TempDir(), Check{
				ID: tc.name, Command: os.Args[0], Args: []string{"-test.run=TestRunnerHelper"}, Approved: true,
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.message) || IsFailure(err) {
				t.Fatalf("expected %s evaluation error, got %v", tc.message, err)
			}
		})
	}
}

func TestCheckTimeoutYAML(t *testing.T) {
	var omitted Check
	if err := goyaml.Unmarshal([]byte("id: x\ncommand: go\napproved: true\n"), &omitted); err != nil {
		t.Fatal(err)
	}
	if omitted.TimeoutSeconds != 0 {
		t.Fatalf("omitted timeout = %d", omitted.TimeoutSeconds)
	}
	for _, value := range []string{"0", "-1"} {
		var check Check
		err := goyaml.Unmarshal([]byte("id: x\ncommand: go\ntimeout_seconds: "+value+"\n"), &check)
		if err == nil {
			t.Fatalf("timeout_seconds %s accepted", value)
		}
	}
}

func TestRunnerHelper(t *testing.T) {
	switch os.Getenv("GO_WANT_HARNEST_RUNNER_HELPER") {
	case "inspect":
		separator := 0
		for i, arg := range os.Args {
			if arg == "--" {
				separator = i
				break
			}
		}
		output := os.Args[separator+1]
		content := strings.Join([]string{
			mustGetwd(t), os.Getenv("HARNEST_CHANGED_FILES"),
			os.Args[separator+2], os.Args[separator+3],
		}, "\n")
		if err := os.WriteFile(output, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	case "fail":
		_, _ = os.Stdout.Write([]byte("failure output\n"))
		os.Exit(7)
	case "success-output":
		_, _ = os.Stdout.Write([]byte("success output\n"))
	case "sleep":
		time.Sleep(time.Minute)
	case "output":
		_, _ = os.Stdout.Write(make([]byte, outputLimit+1))
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
