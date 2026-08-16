package harness

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInstalledReturnsOnlyExistingHarnessDirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}

	got := Installed("claude-code", "codex")
	want := []string{"codex"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Installed() = %v, want %v", got, want)
	}
}

func TestRegistryContainsOnlySupportedHarnesses(t *testing.T) {
	want := []string{"claude-code", "codex"}
	if got := Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}

	if _, err := Get("cursor"); err == nil {
		t.Fatal("Get(cursor) succeeded; unsupported harness must be rejected")
	}
}

func TestGlobalSkillsDirUsesNativeHarnessPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	for name, want := range map[string]string{
		"claude-code": filepath.Join(home, ".claude", "skills"),
		"codex":       filepath.Join(home, ".agents", "skills"),
	} {
		got, err := GlobalSkillsDir(name)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("GlobalSkillsDir(%s) = %s, want %s", name, got, want)
		}
	}
}

func TestGlobalDirsRespectHarnessOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	claudeDir := filepath.Join(t.TempDir(), "Claude Config")
	codexDir := filepath.Join(t.TempDir(), "Codex Config")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	t.Setenv("CODEX_HOME", codexDir)

	for name, want := range map[string]string{"claude-code": claudeDir, "codex": codexDir} {
		got, err := GlobalDir(name)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("GlobalDir(%s) = %s, want %s", name, got, want)
		}
	}

	claudeSkills, err := GlobalSkillsDir("claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(claudeDir, "skills"); claudeSkills != want {
		t.Fatalf("Claude skills = %s, want %s", claudeSkills, want)
	}
	codexSkills, err := GlobalSkillsDir("codex")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".agents", "skills"); codexSkills != want {
		t.Fatalf("Codex skills = %s, want %s", codexSkills, want)
	}
}
