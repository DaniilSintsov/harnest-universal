package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/daniilsintsov/harnest-universal/internal/checks"
)

func TestEvaluatePreToolUseContracts(t *testing.T) {
	root := hookFixture(t, []string{"protect"}, "", `id: protect
severity: hard
statement: protect production
scope:
  paths: ["config/**"]
  operations: [change]
enforcement:
  - type: protect-path
    paths: ["config/production/**"]
`)
	tests := []struct {
		name, platform, tool, input, want string
	}{
		{"allowed", "claude-code", "Edit", `{"file_path":"config/dev/app.yaml"}`, "not-applicable"},
		{"protected", "claude-code", "Write", `{"file_path":"config/production/app.yaml"}`, "violation"},
		{"notebook", "claude-code", "NotebookEdit", `{"notebook_path":"config/production/book.ipynb"}`, "violation"},
		{"multi patch", "codex", "apply_patch", `{"command":"*** Begin Patch\n*** Add File: ok.go\n+x\n*** Update File: config/production/app.yaml\n@@\n-old\n+new\n*** End Patch"}`, "violation"},
		{"move destination", "codex", "apply_patch", `{"command":"*** Begin Patch\n*** Update File: config/dev/app.yaml\n*** Move to: config/production/app.yaml\n@@\n-old\n+new\n*** End Patch"}`, "violation"},
		{"unknown tool", "codex", "Write", `{}`, "not-applicable"},
		{"malformed supported", "codex", "apply_patch", `{"command":"bad patch"}`, "evaluation-error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{"hook_event_name":"PreToolUse","cwd":%q,"tool_name":%q,"tool_input":%s,"session_id":"secret-session"}`, root, test.tool, test.input)
			got := Evaluate(context.Background(), Options{Platform: test.platform, Event: "pre-tool-use", Project: root}, strings.NewReader(payload))
			if got.Status != test.want {
				t.Fatalf("status = %q (%s), want %q", got.Status, got.Reason, test.want)
			}
			encoded, _ := json.Marshal(got.Output)
			if strings.Contains(string(encoded), "secret-session") || strings.Contains(string(encoded), "bad patch") {
				t.Fatalf("native output leaked raw payload: %s", encoded)
			}
		})
	}
}

func TestInvalidBindingStillDeniesActualPreToolUse(t *testing.T) {
	root := t.TempDir()
	payload := fmt.Sprintf(`{"hook_event_name":"PreToolUse","cwd":%q,"tool_name":"Edit","tool_input":{"file_path":"protected"}}`, root)
	for _, event := range []string{"typo", "stop"} {
		result := Evaluate(context.Background(), Options{Platform: "codex", Event: event, Project: root}, strings.NewReader(payload))
		output, ok := result.Output["hookSpecificOutput"].(map[string]any)
		if result.Status != "evaluation-error" || !ok || output["permissionDecision"] != "deny" {
			t.Fatalf("invalid binding %q failed open: %#v", event, result)
		}
		if runtime.GOOS == "windows" && !strings.Contains(result.Output["systemMessage"].(string), "history could not be written") {
			t.Fatalf("unsupported log writer was not reported: %#v", result.Output)
		}
	}
	if runtime.GOOS == "windows" {
		return // The Windows log writer deliberately rejects native hook history.
	}
	log, err := os.ReadFile(filepath.Join(root, ".harnest", "state", "hooks.jsonl"))
	if err != nil || !strings.Contains(string(log), `"event":"pre-tool-use"`) || strings.Contains(string(log), `"event":"stop"`) {
		t.Fatalf("mismatched binding did not log actual event: %s, %v", log, err)
	}
}

func TestParentHookDoesNotRunInNestedWorktree(t *testing.T) {
	root := hookFixture(t, []string{"protect"}, "", "id: protect\nseverity: hard\nstatement: protect\nenforcement:\n  - type: protect-path\n    paths: ['**']\n")
	initGit(t, root)
	for _, args := range [][]string{
		{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "baseline"},
		{"worktree", "add", "--detach", filepath.Join(root, "nested"), "HEAD"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v: %s", err, output)
		}
	}
	payload := fmt.Sprintf(`{"hook_event_name":"PreToolUse","cwd":%q,"tool_name":"Edit","tool_input":{"file_path":"protected"}}`, filepath.Join(root, "nested"))
	result := Evaluate(context.Background(), Options{Platform: "claude-code", Event: "pre-tool-use", Project: root}, strings.NewReader(payload))
	if result.Status != "not-applicable" || len(result.Output) != 0 {
		t.Fatalf("parent hook activated in nested worktree: %#v", result)
	}
}

