package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func installFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPlanHooksRejectsDeletedTrackedConfig(t *testing.T) {
	requireNativeHookInstallation(t)
	for _, path := range []string{".codex/hooks.json", ".claude/settings.local.json"} {
		t.Run(path, func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "init", "--quiet")
			installFile(t, filepath.Join(dir, path), "{}")
			runGit(t, dir, "add", "-f", "--", path)
			if err := os.Remove(filepath.Join(dir, path)); err != nil {
				t.Fatal(err)
			}
			artifacts, err := PlanHooks(dir, hookProject(), "/usr/local/bin/harnest")
			if err == nil || !strings.Contains(err.Error(), "tracked") || len(artifacts) != 0 {
				t.Fatalf("artifacts=%d, error=%v; want tracked conflict", len(artifacts), err)
			}
		})
	}
}

func TestPlanHooksRejectsIneffectiveIgnore(t *testing.T) {
	requireNativeHookInstallation(t)
	for _, tc := range []struct{ name, path, content string }{
		{"config", ".codex/.gitignore", "!hooks.json\n"},
		{"state", ".harnest/.gitignore", "!state/\n"},
		{"existing-exclude", ".git/info/exclude", "/.codex/hooks.json\n/.claude/settings.local.json\n/.harnest/state/\n!/.codex/hooks.json\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "init", "--quiet")
			installFile(t, filepath.Join(dir, tc.path), tc.content)
			before, err := os.ReadFile(filepath.Join(dir, ".git/info/exclude"))
			if err != nil {
				t.Fatal(err)
			}
			artifacts, err := PlanHooks(dir, hookProject(), "/usr/local/bin/harnest")
			if err == nil || !strings.Contains(err.Error(), "ignor") || len(artifacts) != 0 {
				t.Fatalf("artifacts=%d, error=%v; want ignore conflict", len(artifacts), err)
			}
			after, err := os.ReadFile(filepath.Join(dir, ".git/info/exclude"))
			if err != nil || string(before) != string(after) {
				t.Fatalf("planning changed exclude: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, ".claude/settings.local.json")); !os.IsNotExist(err) {
				t.Fatalf("partial config installed: %v", err)
			}
		})
	}
}

func TestPlanHooksChecksProspectiveExcludeAndRepoCaseSetting(t *testing.T) {
	requireNativeHookInstallation(t)
	for _, ignoreCase := range []string{"true", "false"} {
		t.Run(ignoreCase, func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "init", "--quiet")
			runGit(t, dir, "config", "core.ignoreCase", ignoreCase)
			installFile(t, filepath.Join(dir, ".codex/.gitignore"), "!HOOKS.JSON\n")
			installFile(t, filepath.Join(dir, ".git/info/exclude"), "!/.codex/hooks.json\n")
			artifacts, err := PlanHooks(dir, hookProject(), "/usr/local/bin/harnest")
			if ignoreCase == "true" {
				if err == nil || !strings.Contains(err.Error(), "ignored") {
					t.Fatalf("case-insensitive ignore conflict error=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ApplyHooks(artifacts); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{".codex/hooks.json", ".claude/settings.local.json", ".harnest/state/hooks.jsonl"} {
				runGit(t, dir, "check-ignore", "--quiet", "--", path)
			}
			artifacts, err = PlanHooks(dir, hookProject(), "/usr/local/bin/harnest")
			if err != nil || len(artifacts) != 0 {
				t.Fatalf("idempotent plan artifacts=%d error=%v", len(artifacts), err)
			}
		})
	}
}

func TestPlanHooksEmptySelectionHandlesSymlinks(t *testing.T) {
	for _, directory := range []bool{false, true} {
		for _, owned := range []bool{false, true} {
			name := "file"
			if directory {
				name = "directory"
			}
			if owned {
				name += "-owned"
			}
			t.Run(name, func(t *testing.T) {
				dir, outside := t.TempDir(), t.TempDir()
				root, err := filepath.EvalSymlinks(dir)
				if err != nil {
					t.Fatal(err)
				}
				content := []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"foreign"}]}]}}`)
				if owned {
					content, err = json.Marshal(map[string]any{"hooks": map[string]any{"Stop": []any{nativeEntry("codex", "Stop", root, filepath.Join(root, "harnest"), "")}}})
					if err != nil {
						t.Fatal(err)
					}
				}
				external := filepath.Join(outside, "hooks.json")
				installFile(t, external, string(content))
				link, target := filepath.Join(dir, ".codex"), outside
				if !directory {
					if err := os.Mkdir(link, 0755); err != nil {
						t.Fatal(err)
					}
					link, target = filepath.Join(link, "hooks.json"), external
				}
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}
				project := hookProject()
				project.Hooks.Rules = nil
				project.Targets = []string{"claude-code"}
				artifacts, err := PlanHooks(dir, project, "/usr/local/bin/harnest")
				if owned {
					if err == nil || !strings.Contains(err.Error(), "symlink") {
						t.Fatalf("owned symlink error=%v", err)
					}
				} else if err != nil || len(artifacts) != 0 {
					t.Fatalf("foreign symlink artifacts=%v error=%v", artifacts, err)
				}
			})
		}
	}
}
