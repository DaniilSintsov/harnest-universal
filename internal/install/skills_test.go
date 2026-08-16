package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallBundledSkills(t *testing.T) {
	dir := t.TempDir()
	if err := installBundledSkills(filepath.Join(dir, "skills")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"harnest-bootstrap", "architecture-context-builder", "project-rules-builder", "compliance-review"} {
		data, err := os.ReadFile(filepath.Join(dir, "skills", name, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "name: "+name) {
			t.Fatalf("invalid installed skill %s", name)
		}
	}
	for _, rel := range []string{
		"architecture-context-builder/references/ecc/LICENSE",
		"architecture-context-builder/references/ecc/SOURCES.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, "skills", rel)); err != nil {
			t.Fatalf("missing installed third-party notice %s: %v", rel, err)
		}
	}
}

func TestInstallBundledSkillsKeepsHashedBackup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "skills", "harnest-bootstrap", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	custom := []byte("custom skill\n")
	if err := os.WriteFile(target, custom, 0600); err != nil {
		t.Fatal(err)
	}
	if err := installBundledSkills(filepath.Join(dir, "skills")); err != nil {
		t.Fatal(err)
	}
	backupPath := target + ".pre-" + contentHash(custom)[:12] + ".bak"
	backup, err := os.ReadFile(backupPath)
	if err != nil || string(backup) != string(custom) {
		t.Fatalf("skill backup = %q, err = %v", backup, err)
	}
}