func TestEvaluateDisabledAndWrongRoot(t *testing.T) {
	root := hookFixture(t, []string{"protect"}, "", `id: protect
severity: hard
statement: protect
enforcement:
  - type: protect-path
    paths: ["**"]
`)
	if err := os.WriteFile(filepath.Join(root, ".harnest-local.yaml"), []byte("hooks:\n  enabled: false\n"), 0644); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"hook_event_name":"PreToolUse","cwd":%q,"tool_name":"Edit","tool_input":{"file_path":"x"}}`, root)
	got := Evaluate(context.Background(), Options{Platform: "claude-code", Event: "pre-tool-use", Project: root}, strings.NewReader(payload))
	if got.Status != "disabled" {
		t.Fatalf("disabled status = %q: %s", got.Status, got.Reason)
	}
	if err := os.WriteFile(filepath.Join(root, ".harnest-local.yaml"), []byte("hooks: [\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got = Evaluate(context.Background(), Options{Platform: "claude-code", Event: "pre-tool-use", Project: root}, strings.NewReader(payload))
	if got.Status != "evaluation-error" {
		t.Fatalf("invalid local status = %q: %s", got.Status, got.Reason)
	}

	other := t.TempDir()
	payload = fmt.Sprintf(`{"hook_event_name":"PreToolUse","cwd":%q,"tool_name":"Edit","tool_input":{"file_path":"x"}}`, other)
	got = Evaluate(context.Background(), Options{Platform: "claude-code", Event: "pre-tool-use", Project: root}, strings.NewReader(payload))
	if got.Status != "not-applicable" {
		t.Fatalf("wrong-root status = %q: %s", got.Status, got.Reason)
	}
}

func TestEvaluatePreToolUseRequiresSamePathIntersection(t *testing.T) {
	root := hookFixture(t, []string{"protect"}, "", `id: protect
severity: hard
statement: protect
scope:
  paths: ["src/**"]
enforcement:
  - type: protect-path
    paths: ["config/protected/**"]
`)
	patch := "*** Begin Patch\n*** Add File: src/open.go\n+x\n*** Add File: config/protected/other.go\n+y\n*** End Patch"
	payload := fmt.Sprintf(`{"hook_event_name":"PreToolUse","cwd":%q,"tool_name":"apply_patch","tool_input":{"command":%q}}`, root, patch)
	got := Evaluate(context.Background(), Options{Platform: "codex", Event: "pre-tool-use", Project: root}, strings.NewReader(payload))
	if got.Status != "not-applicable" {
		t.Fatalf("cross-file intersection matched: %#v", got)
	}
}

func TestEvaluateRejectsNestedGitCheckout(t *testing.T) {
	root := hookFixture(t, []string{"protect"}, "", `id: protect
severity: hard
statement: protect
enforcement:
  - type: protect-path
    paths: ["**"]
`)
	initGit(t, root)
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0755); err != nil {
		t.Fatal(err)
	}
	initGit(t, nested)
	payload := fmt.Sprintf(`{"hook_event_name":"PreToolUse","cwd":%q,"tool_name":"Edit","tool_input":{"file_path":"x"}}`, nested)
	got := Evaluate(context.Background(), Options{Platform: "claude-code", Event: "pre-tool-use", Project: root}, strings.NewReader(payload))
	if got.Status != "not-applicable" {
		t.Fatalf("nested checkout status = %q: %s", got.Status, got.Reason)
	}
}

func TestEvaluateStopDeduplicatesCheckAndLimitsRepeat(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "runs")
	check := fmt.Sprintf("id: shared\ncommand: %q\nargs: [\"-test.run=TestHookCheckProcess\", \"--\", \"pass\", %q]\napproved: true\ntimeout_seconds: 5\n", os.Args[0], marker)
	rules := `id: first
severity: required
statement: test
scope:
  paths: ["*.go"]
enforcement:
  - type: require-check
    check: shared
---RULE---
id: second
severity: required
statement: test again
scope:
  paths: ["*.go"]
enforcement:
  - type: require-check
    check: shared
