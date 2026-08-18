package converter

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	harnestYaml "github.com/daniilsintsov/harnest-universal/internal/yaml"
)

func TestConvertReadsExactClaudeSource(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, filepath.Join(dir, "CLAUDE.md"), legacyConfig("architect", "claude-architect"))
	writeTestConfig(t, filepath.Join(dir, ".cursorrules"), legacyConfig("security", "cursor-security"))

	outPath, err := Convert(dir, "claude-code", "codex")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "claude-architect") || strings.Contains(content, "cursor-security") {
		t.Fatalf("converter did not select CLAUDE.md exactly:\n%s", content)
	}
}

func TestConvertDoesNotFallBackWhenClaudeSourceIsMissing(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, filepath.Join(dir, ".cursorrules"), legacyConfig("security", "cursor-security"))

	_, err := Convert(dir, "claude-code", "codex")
	if err == nil || !strings.Contains(err.Error(), "CLAUDE.md") {
		t.Fatalf("Convert() error = %v, want missing exact CLAUDE.md source", err)
	}
}

func TestConvertKeepsModelTiersOutOfConsiliumWithoutExecuting(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, filepath.Join(dir, "CLAUDE.md"), "### Consilium\n| Role | Agent |\n|------|-------|\n| architect | claude-architect |\n\n### Model tiers\nConcrete models come from the user adapter mapping.\n\n| Role | Tier |\n|------|-------|\n| architect | high |\n")

	outPath, err := Convert(dir, "claude-code", "codex")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Count(content, "- **architect**:") != 1 || strings.Contains(content, "(high)") {
		t.Fatalf("model tier was parsed as a consilium agent:\n%s", content)
	}
}

func TestConvertUpdatesManagedProjectTarget(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, filepath.Join(dir, "CLAUDE.md"), legacyConfig("architect", "legacy-agent"))
	writeTestConfig(t, filepath.Join(dir, "harnest.yaml"), `version: 2
project:
  name: managed-project
stacks:
  - name: fastapi
    lang: python
    category: backend
    path: .
agents:
  consilium:
    architect: managed-agent
  executing:
    - agent: general-purpose
      scope: "**/*.py"
harnesses:
  - claude-code
settings:
  language: ru
`)

	outPath, err := Convert(dir, "claude-code", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if outPath != filepath.Join(dir, "AGENTS.md") {
		t.Fatalf("output path = %q", outPath)
	}
	cfg, err := harnestYaml.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"codex"}) {
		t.Fatalf("harnesses = %v, want [codex]", cfg.Harnesses)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "managed-agent") || strings.Contains(string(data), "legacy-agent") {
		t.Fatalf("converter did not use harnest.yaml as source of truth:\n%s", data)
	}
}

func TestConvertRejectsInvalidSource(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		content string
		want    string
	}{
		{name: "unsupported", from: "cursor", want: "supported source: claude-code"},
		{name: "missing", from: "claude-code", want: "CLAUDE.md"},
		{name: "malformed", from: "claude-code", content: "### Consilium\n| broken\n", want: "malformed Consilium table"},
		{name: "mixed valid and malformed", from: "claude-code", content: "### Consilium\n| Role | Agent |\n|---|---|\n| architect | valid-agent |\n| security | invalid-agent | extra |\n", want: "expected 2 cells"},
		{name: "empty required cell", from: "claude-code", content: "### Consilium\n| Role | Agent |\n|---|---|\n| architect | |\n", want: "must not be empty"},
		{name: "extra column", from: "claude-code", content: "### Consilium\n| Role | Agent |\n|---|---|\n| architect | agent | extra |\n", want: "expected 2 cells"},
		{name: "no agents", from: "claude-code", content: "# Project instructions\n", want: "no agent config found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.content != "" {
				writeTestConfig(t, filepath.Join(dir, "CLAUDE.md"), tt.content)
			}
			_, err := Convert(dir, tt.from, "codex")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Convert() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func legacyConfig(role, agent string) string {
	return "### Consilium\n| Role | Agent |\n|---|---|\n| " + role + " | " + agent + " |\n\n### Executing\n| Agent | Scope |\n|---|---|\n"
}

func writeTestConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
