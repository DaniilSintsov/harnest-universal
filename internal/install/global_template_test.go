package install

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexTemplateUsesCodexProjectFileAndProfileDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), ".codex")
	content := globalTemplateFor("codex", dir)

	for _, want := range []string{"Codex", "AGENTS.md", filepath.Join(dir, "profiles"), "harnest verify --changed", "workflow-профиль интерактивно", "Primary roles определять автоматически"} {
		if !strings.Contains(content, want) {
			t.Fatalf("template does not contain %q", want)
		}
	}
	if strings.Contains(content, "~/.claude") || strings.Contains(content, "попросить пользователя выбрать одну или несколько primary roles") || !strings.Contains(content, "production deploy без") || !strings.Contains(content, "Research, затем Plan, затем Executing") || strings.Contains(content, "Мелкую локальную задачу выполнять напрямую") {
		t.Fatalf("template contains incompatible or unsafe workflow: %q", content)
	}
}

func TestClaudeTemplateUsesClaudeProjectFile(t *testing.T) {
	t.Parallel()

	content := globalTemplateFor("claude-code", filepath.Join(t.TempDir(), ".claude"))
	if !strings.Contains(content, "Claude Code") || !strings.Contains(content, "CLAUDE.md") {
		t.Fatalf("unexpected Claude template: %q", content)
	}
}
