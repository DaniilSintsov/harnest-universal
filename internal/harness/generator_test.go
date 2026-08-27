package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniilsintsov/harnest-universal/internal/detector"
	"github.com/daniilsintsov/harnest-universal/internal/ir"
	"github.com/daniilsintsov/harnest-universal/internal/mapping"
)

func TestClaudeGeneratorUpdatesActiveFileAndPreservesUserContent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("user instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}

	generated, err := (&ClaudeCodeGenerator{}).Generate(dir, testProject())
	if err != nil {
		t.Fatal(err)
	}
	if generated != path {
		t.Fatalf("generated path = %s, want %s", generated, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{"user instructions", "harnest-managed:start", "| architect | high |"} {
		if !strings.Contains(content, want) {
			t.Fatalf("generated content missing %q: %s", want, content)
		}
	}
	if strings.Contains(content, "opus") || strings.Contains(content, "sonnet") {
		t.Fatalf("generated content hardcodes a model: %s", content)
	}
}

func TestCodexGeneratorUpdatesActiveFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("user instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}

	generated, err := (&CodexGenerator{}).Generate(dir, testProject())
	if err != nil {
		t.Fatal(err)
	}
	if generated != path {
		t.Fatalf("generated path = %s, want %s", generated, path)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.generated.md")); !os.IsNotExist(err) {
		t.Fatalf("inactive generated file was created: %v", err)
	}
}

func TestV1AdaptersExposeCapabilities(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"claude-code", "codex"} {
		caps, err := Capabilities(name)
		if err != nil {
			t.Fatal(err)
		}
		if caps.Instructions != ir.Native || caps.Agents != ir.Native {
			t.Fatalf("%s missing generated capabilities: %#v", name, caps)
		}
		if caps.PreToolHook == ir.Native || caps.PostToolHook == ir.Native || caps.Permissions == ir.Native || caps.Verification == ir.Native {
			t.Fatalf("%s claims a native capability it does not generate: %#v", name, caps)
		}
	}
}

func TestControlPlaneMarksConfiguredResourcesConditional(t *testing.T) {
	tests := []struct {
		language string
		want     []string
	}{
		{language: "en", want: []string{"If architecture entrypoint", "If skills root", "If rules root"}},
		{language: "ru", want: []string{"Если существует точка входа архитектуры", "Если существует каталог skills", "Если существует каталог rules"}},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			project := testProject()
			project.Language = tt.language
			project.Architecture = ir.Architecture{Index: "docs/architecture/INDEX.md"}
			project.Skills = ir.ResourceIndex{Root: ".agents/skills"}
			project.Rules = ir.ResourceIndex{Root: ".harnest/rules"}
			content := renderControlPlane(project)
			for _, want := range tt.want {
				if !strings.Contains(content, want) {
					t.Fatalf("control plane missing %q:\n%s", want, content)
				}
			}
		})
	}
}

func TestControlPlaneRequiresResearchPlanBeforeExecuting(t *testing.T) {
	project := testProject()
	project.Workflow.DefaultProfile = "business-feature"
	content := renderControlPlane(project)
	for _, want := range []string{"Research -> Plan -> Executing", "`business-feature`", "`auto`"} {
		if !strings.Contains(content, want) {
			t.Fatalf("strict control plane missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "тривиальные задачи можно выполнять напрямую") {
		t.Fatalf("strict control plane permits direct execution:\n%s", content)
	}
}

func TestControlPlaneKeepsExplicitAdaptiveOptIn(t *testing.T) {
	project := testProject()
	project.Workflow.Adaptive = true
	content := renderControlPlane(project)
	if !strings.Contains(content, "Адаптивный workflow явно включён") {
		t.Fatalf("adaptive opt-in missing:\n%s", content)
	}
}

func TestControlPlaneAsksForProfileAndSelectsRolesAutomatically(t *testing.T) {
	project := testProject()
	project.Agents.Consilium = append(project.Agents.Consilium, mapping.ConsiliumRole{Role: "security", Agent: "auto"})
	content := renderControlPlane(project)
	for _, want := range []string{"workflow-профиль интерактивно", "лучший match (recommended)", "`business-feature`", "Primary roles определяй автоматически"} {
		if !strings.Contains(content, want) {
			t.Fatalf("workflow selection instruction missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "попроси пользователя выбрать одну или несколько primary roles") {
		t.Fatalf("control plane still asks for roles:\n%s", content)
	}

	project.Language = "en"
	content = renderControlPlane(project)
	for _, want := range []string{"select a workflow profile interactively", "best match (recommended)", "Determine primary roles automatically"} {
		if !strings.Contains(content, want) {
			t.Fatalf("English workflow selection instruction missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "ask the user to choose one or more primary roles") {
		t.Fatalf("English control plane still asks for roles:\n%s", content)
	}
}

func TestControlPlaneTreatsLegacyInteractiveRoleSelectionAsAutomatic(t *testing.T) {
	project := testProject()
	project.Workflow.RoleSelection = ir.RoleSelectionInteractive
	content := renderControlPlane(project)
	if !strings.Contains(content, "Primary roles определяй автоматически") || strings.Contains(content, "выбрать одну или несколько primary roles") {
		t.Fatalf("legacy role selection instruction is wrong:\n%s", content)
	}
}

func testAgentConfig() mapping.AgentConfig {
	return mapping.AgentConfig{
		Consilium: []mapping.ConsiliumRole{{Role: "architect", Agent: "architect"}},
		Models:    map[string]string{"architect": "high"},
	}
}

func testProject() ir.Project {
	return ir.Project{
		Version:  2,
		Stacks:   []detector.Stack{{Name: "go", Path: "."}},
		Agents:   testAgentConfig(),
		Language: "ru",
		Workflow: ir.Workflow{VerifyChanged: true},
	}
}
