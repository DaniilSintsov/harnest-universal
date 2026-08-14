package profile

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestListInUsesTargetBaseDirectory(t *testing.T) {
	baseDir := t.TempDir()
	profilesDir := filepath.Join(baseDir, "profiles")
	if err := os.Mkdir(profilesDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "codex-only.md"), []byte("profile"), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := ListIn(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"codex-only"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ListIn() = %v, want %v", got, want)
	}
}

func TestRemoveInOnlyRemovesFromTargetBaseDirectory(t *testing.T) {
	targetDir := t.TempDir()
	otherDir := t.TempDir()
	for _, dir := range []string{targetDir, otherDir} {
		if err := os.Mkdir(filepath.Join(dir, "profiles"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "profiles", "custom.md"), []byte("profile"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	if err := RemoveIn("custom", targetDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "profiles", "custom.md")); !os.IsNotExist(err) {
		t.Fatalf("target profile still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(otherDir, "profiles", "custom.md")); err != nil {
		t.Fatalf("other target profile changed: %v", err)
	}
}

func TestCreateInWritesToTargetBaseDirectory(t *testing.T) {
	baseDir := t.TempDir()
	input := bufio.NewReader(strings.NewReader("Research\nnone\nn\nworkflow\nTarget profile\n"))

	if err := CreateIn("custom", baseDir, input); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, "profiles", "custom.md")); err != nil {
		t.Fatalf("target profile missing: %v", err)
	}
}

func TestEditInOpensProfileFromTargetBaseDirectory(t *testing.T) {
	baseDir := t.TempDir()
	profilesDir := filepath.Join(baseDir, "profiles")
	if err := os.Mkdir(profilesDir, 0700); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(profilesDir, "custom.md")
	if err := os.WriteFile(profilePath, []byte("profile"), 0600); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "edited-path")
	editor := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nprintf '%s' \"$1\" > \"$CAPTURE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor)
	t.Setenv("CAPTURE", capture)

	if err := EditIn("custom", baseDir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != profilePath {
		t.Fatalf("editor opened %q, want %q", got, profilePath)
	}
}

func TestInstallToForUsesTargetContentAndModifiedCheck(t *testing.T) {
	for _, test := range []struct {
		harness   string
		want      string
		forbidden string
	}{
		{harness: "claude-code", want: "CLAUDE.md", forbidden: "AGENTS.md"},
		{harness: "codex", want: "AGENTS.md", forbidden: "CLAUDE.md"},
	} {
		t.Run(test.harness, func(t *testing.T) {
			baseDir := t.TempDir()
			if err := InstallToFor("business-feature", baseDir, test.harness); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(baseDir, "profiles", "business-feature.md")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			content := string(data)
			if !strings.Contains(content, test.want) || strings.Contains(content, test.forbidden) {
				t.Fatalf("unexpected %s profile:\n%s", test.harness, content)
			}
			modified, err := IsModifiedInFor("business-feature", baseDir, test.harness)
			if err != nil {
				t.Fatal(err)
			}
			if modified {
				t.Fatal("freshly installed profile reported as modified")
			}
		})
	}
}

func TestMigrateInForAdaptsCodexAndKeepsBackup(t *testing.T) {
	baseDir := t.TempDir()
	profilesDir := filepath.Join(baseDir, "profiles")
	if err := os.Mkdir(profilesDir, 0700); err != nil {
		t.Fatal(err)
	}
	original := "See CLAUDE.md. Use opus via Task tool.\n"
	path := filepath.Join(profilesDir, "business-feature.md")
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	changed, backupPath, err := MigrateInFor("business-feature", baseDir, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected migration")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "See AGENTS.md. Use sol via Codex subagent workflow.\n" {
		t.Fatalf("migrated profile = %q", got)
	}
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != original {
		t.Fatalf("backup = %q, want %q", backup, original)
	}
	changed, _, err = MigrateInFor("business-feature", baseDir, "codex")
	if err != nil || changed {
		t.Fatalf("second migration = %v, %v; want no-op", changed, err)
	}

	claudeDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(claudeDir, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "profiles", "business-feature.md"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	changed, _, err = MigrateInFor("business-feature", claudeDir, "claude-code")
	if err != nil || changed {
		t.Fatalf("Claude migration = %v, %v; want no-op", changed, err)
	}
}

func TestRepairBuiltinMetaInAddsMissingMetaAndBackup(t *testing.T) {
	baseDir := t.TempDir()
	profilesDir := filepath.Join(baseDir, "profiles")
	if err := os.Mkdir(profilesDir, 0700); err != nil {
		t.Fatal(err)
	}

	original := strings.Join([]string{
		"# Profile: Bug Hunting",
		"",
		"## Workflow (STRICT)",
		"",
		"Custom body stays here.",
		"",
	}, "\n")
	path := filepath.Join(profilesDir, "bug-hunting.md")
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	changed, backupPath, err := RepairBuiltinMetaIn("bug-hunting", baseDir)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected repair to change file")
	}
	if backupPath != path+".bak" {
		t.Fatalf("backup path = %q, want %q", backupPath, path+".bak")
	}

	repaired, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(repaired)
	for _, want := range []string{
		"# Profile: Bug Hunting",
		"## Meta",
		"**Keywords:** баг, ошибка, краш",
		"**Description:** Баг, регрессия, краш, неожиданное поведение",
		"## Workflow (STRICT)",
		"Custom body stays here.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("repaired profile missing %q:\n%s", want, text)
		}
	}

	backup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != original {
		t.Fatalf("backup = %q, want %q", backup, original)
	}
}

func TestRepairBuiltinMetaInNoOpWhenMetaExists(t *testing.T) {
	baseDir := t.TempDir()
	profilesDir := filepath.Join(baseDir, "profiles")
	if err := os.Mkdir(profilesDir, 0700); err != nil {
		t.Fatal(err)
	}

	content, ok := BuiltinContent("research")
	if !ok {
		t.Fatal("builtin research profile missing")
	}
	path := filepath.Join(profilesDir, "research.md")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	changed, backupPath, err := RepairBuiltinMetaIn("research", baseDir)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no-op when Meta already exists")
	}
	if backupPath != "" {
		t.Fatalf("unexpected backup path: %q", backupPath)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("unexpected backup file: %v", err)
	}
}
