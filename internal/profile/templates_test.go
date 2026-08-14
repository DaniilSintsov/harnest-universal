package profile

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuiltinProfileSet(t *testing.T) {
	t.Parallel()

	want := []string{
		"bug-hunting",
		"business-feature",
		"code-review",
		"coordinator",
		"e2e-testing",
		"redesign",
		"refactoring",
		"research",
		"strat-session",
		"task-creation",
	}
	if got := BuiltinNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("BuiltinNames() = %v, want %v", got, want)
	}
}

func TestBuiltinProfilesAvoidUnsafeProductionDefaults(t *testing.T) {
	t.Parallel()

	for name, content := range builtinProfiles {
		lower := strings.ToLower(content)
		for _, forbidden := range []string{"**deploy** — deploy to prod", "деплой на прод (если нужен)"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("profile %s contains forbidden default %q", name, forbidden)
			}
		}
	}
}

func TestCodexProfilesUseCodexInstructionsAndModels(t *testing.T) {
	t.Parallel()

	var all strings.Builder
	for _, name := range BuiltinNames() {
		content, ok := BuiltinContentFor(name, "codex")
		if !ok {
			t.Fatalf("missing builtin profile %s", name)
		}
		all.WriteString(content)
	}
	content := all.String()
	for _, want := range []string{"AGENTS.md", "sol", "terra", "luna", "Codex subagent workflow"} {
		if !strings.Contains(content, want) {
			t.Errorf("Codex profiles do not contain %q", want)
		}
	}
	for _, forbidden := range []string{
		"CLAUDE.md", "~/.claude", "AskUserQuestion", "Task tool", "Read tool",
		"playwright-cli", "Bash", "`Explore`", "general-purpose", "`/loop`", "`/deploy`",
		"/test-android", "/test-ios", "/test-desktop", "| opus", "| sonnet", "| haiku", "voltagent-",
	} {
		if strings.Contains(content, forbidden) {
			t.Errorf("Codex profiles contain Claude-specific token %q", forbidden)
		}
	}
}

func TestClaudeProfilesKeepClaudeInstructionsAndModels(t *testing.T) {
	t.Parallel()

	content, ok := BuiltinContentFor("business-feature", "claude-code")
	if !ok {
		t.Fatal("missing business-feature")
	}
	for _, want := range []string{"CLAUDE.md", "opus", "sonnet", "haiku", "Task tool"} {
		if !strings.Contains(content, want) {
			t.Errorf("Claude profile does not contain %q", want)
		}
	}
}

func TestCodeChangingProfilesPlanBeforeExecution(t *testing.T) {
	if strings.Contains(businessFeature, "Research   -> Executing") {
		t.Fatal("business-feature bypasses Plan")
	}
	if !strings.Contains(bugHunting, "Diagnose   -> Plan") || !strings.Contains(bugHunting, "Plan       -> Fix") {
		t.Fatal("bug-hunting does not plan before Fix")
	}
}
