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
