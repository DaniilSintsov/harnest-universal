package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/daniilsintsov/harnest-universal/internal/ir"
	"github.com/daniilsintsov/harnest-universal/internal/rules"
)

func hookProject() ir.Project {
	return ir.Project{
		Hooks:   ir.Hooks{Enabled: true, Rules: []string{"protect"}},
		Targets: []string{"claude-code", "codex"},
		PolicyRules: []rules.Rule{{
			ID: "protect", Severity: rules.Hard, Statement: "protect files",
			Enforcement: []rules.Enforcement{{Type: "protect-path", Paths: []string{"config/**"}}},
		}},
	}
}

func requireNativeHookInstallation(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("native hook installation is intentionally unverified on Windows")
	}
}

func TestPlanHooksRejectsNativeInstallationOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows installation guard")
	}
	artifacts, err := PlanHooks(t.TempDir(), hookProject(), "")
	if err == nil || !strings.Contains(err.Error(), "not verified on Windows") || len(artifacts) != 0 {
		t.Fatalf("artifacts=%d error=%v; want Windows installation rejection", len(artifacts), err)
	}
}

func TestPlanApplyHooksPreservesForeignConfigAndIsIdempotent(t *testing.T) {
	requireNativeHookInstallation(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	foreign := `{"permissions":{"allow":["x"]},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"foreign"}]}]}}`
	if err := os.WriteFile(path, []byte(foreign), 0644); err != nil {
		t.Fatal(err)
	}

	artifacts, err := PlanHooks(dir, hookProject(), "/usr/local/bin/harnest")
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 3 {
		t.Fatalf("artifacts = %d, want 3", len(artifacts))
	}
	if _, err := ApplyHooks(artifacts); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["permissions"] == nil || !strings.Contains(string(data), "foreign") {
		t.Fatal("foreign config was not preserved")
	}
	second, err := PlanHooks(dir, hookProject(), "/usr/local/bin/harnest")
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("idempotent plan has %d artifacts", len(second))
	}
}

func TestDisabledHooksKeepNativeWiring(t *testing.T) {
	requireNativeHookInstallation(t)
	dir := t.TempDir()
	project := hookProject()
	artifacts, err := PlanHooks(dir, project, "/usr/local/bin/harnest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyHooks(artifacts); err != nil {
		t.Fatal(err)
	}
	project.Hooks.Enabled = false
	again, err := PlanHooks(dir, project, "/usr/local/bin/harnest")
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("local disable planned %d wiring changes", len(again))
	}
}

func TestMergeNativeHooksPreservesForeignSiblingHandler(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "harnest")
	owned := nativeEntry("codex", "Stop", root, executable, "")
	handlers := owned["hooks"].([]any)
	owned["hooks"] = append(handlers, map[string]any{"type": "command", "command": "foreign"})
	doc := map[string]any{"hooks": map[string]any{"Stop": []any{owned}}}
	changed, err := mergeNativeHooks(doc, "codex", root, executable, "", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("owned handler was not removed")
	}
	data, _ := json.Marshal(doc)
	if !strings.Contains(string(data), "foreign") || strings.Contains(string(data), nativeHookStatus) {
		t.Fatalf("merged config = %s", data)
	}
}

func TestOwnedHandlerRejectsExtraFlags(t *testing.T) {
	root := t.TempDir()
	entry := nativeEntry("claude-code", "Stop", root, filepath.Join(root, "harnest"), "")
	handler := entry["hooks"].([]any)[0].(map[string]any)
	if owned, _, err := ownedHandler(handler, "claude-code", "Stop"); err != nil || !owned {
		t.Fatalf("valid handler rejected: owned=%v error=%v", owned, err)
	}
	handler["command"] = handler["command"].(string) + " '--extra' 'value'"
	if _, _, err := ownedHandler(handler, "claude-code", "Stop"); err == nil {
		t.Fatal("extra ownership flags accepted")
	}
}

func TestNativeEntryCommandExecutesQuotedArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell invocation")
	}
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell unavailable")
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "harnest's binary")
	root := filepath.Join(dir, "project's $(printf wrong) directory")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"claude-code", "codex"} {
		for _, event := range []string{"PreToolUse", "Stop"} {
			t.Run(platform+"/"+event, func(t *testing.T) {
				entry := nativeEntry(platform, event, root, executable, "")
				handler := entry["hooks"].([]any)[0].(map[string]any)
				output, err := exec.Command(shell, "-c", handler["command"].(string)).CombinedOutput()
				if err != nil {
					t.Fatalf("command failed: %v: %s", err, output)
				}
				eventArg := "stop"
				if event == "PreToolUse" {
					eventArg = "pre-tool-use"
				}
				want := strings.Join([]string{"hook", "evaluate", "--platform", platform, "--event", eventArg, "--project", root}, "\n") + "\n"
				if string(output) != want {
					t.Fatalf("command arguments = %q, want %q", output, want)
				}
				if _, exists := handler["args"]; exists {
					t.Fatal("command hook contains sibling args instead of a complete shell invocation")
				}
				if owned, gotRoot, err := ownedHandler(handler, platform, event); err != nil || !owned || gotRoot != root {
					t.Fatalf("ownership = %v, %q, %v", owned, gotRoot, err)
				}
			})
		}
	}
}

