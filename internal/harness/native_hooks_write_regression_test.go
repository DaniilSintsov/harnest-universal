package harness

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestApplyHooksPreservesNativeConfigPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	for _, tc := range []struct {
		name   string
		target string
		path   string
		mode   os.FileMode
	}{
		{"private Claude config", "claude-code", ".claude/settings.local.json", 0600},
		{"shared Codex config", "codex", ".codex/hooks.json", 0640},
		{"new Claude config", "claude-code", ".claude/settings.local.json", 0},
		{"new Codex config", "codex", ".codex/hooks.json", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, filepath.FromSlash(tc.path))
			if tc.mode != 0 {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(`{"env":{"EXAMPLE_TOKEN":"private"}}`), tc.mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, tc.mode); err != nil {
					t.Fatal(err)
				}
			}
			project := hookProject()
			project.Targets = []string{tc.target}
			plan, err := PlanHooks(root, project, "/usr/local/bin/harnest")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ApplyHooks(plan); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.mode
			if want == 0 {
				want = 0600
			}
			if got := info.Mode().Perm(); got != want {
				t.Fatalf("mode = %04o, want %04o", got, want)
			}
		})
	}
}

func TestApplyHooksRollsBackOnLaterReadError(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "update", true: "remove"}[remove], func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, ".codex", "hooks.json")
			before := []byte("{\"env\":{\"EXAMPLE_TOKEN\":\"private\"}}\n")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, before, 0600); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			first := HookArtifact{Path: path, Before: before, Content: []byte("{}\n"), Mode: info.Mode().Perm(), Exists: true, Remove: remove}
			second := filepath.Join(root, "second.json")
			if err := os.Mkdir(second, 0700); err != nil {
				t.Fatal(err)
			}
			paths, err := ApplyHooks([]HookArtifact{first, {Path: second, Content: []byte("after")}})
			if err == nil || len(paths) != 0 {
				t.Fatalf("paths = %v, error = %v", paths, err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, before) {
				t.Fatalf("rollback content = %q, want %q", got, before)
			}
			if runtime.GOOS != "windows" {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if got := info.Mode().Perm(); got != 0600 {
					t.Fatalf("rollback mode = %04o, want 0600", got)
				}
			}
		})
	}
}

func TestApplyHooksRollsBackOnConcurrentPermissionChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	root := t.TempDir()
	first, second := filepath.Join(root, "first.json"), filepath.Join(root, "second.json")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := ApplyHooks([]HookArtifact{
		{Path: first, Before: []byte("before"), Content: []byte("after"), Exists: true, Mode: 0600},
		{Path: second, Before: []byte("before"), Content: []byte("after"), Exists: true, Mode: 0644},
	})
	if err == nil || !strings.Contains(err.Error(), "permissions changed after planning") || len(paths) != 0 {
		t.Fatalf("paths = %v, error = %v", paths, err)
	}
	for _, path := range []string{first, second} {
		content, err := os.ReadFile(path)
		if err != nil || string(content) != "before" {
			t.Fatalf("%s content = %q, error = %v", path, content, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0600 {
			t.Fatalf("%s mode = %04o, want 0600", path, got)
		}
	}
}

func TestApplyHooksReportsOnlyFailedRestorations(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires POSIX read permissions")
	}
	root := t.TempDir()
	first, second := filepath.Join(root, "first.json"), filepath.Join(root, "second.json")
	paths, err := ApplyHooks([]HookArtifact{
		{Path: first, Content: []byte("after"), Mode: 0600},
		{Path: second, Content: []byte("after"), Mode: 0200},
		{Path: root, Content: []byte("after")},
	})
	if err == nil || !strings.Contains(err.Error(), "rollback failed for "+second) {
		t.Fatalf("error = %v", err)
	}
	if len(paths) != 1 || paths[0] != second {
		t.Fatalf("unrestored paths = %v, want [%s]", paths, second)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("first artifact was not rolled back: %v", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("failed restoration was not reported accurately: %v", err)
	}
}
