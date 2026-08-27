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
		filepath.Join(".codex", "agents", "architect.toml"),
	} {
		path := filepath.Join(dir, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("portable target %s missing: %v", rel, err)
		}
		if !strings.Contains(string(data), "name: architect") && !strings.Contains(string(data), `name = "architect"`) {
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

func TestGenerateRejectsTargetsBeforeWriting(t *testing.T) {
	for _, targets := range [][]string{nil, {"claude-code", "unknown"}, {"codex", "codex"}} {
		dir := t.TempDir()
		cfg := &HarnestConfig{Version: CurrentVersion, Harnesses: targets}
		if _, err := Generate(dir, cfg); err == nil {
			t.Fatalf("Generate(%v) unexpectedly succeeded", targets)
		}
		for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Fatalf("Generate(%v) wrote %s: %v", targets, name, err)
			}
		}
	}
}

func TestGenerateRejectsMalformedAdapterBeforeMaterializingSkills(t *testing.T) {
	dir := t.TempDir()
	skill := filepath.Join(dir, ".agents", "skills", "review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("# Review\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("<!-- harnest-managed:start -->\nbroken\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &HarnestConfig{
		Version:   CurrentVersion,
		Harnesses: []string{"claude-code"},
		Skills:    ResourceBlock{Root: ".agents/skills"},
	}
	if _, err := Generate(dir, cfg); err == nil || !strings.Contains(err.Error(), "malformed harnest managed markers") {
		t.Fatalf("Generate() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills")); !os.IsNotExist(err) {
		t.Fatalf("preflight wrote skills: %v", err)
	}
}

func TestGenerateDryRunPreviewsSkillsAgentsAndProjectNameWithoutWrites(t *testing.T) {
	dir := t.TempDir()
	for path, content := range map[string]string{
		filepath.Join(dir, ".agents", "skills", "review", "SKILL.md"): "# Review\n",
		filepath.Join(dir, ".agents", "agents", "architect.md"):       "---\nname: architect\ndescription: portable\n---\nUse this role.\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &HarnestConfig{
		Version:   CurrentVersion,
		Project:   ProjectInfo{Name: "declared-project"},
		Harnesses: []string{"claude-code", "codex"},
		Skills:    ResourceBlock{Root: ".agents/skills"},
	}
	preview, err := GenerateDryRun(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(dir, ".claude", "skills", "review", "SKILL.md"),
		filepath.Join(dir, ".claude", "agents", "architect.md"),
		filepath.Join(dir, ".codex", "agents", "architect.toml"),
		filepath.Join(dir, "CLAUDE.md"),
		filepath.Join(dir, "AGENTS.md"),
	} {
		if !containsPath(preview.Files, path) {
			t.Fatalf("dry-run files miss %s: %v", path, preview.Files)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("dry-run wrote %s: %v", path, err)
		}
	}
	if !strings.Contains(preview.Adapters["claude-code"], "# declared-project") {
		t.Fatalf("dry-run lost project name: %s", preview.Adapters["claude-code"])
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
