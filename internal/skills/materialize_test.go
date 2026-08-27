package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeMirrorsClaudeAndLeavesCodexSourceNative(t *testing.T) {
	project := t.TempDir()
	source := filepath.Join(project, ".agents", "skills", "review")
	if err := os.MkdirAll(filepath.Join(source, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: review\ndescription: Review code\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "scripts", "check.sh"), []byte("exit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}

	paths, err := Materialize(project, ".agents/skills", []string{"claude-code", "codex"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 {
		t.Fatalf("paths = %v", paths)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "review", ownershipFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(source, ownershipFile)); !os.IsNotExist(err) {
		t.Fatalf("Codex-native source was mutated: %v", err)
	}
}

func TestMaterializeRejectsUserOwnedConflictBeforeWrites(t *testing.T) {
	project := t.TempDir()
	for _, name := range []string{"a", "b"} {
		dir := filepath.Join(project, ".agents", "skills", name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	conflict := filepath.Join(project, ".claude", "skills", "b")
	if err := os.MkdirAll(conflict, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conflict, "SKILL.md"), []byte("user"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Materialize(project, ".agents/skills", []string{"claude-code"}, false)
	if err == nil || !strings.Contains(err.Error(), "user-owned") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "a")); !os.IsNotExist(err) {
		t.Fatalf("preflight wrote before conflict: %v", err)
	}
}

func TestMaterializeDryRunDoesNotWrite(t *testing.T) {
	project := t.TempDir()
	source := filepath.Join(project, ".agents", "skills", "review")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("review"), 0600); err != nil {
		t.Fatal(err)
	}
	paths, err := Materialize(project, ".agents/skills", []string{"claude-code"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %v", paths)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote files: %v", err)
	}
}

func TestMaterializeDryRunPreviewsStaleManagedFile(t *testing.T) {
	project := t.TempDir()
	source := filepath.Join(project, ".agents", "skills", "review")
	target := filepath.Join(project, ".claude", "skills", "review")
	for _, dir := range []string{source, target} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("review"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, ownershipFile), []byte("managed"), 0600); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(target, "old.txt")
	if err := os.WriteFile(stale, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	paths, err := Materialize(project, ".agents/skills", []string{"claude-code"}, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, path := range paths {
		found = found || path == stale+" (remove)"
	}
	if !found {
		t.Fatalf("cleanup preview missing: %v", paths)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("dry-run removed stale file: %v", err)
	}
}
