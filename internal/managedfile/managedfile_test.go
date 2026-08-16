package managedfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUpsertPreservesUserContentAndMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("user content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Upsert(path, "harnest", "generated content"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "user content") || !strings.Contains(content, "generated content") {
		t.Fatalf("content was not preserved: %q", content)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0644 {
		t.Fatalf("mode = %o, want 644", got)
	}

	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
}

func TestUpsertRejectsUnpairedMarkersWithoutMutation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	original := "user content\n<!-- harnest-managed:start -->\nbroken\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	err := Upsert(path, "harnest", "generated content")
	if err == nil {
		t.Fatal("expected malformed marker error")
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := string(data); got != original {
		t.Fatalf("file mutated after error: %q", got)
	}
}

func TestUpsertReplacesOnlyManagedBlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	original := "before\n<!-- harnest-managed:start -->\nold\n<!-- harnest-managed:end -->\nafter\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	if err := Upsert(path, "harnest", "new"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "old") || !strings.Contains(got, "before") || !strings.Contains(got, "new") || !strings.Contains(got, "after") {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestUpsertNoOpPreservesBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(path, []byte("user text\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertWithMode(path, "harnest", "workflow", 0644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertWithMode(path, "harnest", "workflow", 0644); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != "user text\n" {
		t.Fatalf("no-op install replaced backup: %q", backup)
	}
}

func TestWriteAtomicReplacesExistingFileInPortablePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config with spaces", "юникод")
	path := filepath.Join(dir, "settings.md")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("content = %q, want new", data)
	}
}
