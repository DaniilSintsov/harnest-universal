package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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

func TestPlanApplyHooksPreservesForeignConfigAndIsIdempotent(t *testing.T) {
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
	owned := nativeEntry("codex", "Stop", root, "/usr/local/bin/harnest")
	handlers := owned["hooks"].([]any)
	owned["hooks"] = append(handlers, map[string]any{"type": "command", "command": "foreign"})
	doc := map[string]any{"hooks": map[string]any{"Stop": []any{owned}}}
	changed, err := mergeNativeHooks(doc, "codex", root, "/usr/local/bin/harnest", false, false, false)
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
	entry := nativeEntry("claude-code", "Stop", "/project", "/usr/local/bin/harnest")
	handler := entry["hooks"].([]any)[0].(map[string]any)
	handler["args"] = append(handler["args"].([]any), "--extra", "value")
	if _, _, err := ownedHandler(handler, "claude-code", "Stop"); err == nil {
		t.Fatal("extra ownership flags accepted")
	}
}

func TestPlanHooksRemovesOnlyOwnedBindings(t *testing.T) {
	dir := t.TempDir()
	project := hookProject()
	artifacts, err := PlanHooks(dir, project, "/usr/local/bin/harnest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyHooks(artifacts); err != nil {
		t.Fatal(err)
	}
	project.Hooks.Rules = nil
	remove, err := PlanHooks(dir, project, "/usr/local/bin/harnest")
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
	artifacts, err := PlanHooks(dir, hookProject(), "/usr/local/bin/harnest")
	if err != nil {
		t.Fatal(err)
	}
	first := artifacts[0]
	if err := os.MkdirAll(filepath.Dir(first.Path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first.Path, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyHooks(artifacts); err == nil || !strings.Contains(err.Error(), "changed after planning") {
		t.Fatalf("error = %v", err)
	}
}

func TestPlanHooksRejectsInlineCodexHooks(t *testing.T) {
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