func TestMergeNativeHooksMigratesLegacyClaudeCommand(t *testing.T) {
	dir := t.TempDir()
	root, executable := filepath.Join(dir, "project's directory"), filepath.Join(dir, "harnest's binary")
	handler := map[string]any{
		"type": "command", "statusMessage": nativeHookStatus, "command": executable,
		"args": []any{"hook", "evaluate", "--platform", "claude-code", "--event", "stop", "--project", root},
	}
	doc := map[string]any{"hooks": map[string]any{"Stop": []any{map[string]any{"hooks": []any{handler}}}}}
	changed, err := mergeNativeHooks(doc, "claude-code", root, executable, "", true, false, true)
	if err != nil || !changed {
		t.Fatalf("migration changed=%v error=%v", changed, err)
	}
	entries := doc["hooks"].(map[string]any)["Stop"].([]any)
	if len(entries) != 1 {
		t.Fatalf("migration left %d entries", len(entries))
	}
	migrated := entries[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if _, exists := migrated["args"]; exists {
		t.Fatal("migration retained legacy args")
	}
	if changed, err := mergeNativeHooks(doc, "claude-code", root, executable, "", true, false, true); err != nil || changed {
		t.Fatalf("second merge changed=%v error=%v", changed, err)
	}
}

func TestPlanHooksRemovesOnlyOwnedBindings(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project := hookProject()
	executable := filepath.Join(dir, "harnest")
	for platform, path := range map[string]string{"claude-code": ".claude/settings.local.json", "codex": ".codex/hooks.json"} {
		data, err := json.Marshal(map[string]any{"hooks": map[string]any{
			"PreToolUse": []any{nativeEntry(platform, "PreToolUse", dir, executable, "")},
			"Stop":       []any{nativeEntry(platform, "Stop", dir, executable, "")},
		}})
		if err != nil {
			t.Fatal(err)
		}
		installFile(t, filepath.Join(dir, path), string(data))
	}
	project.Hooks.Rules = nil
	remove, err := PlanHooks(dir, project, executable)
	if err != nil {
		t.Fatal(err)
	}
	if len(remove) != 2 {
		t.Fatalf("removal artifacts = %d, want 2", len(remove))
	}
	if _, err := ApplyHooks(remove); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, ".claude", "settings.local.json"), filepath.Join(dir, ".codex", "hooks.json")} {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if err == nil && strings.Contains(string(data), nativeHookStatus) {
			t.Fatalf("owned hook remains in %s", path)
		}
	}
}

func TestApplyHooksRejectsConcurrentChange(t *testing.T) {
	dir := t.TempDir()
	first := HookArtifact{Path: filepath.Join(dir, ".codex", "hooks.json"), Content: []byte("{}\n"), Mode: 0600}
	if err := os.MkdirAll(filepath.Dir(first.Path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first.Path, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyHooks([]HookArtifact{first}); err == nil || !strings.Contains(err.Error(), "changed after planning") {
		t.Fatalf("error = %v", err)
	}
}

func TestPlanHooksRejectsInlineCodexHooks(t *testing.T) {
	requireNativeHookInstallation(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[hooks]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := PlanHooks(dir, hookProject(), "/usr/local/bin/harnest")
	if err == nil || !strings.Contains(err.Error(), "inline hooks") {
		t.Fatalf("error = %v", err)
	}
}

func TestPlanHooksEmptySelectionLeavesForeignConfigUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"foreign"}]}]}}`), 0644); err != nil {
		t.Fatal(err)
	}
	project := hookProject()
	project.Hooks.Rules = nil
	artifacts, err := PlanHooks(dir, project, "/usr/local/bin/harnest")
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("empty selection planned %d changes", len(artifacts))
	}
}

func TestPlanHooksEmptySelectionIgnoresInvalidForeignJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	project := hookProject()
	project.Hooks.Rules = nil
	if artifacts, err := PlanHooks(dir, project, "/usr/local/bin/harnest"); err != nil || len(artifacts) != 0 {
		t.Fatalf("artifacts=%d error=%v", len(artifacts), err)
	}
}

func TestPlanHooksRejectsSymlinkedConfigDirectory(t *testing.T) {
	requireNativeHookInstallation(t)
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, ".codex")); err != nil {
		t.Fatal(err)
	}
	project := hookProject()
	project.Targets = []string{"codex"}
	_, err := PlanHooks(dir, project, "/usr/local/bin/harnest")
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v", err)
	}
}

func TestHookArtifactsAreIgnoredAcrossLinkedWorktree(t *testing.T) {
	requireNativeHookInstallation(t)
	base := t.TempDir()
	main := filepath.Join(base, "main")
	linked := filepath.Join(base, "linked")
	if err := os.Mkdir(main, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, main, "init")
	runGit(t, main, "config", "user.email", "test@example.com")
	runGit(t, main, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(main, "README.md"), []byte("test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, main, "add", "README.md")
	runGit(t, main, "commit", "-m", "initial")
	runGit(t, main, "worktree", "add", "-b", "linked", linked)
	artifacts, err := PlanHooks(linked, hookProject(), "/usr/local/bin/harnest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyHooks(artifacts); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ root, path string }{{main, ".claude/settings.local.json"}, {linked, ".codex/hooks.json"}, {linked, ".harnest/state/hooks.jsonl"}} {
		cmd := exec.Command("git", "-C", item.root, "check-ignore", "-q", "--no-index", item.path)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s not ignored: %v: %s", item.path, err, output)
		}
	}
}

func TestHookArtifactsUseCheckoutAnchoredSubprojectPatterns(t *testing.T) {
	requireNativeHookInstallation(t)
	repo := t.TempDir()
	runGit(t, repo, "init")
	sub := filepath.Join(repo, "folder [one]")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	project := hookProject()
	project.Targets = []string{"codex"}
	artifacts, err := PlanHooks(sub, project, "/usr/local/bin/harnest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyHooks(artifacts); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"folder [one]/.codex/hooks.json", "folder [one]/.harnest/state/hooks.jsonl"} {
		cmd := exec.Command("git", "-C", repo, "check-ignore", "-q", "--no-index", path)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s not ignored: %v: %s", path, err, output)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := append([]string{"-C", dir}, args...)
	if output, err := exec.Command("git", command...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
