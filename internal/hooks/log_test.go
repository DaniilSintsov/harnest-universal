package hooks

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogOpenRejectsFinalSymlinkAfterValidation(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.WriteFile(filepath.Join(dir, "other-state"), []byte("unchanged"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other-state", filepath.Join(dir, "hooks.jsonl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// Simulate final-component replacement after the initial Lstat check.
	file, err := openLogFile(root, "hooks.jsonl")
	if err == nil {
		file.Close()
		t.Fatal("log open followed swapped final symlink")
	}
	data, err := os.ReadFile(filepath.Join(dir, "other-state"))
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("other state changed: %q, %v", data, err)
	}
}

func TestAppendLogConcurrentProcesses(t *testing.T) {
	root := t.TempDir()
	const processes = 12
	var wg sync.WaitGroup
	errs := make(chan error, processes)
	for i := 0; i < processes; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=TestAppendLogProcessHelper")
			cmd.Env = append(os.Environ(), "GO_WANT_HOOK_LOG_HELPER=1", "HOOK_LOG_ROOT="+root)
			errs <- cmd.Run()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	file, err := os.Open(filepath.Join(root, ".harnest", "state", "hooks.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		var entry logEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("invalid JSONL record: %v", err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != processes {
		t.Fatalf("records = %d, want %d", count, processes)
	}
}

func TestAppendLogProcessHelper(t *testing.T) {
	if os.Getenv("GO_WANT_HOOK_LOG_HELPER") != "1" {
		return
	}
	if err := appendLog(os.Getenv("HOOK_LOG_ROOT"), Options{Platform: "codex", Event: "stop"},
		Event{SessionID: "session"}, []Record{{Status: "passed", RuleIDs: []string{"rule"}}}, time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

func TestAppendLogRotatesOnce(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, ".harnest", "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, "hooks.jsonl")
	old := bytes.Repeat([]byte{'x'}, hookLogLimit-1)
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := appendLog(root, Options{Platform: "codex", Event: "stop"}, Event{},
		[]Record{{Status: "passed"}}, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(previous, old) {
		t.Fatal("rotated log differs from previous log")
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(current) == 0 || len(current) > hookLogLimit {
		t.Fatalf("current log size = %d", len(current))
	}
}

func TestAppendLogExcludesPayloadAndSanitizesIDs(t *testing.T) {
	root := t.TempDir()
	event := Event{
		ToolInput: []byte("{\"secret\":\"RAW-PAYLOAD-SECRET\"}"),
		SessionID: strings.Repeat("a", 140) + string(rune(10)) + "unsafe",
		TurnID:    "turn/id",
	}
	if err := appendLog(root, Options{Platform: "codex", Event: "stop"}, event,
		[]Record{{Status: "evaluation-error", Reason: "safe\nreason", RuleIDs: []string{"rule/id"}}},
		1500*time.Microsecond); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".harnest", "state", "hooks.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("RAW-PAYLOAD-SECRET")) || bytes.Contains(data, []byte("tool_input")) {
		t.Fatalf("raw event payload leaked: %s", data)
	}
	var entry logEntry
	if err := json.Unmarshal(bytes.TrimSpace(data), &entry); err != nil {
		t.Fatal(err)
	}
	if len(entry.SessionID) != 128 || strings.ContainsAny(entry.TurnID, "/\n") ||
		strings.ContainsAny(entry.RuleIDs[0], "/\n") || entry.Reason != "safe reason" {
		t.Fatalf("unsanitized log entry: %#v", entry)
	}
}

func TestAppendLogRejectsSymlinkAndStaleLock(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, ".harnest"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, ".harnest", "state")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		err := appendLog(root, Options{}, Event{}, []Record{{Status: "passed"}}, 0)
		if err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("expected symlink error, got %v", err)
		}
	})
	t.Run("log symlink", func(t *testing.T) {
		root := t.TempDir()
		stateRoot, err := secureStateRoot(root)
		if err != nil {
			t.Fatal(err)
		}
		_ = stateRoot.Close()
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(outside, []byte("unchanged"), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, ".harnest", "state", "hooks.jsonl")
		if err := os.Symlink(outside, path); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		err = appendLog(root, Options{}, Event{}, []Record{{Status: "passed"}}, 0)
		if err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("expected symlink error, got %v", err)
		}
		data, readErr := os.ReadFile(outside)
		if readErr != nil || string(data) != "unchanged" {
			t.Fatalf("external target changed: %q, %v", data, readErr)
		}
	})
	t.Run("stale lock", func(t *testing.T) {
		root := t.TempDir()
		stateRoot, err := secureStateRoot(root)
		if err != nil {
			t.Fatal(err)
		}
		defer stateRoot.Close()
		if err := stateRoot.Mkdir("hooks.lock", 0o700); err != nil {
			t.Fatal(err)
		}
		err = appendLog(root, Options{}, Event{}, []Record{{Status: "passed"}}, 0)
		if err == nil || !strings.Contains(err.Error(), "busy or stale") {
			t.Fatalf("expected stale lock diagnostic, got %v", err)
		}
	})
}
