package yaml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateMaterializesCallablePortableAgentForEverySelectedTarget(t *testing.T) {
	dir := t.TempDir()
	source := "---\nname: architect\ndescription: portable\n---\nUse this role.\n"
	sourcePath := filepath.Join(dir, ".agents", "agents", "source.md")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &HarnestConfig{
		Version:   CurrentVersion,
		Harnesses: []string{"claude-code", "codex"},
		Agents: AgentsBlock{
			Consilium: map[string]string{"architect": "architect"},
		},
		Settings: SettingsBlock{Language: "ru"},
	}
	files, err := Generate(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		filepath.Join(".claude", "agents", "architect.md"),
		filepath.Join(".codex", "agents", "architect.md"),
	} {
		path := filepath.Join(dir, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("portable target %s missing: %v", rel, err)
		}
		if !strings.HasPrefix(string(data), "---\nname: architect\n") {
			t.Fatalf("portable target %s is not callable: %q", rel, data)
		}
		if !containsPath(files, path) {
			t.Fatalf("generated file list misses portable target %s: %v", rel, files)
		}
	}
	for _, instruction := range []string{"CLAUDE.md", "AGENTS.md"} {
		data, err := os.ReadFile(filepath.Join(dir, instruction))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "portable:architect") || !strings.Contains(string(data), "architect") {
			t.Fatalf("%s contains non-callable assignment: %s", instruction, data)
		}
	}
}

func containsPath(paths []string, wanted string) bool {
	for _, path := range paths {
		if path == wanted {
			return true
		}
	}
	return false
}