`
	root := hookFixture(t, []string{"first", "second"}, check, rules)
	options := approvedStopOptions(t, root)
	initGit(t, root)
	if err := os.WriteFile(filepath.Join(root, "changed.go"), []byte("package changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"hook_event_name":"Stop","cwd":%q,"stop_hook_active":false}`, root)
	got := Evaluate(context.Background(), options, strings.NewReader(payload))
	if got.Status != "passed" || len(got.Records) != 1 || len(got.Records[0].RuleIDs) != 2 {
		t.Fatalf("dedup result = %#v", got)
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "run\n" {
		t.Fatalf("helper executions = %q, %v", data, err)
	}

	failCheck := fmt.Sprintf("id: shared\ncommand: %q\nargs: [\"-test.run=TestHookCheckProcess\", \"--\", \"fail\", %q]\napproved: true\n", os.Args[0], marker)
	if err := os.WriteFile(filepath.Join(root, ".harnest/checks/shared.yaml"), []byte(failCheck), 0644); err != nil {
		t.Fatal(err)
	}
	payload = fmt.Sprintf(`{"hook_event_name":"Stop","cwd":%q,"stop_hook_active":true}`, root)
	got = Evaluate(context.Background(), approvedStopOptions(t, root), strings.NewReader(payload))
	if got.Status != "violation" || got.Output["decision"] != nil {
		t.Fatalf("repeated failure = %#v", got)
	}
}

func TestEvaluateStopMissingApprovalAndNonGit(t *testing.T) {
	check := "id: shared\ncommand: true\napproved: false\n"
	rule := `id: required
severity: required
statement: test
scope:
  paths: ["*.go"]
enforcement:
  - type: require-check
    check: shared
`
	root := hookFixture(t, []string{"required"}, check, rule)
	payload := fmt.Sprintf(`{"hook_event_name":"Stop","cwd":%q,"stop_hook_active":false}`, root)
	got := Evaluate(context.Background(), Options{Platform: "codex", Event: "stop", Project: root}, strings.NewReader(payload))
	if got.Status != "not-verified" || got.Output["decision"] != "block" {
		t.Fatalf("non-git result = %#v", got)
	}
	initGit(t, root)
	if err := os.WriteFile(filepath.Join(root, "changed.go"), []byte("package changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got = Evaluate(context.Background(), Options{Platform: "codex", Event: "stop", Project: root}, strings.NewReader(payload))
	if got.Status != "evaluation-error" {
		t.Fatalf("unapproved result = %#v", got)
	}
}

func TestEvaluateStopTimeoutIsEvaluationError(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "runs")
	check := fmt.Sprintf("id: shared\ncommand: %q\nargs: [\"-test.run=TestHookCheckProcess\", \"--\", \"sleep\", %q]\napproved: true\ntimeout_seconds: 1\n", os.Args[0], marker)
	rule := `id: required
severity: required
statement: test
scope:
  paths: ["*.go"]
enforcement:
  - type: require-check
    check: shared
`
	root := hookFixture(t, []string{"required"}, check, rule)
	initGit(t, root)
	if err := os.WriteFile(filepath.Join(root, "changed.go"), []byte("package changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"hook_event_name":"Stop","cwd":%q,"stop_hook_active":false}`, root)
	got := Evaluate(context.Background(), approvedStopOptions(t, root), strings.NewReader(payload))
	if got.Status != "evaluation-error" || got.Output["decision"] != "block" {
		t.Fatalf("timeout result = %#v", got)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("approved check never started: %v", err)
	}
}

func approvedStopOptions(t *testing.T, root string) Options {
	t.Helper()
	check, err := checks.Load(root, ".harnest/checks", "shared")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := checks.Digest(root, []checks.Check{check})
	if err != nil {
		t.Fatal(err)
	}
	return Options{Platform: "codex", Event: "stop", Project: root, ChecksDigest: digest}
}

func TestHookCheckProcess(t *testing.T) {
	args := os.Args
	if len(args) < 3 || args[len(args)-3] != "--" {
		return
	}
	mode, marker := args[len(args)-2], args[len(args)-1]
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		os.Exit(3)
	}
	_, _ = f.WriteString("run\n")
	_ = f.Close()
	if mode == "sleep" {
		time.Sleep(3 * time.Second)
	}
	if mode == "fail" {
		os.Exit(1)
	}
}

func hookFixture(t *testing.T, ids []string, check, ruleData string) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{".harnest/rules", ".harnest/checks"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	config := fmt.Sprintf("version: 2\nrules:\n  root: .harnest/rules\nchecks:\n  root: .harnest/checks\nhooks:\n  rules: [%s]\nagents:\n  consilium: {}\n  executing: []\nharnesses: [claude-code, codex]\n", strings.Join(ids, ", "))
	if err := os.WriteFile(filepath.Join(root, "harnest.yaml"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	for i, data := range strings.Split(ruleData, "---RULE---") {
		if err := os.WriteFile(filepath.Join(root, ".harnest/rules", fmt.Sprintf("rule-%d.yaml", i)), []byte(strings.TrimSpace(data)+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if check != "" {
		if err := os.WriteFile(filepath.Join(root, ".harnest/checks/shared.yaml"), []byte(check), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func initGit(t *testing.T, root string) {
	t.Helper()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v: %s", err, output)
	}
}
