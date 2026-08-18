package agents

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestDiscover(t *testing.T) {
	all := Discover("")
	t.Logf("Found %d agents", len(all))
	for _, a := range all {
		t.Logf("  %s", a)
	}
	if len(all) == 0 {
		t.Log("No agents found (may be expected in CI)")
	}
}

func TestDiscoverProject(t *testing.T) {
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".claude", "agents")
	mustMkdir(t, agentsDir)

	// Agent 1: with frontmatter name (different from filename)
	writeFile(t, filepath.Join(agentsDir, "backend.md"), `---
name: backend-csharp
description: Backend specialist for C#
---
Some agent instructions.
`)

	// Agent 2: with frontmatter name matching filename
	writeFile(t, filepath.Join(agentsDir, "vue-expert.md"), `---
name: vue-expert
description: Vue.js frontend specialist
---
Vue expert instructions.
`)

	// Agent 3: no frontmatter — falls back to filename
	writeFile(t, filepath.Join(agentsDir, "no-fm.md"), "Just markdown content.\nNo frontmatter here.")

	// Agent 4: README.md — should be skipped
	writeFile(t, filepath.Join(agentsDir, "README.md"), `---
name: readme-agent
---
This is a README, should be skipped.
`)

	// Agent 5: frontmatter has no name field — fallback to filename
	writeFile(t, filepath.Join(agentsDir, "fallback.md"), `---
description: No name field here
---
Uses filename as agent name.
`)

	// Agent 6: non-.md file — should be skipped
	writeFile(t, filepath.Join(agentsDir, "plugin.json"), `{"name": "test"}`)

	agents := DiscoverProject(tmpDir)
	sort.Strings(agents)

	expected := []string{"backend-csharp", "fallback", "no-fm", "vue-expert"}
	if len(agents) != len(expected) {
		t.Fatalf("expected %d agents, got %d: %v", len(expected), len(agents), agents)
	}
	for i, a := range agents {
		if a != expected[i] {
			t.Errorf("agent[%d]: expected %q, got %q", i, expected[i], a)
		}
	}
}

func TestDiscoverProject_EmptyDir(t *testing.T) {
	emptyDir := filepath.Join(t.TempDir(), ".claude", "agents")
	mustMkdir(t, emptyDir)
	agents := DiscoverProject(filepath.Dir(emptyDir))
	if len(agents) != 0 {
		t.Errorf("expected 0 agents from empty dir, got %d", len(agents))
	}
}

func TestDiscoverProject_NonExistent(t *testing.T) {
	agents := DiscoverProject(filepath.Join(t.TempDir(), "nonexistent"))
	if len(agents) != 0 {
		t.Errorf("expected 0 agents from nonexistent dir, got %d", len(agents))
	}
}

func TestDiscoverProject_SupportedHarnessesOnly(t *testing.T) {
	// Only agents from registered harness dirs should be discovered.
	tmpDir := t.TempDir()

	// claude-code
	claudeDir := filepath.Join(tmpDir, ".claude", "agents")
	mustMkdir(t, claudeDir)
	writeFile(t, filepath.Join(claudeDir, "claude-agent.md"), "---\nname: claude-expert\n---\nbody")

	// cursor
	cursorDir := filepath.Join(tmpDir, ".cursor", "agents")
	mustMkdir(t, cursorDir)
	writeFile(t, filepath.Join(cursorDir, "cursor-agent.md"), "---\nname: cursor-expert\n---\nbody")

	// windsurf
	windsurfDir := filepath.Join(tmpDir, ".windsurf", "agents")
	mustMkdir(t, windsurfDir)
	writeFile(t, filepath.Join(windsurfDir, "windsurf-agent.md"), "---\nname: windsurf-expert\n---\nbody")

	// codex
	codexDir := filepath.Join(tmpDir, ".codex", "agents")
	mustMkdir(t, codexDir)
	writeFile(t, filepath.Join(codexDir, "codex-agent.md"), "---\nname: codex-expert\n---\nbody")

	// opencode
	opencodeDir := filepath.Join(tmpDir, ".config", "opencode", "agents")
	mustMkdir(t, opencodeDir)
	writeFile(t, filepath.Join(opencodeDir, "opencode-agent.md"), "---\nname: opencode-expert\n---\nbody")

	// qwen-code
	qwenDir := filepath.Join(tmpDir, ".qwen", "agents")
	mustMkdir(t, qwenDir)
	writeFile(t, filepath.Join(qwenDir, "qwen-agent.md"), "---\nname: qwen-expert\n---\nbody")

	agents := DiscoverProject(tmpDir)

	if len(agents) != 2 {
		t.Fatalf("expected 2 agents from supported harness dirs, got %d: %v", len(agents), agents)
	}

	expected := []string{"claude-expert", "codex-expert"}
	for i, a := range agents {
		if a != expected[i] {
			t.Errorf("agent[%d]: expected %q, got %q", i, expected[i], a)
		}
	}
}

func TestParseAgentName(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		filename string
		content  string
		expected string
	}{
		{
			filename: "my-agent.md",
			content:  "---\nname: my-agent\ndescription: test\n---\nbody",
			expected: "my-agent",
		},
		{
			filename: "crlf.md",
			content:  "---\r\nname: crlf-agent\r\ndescription: test\r\n---\r\nbody",
			expected: "crlf-agent",
		},
		{
			filename: "no-fm.md",
			content:  "Just some markdown content.\nNo frontmatter here.",
			expected: "no-fm", // no frontmatter → falls back to filename
		},
		{
			filename: "fallback.md",
			content:  "---\ndescription: only description\n---\nbody",
			expected: "fallback", // falls back to filename
		},
		{
			filename: "unnamed.md",
			content:  "---\nname: \"\"\ndescription: test\n---\nbody",
			expected: "unnamed", // empty name → falls back to filename
		},
		{
			filename: "broken.md",
			content:  "---\nname: broken\n",
			expected: "broken", // no closing delimiter → falls back to filename
		},
		{
			filename: "deep-nested.md",
			content:  "---\nname: ok\ndep1:\n  dep2:\n    dep3:\n      dep4: deep\n---\nbody",
			expected: "ok", // deeply nested but valid YAML
		},
		{
			filename: "unicode.md",
			content:  "---\nname: агент-тест\ndescription: тестирование\n---\nbody",
			expected: "агент-тест", // unicode name
		},
		{
			filename: "binary-null.md",
			content:  "---\nname: bad\x00agent\n---\nbody",
			expected: "", // null byte → rejected
		},
		{
			filename: "huge-frontmatter.md",
			content:  "---\nname: ok\n" + strings.Repeat("data: xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n", 100) + "---\nbody",
			expected: "huge-frontmatter", // frontmatter > 4KB → falls back to filename
		},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			filePath := filepath.Join(tmpDir, tt.filename)
			writeFile(t, filePath, tt.content)

			got := parseAgentName(filePath)
			if got != tt.expected {
				t.Errorf("parseAgentName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestIndexClosingDelim(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"clean", "name: x\n---\nbody", 7},
		{"eof", "name: x\n---", 7},
		{"crlf", "name: x\n---\r\nbody", 7},
		{"dashes-skipped", "name: x\n----\n---\nbody", 12},
		{"text-after-skipped", "name: x\n---bad\n---\nbody", 14},
		{"none", "name: x\nno delim", -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := indexClosingDelim(c.in); got != c.want {
				t.Errorf("indexClosingDelim(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestScanPlugins_AgentsDirectory(t *testing.T) {
	// Simulate plugin cache: <root>/marketplace/plugin/1.0.0/.claude-plugin/plugin.json
	// with agents/ at version level
	root := t.TempDir()

	pluginDir := filepath.Join(root, "marketplace", "my-plugin", "1.0.0", ".claude-plugin")
	mustMkdir(t, pluginDir)

	// plugin.json — no "agents" field (modern style)
	writeFile(t, filepath.Join(pluginDir, "plugin.json"), `{
  "name": "my-plugin",
  "version": "1.0.0"
}`)

	// agents/ directory at version level
	versionDir := filepath.Dir(pluginDir)
	agentsDir := filepath.Join(versionDir, "agents")
	mustMkdir(t, agentsDir)
	writeFile(t, filepath.Join(agentsDir, "alpha.md"), "---\nname: alpha\n---\nbody")
	writeFile(t, filepath.Join(agentsDir, "beta.md"), "---\nname: beta\n---\nbody")
	writeFile(t, filepath.Join(agentsDir, "README.md"), "# README")

	agents := scanPlugins(root)

	// scanFlatNames uses filename (not frontmatter), namespaced as plugin:agent
	if !contains(agents, "my-plugin:alpha") {
		t.Error("expected my-plugin:alpha to be discovered from agents/ dir")
	}
	if !contains(agents, "my-plugin:beta") {
		t.Error("expected my-plugin:beta to be discovered from agents/ dir")
	}
	if contains(agents, "my-plugin:README") {
		t.Error("README.md should be skipped")
	}
}

func TestScanPlugins_ExplicitAgentsField(t *testing.T) {
	// Backward compat: plugin.json with explicit "agents" field
	root := t.TempDir()

	pluginDir := filepath.Join(root, "marketplace", "old-plugin", "2.0.0", ".claude-plugin")
	mustMkdir(t, pluginDir)

	writeFile(t, filepath.Join(pluginDir, "plugin.json"), `{
  "name": "old-plugin",
  "agents": ["./custom-agent.md", "./specialist.md"]
}`)

	// Create agent files relative to version dir (parent of .claude-plugin/)
	versionDir := filepath.Dir(pluginDir)
	writeFile(t, filepath.Join(versionDir, "custom-agent.md"), "# Custom Agent")
	writeFile(t, filepath.Join(versionDir, "specialist.md"), "# Specialist")

	agents := scanPlugins(root)

	if !contains(agents, "old-plugin:custom-agent") {
		t.Error("expected old-plugin:custom-agent from explicit agents field")
	}
	if !contains(agents, "old-plugin:specialist") {
		t.Error("expected old-plugin:specialist from explicit agents field")
	}
}

func TestScanPlugins_NoPluginName(t *testing.T) {
	// plugin.json without "name" field — should be skipped entirely
	root := t.TempDir()

	pluginDir := filepath.Join(root, "marketplace", "anon", "1.0.0", ".claude-plugin")
	mustMkdir(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.json"), `{
  "version": "1.0.0"
}`)

	// Create agents/ directory — should NOT be scanned (no plugin name)
	versionDir := filepath.Dir(pluginDir)
	agentsDir := filepath.Join(versionDir, "agents")
	mustMkdir(t, agentsDir)
	writeFile(t, filepath.Join(agentsDir, "ghost.md"), "---\nname: ghost\n---\nbody")

	agents := scanPlugins(root)

	if len(agents) != 0 {
		t.Errorf("expected 0 agents from nameless plugin, got %d: %v", len(agents), agents)
	}
}

func TestScanPlugins_EmptyAgentsField(t *testing.T) {
	// plugin.json with empty agents array but agents/ dir → dir scan finds them
	root := t.TempDir()

	pluginDir := filepath.Join(root, "marketplace", "mixed", "1.0.0", ".claude-plugin")
	mustMkdir(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.json"), `{
  "name": "mixed-plugin",
  "agents": []
}`)

	versionDir := filepath.Dir(pluginDir)
	agentsDir := filepath.Join(versionDir, "agents")
	mustMkdir(t, agentsDir)
	writeFile(t, filepath.Join(agentsDir, "from-dir.md"), "---\nname: from-dir\n---\nbody")

	agents := scanPlugins(root)

	if !contains(agents, "mixed-plugin:from-dir") {
		t.Error("expected mixed-plugin:from-dir from agents/ dir despite empty agents field")
	}
}

func TestDiscoverForTargetKeepsPlatformProvenance(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeFile(t, filepath.Join(project, ".agents", "agents", "portable.md"), "---\nname: architect\n---\n")
	writeFile(t, filepath.Join(project, ".claude", "agents", "claude-only.md"), "---\nname: claude-only\n---\n")
	writeFile(t, filepath.Join(home, ".codex", "agents", "codex-only.toml"), "# codex")
	writeFile(t, filepath.Join(home, ".codex", "agents", "legacy-codex.md"), "legacy")
	writeFile(t, filepath.Join(project, ".codex", "agents", "filename-is-not-identity.toml"), "name = \"native-name\"\ndescription = \"test\"\ndeveloper_instructions = \"test\"\n")

	got := DiscoverForTarget(project, "codex")
	if !contains(got, "architect") || !contains(got, "codex-only") || !contains(got, "legacy-codex") || !contains(got, "native-name") {
		t.Fatalf("missing portable/codex agents: %v", got)
	}
	if contains(got, "filename-is-not-identity") {
		t.Fatalf("Codex TOML filename replaced its canonical name: %v", got)
	}
	if contains(got, "portable:architect") {
		t.Fatalf("portable agent exposed a non-callable synthetic ID: %v", got)
	}
	if contains(got, "claude-only") {
		t.Fatalf("Claude-only agent leaked into Codex discovery: %v", got)
	}
}

func TestMaterializePortableCopiesOnlySelectedTargetsAndBacksUpCollision(t *testing.T) {
	project := t.TempDir()
	source := "---\nname: architect\ndescription: portable\n---\nInstructions.\n"
	writeFile(t, filepath.Join(project, ".agents", "agents", "portable.md"), source)
	claudePath := filepath.Join(project, ".claude", "agents", "architect.md")
	writeFile(t, claudePath, "user-owned agent\n")

	files, err := MaterializePortable(project, []string{"claude-code", "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("materialized files = %v, want two selected targets", files)
	}
	claudeData, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if content := string(claudeData); !strings.HasPrefix(content, "---\nname: architect\n") || !strings.Contains(content, portableOwnershipMarker) {
		t.Fatalf("Claude agent is not callable/managed: %q", content)
	}
	codexPath := filepath.Join(project, ".codex", "agents", "architect.toml")
	codexData, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`name = "architect"`,
		`description = "portable"`,
		`developer_instructions = "Instructions.\n"`,
		portableCodexOwnershipMarker,
	} {
		if !strings.Contains(string(codexData), want) {
			t.Fatalf("Codex agent missing %q: %s", want, codexData)
		}
	}
	if _, err := os.Stat(filepath.Join(project, ".cursor", "agents", "architect.md")); !os.IsNotExist(err) {
		t.Fatalf("unselected target was materialized: %v", err)
	}
	backup, err := os.ReadFile(claudePath + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != "user-owned agent\n" {
		t.Fatalf("collision backup = %q", backup)
	}
}

func TestMaterializePortableRejectsUnsafeCallableName(t *testing.T) {
	project := t.TempDir()
	writeFile(t, filepath.Join(project, ".agents", "agents", "portable.md"), "---\nname: ../escape\n---\nbody\n")

	if _, err := MaterializePortable(project, []string{"codex"}); err == nil {
		t.Fatal("unsafe portable agent name was accepted")
	}
	if _, err := os.Stat(filepath.Join(project, ".codex", "escape.toml")); !os.IsNotExist(err) {
		t.Fatalf("unsafe agent escaped target directory: %v", err)
	}
}

func TestMaterializePortableKeepsOriginalCollisionBackupAcrossUpdates(t *testing.T) {
	project := t.TempDir()
	sourcePath := filepath.Join(project, ".agents", "agents", "portable.md")
	targetPath := filepath.Join(project, ".codex", "agents", "architect.toml")
	writeFile(t, sourcePath, "---\nname: architect\n---\nversion one\n")
	writeFile(t, targetPath, "original user agent\n")

	if _, err := MaterializePortable(project, []string{"codex"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, sourcePath, "---\nname: architect\n---\nversion two\n")
	if _, err := MaterializePortable(project, []string{"codex"}); err != nil {
		t.Fatal(err)
	}

	backup, err := os.ReadFile(targetPath + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != "original user agent\n" {
		t.Fatalf("managed update replaced original collision backup: %q", backup)
	}
	target, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(target), "version two") {
		t.Fatalf("managed target was not updated: %q", target)
	}
}

func TestMaterializePortableRestoresCollisionWhenSourceRemoved(t *testing.T) {
	project := t.TempDir()
	sourcePath := filepath.Join(project, ".agents", "agents", "portable.md")
	targetPath := filepath.Join(project, ".claude", "agents", "architect.md")
	writeFile(t, sourcePath, "---\nname: architect\n---\nportable\n")
	writeFile(t, targetPath, "original user agent\n")
	if _, err := MaterializePortable(project, []string{"claude-code"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sourcePath); err != nil {
		t.Fatal(err)
	}

	if _, err := MaterializePortable(project, []string{"claude-code"}); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != "original user agent\n" {
		t.Fatalf("collision was not restored: %q", restored)
	}
}

func TestMaterializePortableBacksUpStaleOwnedAgentWhenSourceDirRemoved(t *testing.T) {
	project := t.TempDir()
	sourceDir := filepath.Join(project, ".agents", "agents")
	targetPath := filepath.Join(project, ".codex", "agents", "architect.toml")
	writeFile(t, filepath.Join(sourceDir, "portable.md"), "---\nname: architect\n---\nportable\n")
	if _, err := MaterializePortable(project, []string{"codex"}); err != nil {
		t.Fatal(err)
	}
	desired, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(sourceDir); err != nil {
		t.Fatal(err)
	}

	if _, err := MaterializePortable(project, []string{"codex"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("stale owned agent still exists: %v", err)
	}
	sum := sha256.Sum256(desired)
	backupPath := fmt.Sprintf("%s.deleted-%x.bak", targetPath, sum[:6])
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(desired) {
		t.Fatalf("stale backup = %q", backup)
	}
}

func TestMaterializePortableCodexEscapesTOMLAndCleansLegacyMarkdown(t *testing.T) {
	project := t.TempDir()
	source := "---\nname: reviewer\ndescription: Проверяет \"сложное\"\n---\nUse \"quotes\", \\slashes and \"\"\" blocks.\r\n"
	writeFile(t, filepath.Join(project, ".agents", "agents", "reviewer.md"), source)
	legacyPath := filepath.Join(project, ".codex", "agents", "reviewer.md")
	writeFile(t, legacyPath, "---\nname: reviewer\n---\nold\n\n"+portableOwnershipMarker+"\n")

	if _, err := MaterializePortable(project, []string{"codex"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy managed Codex markdown still exists: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(project, ".codex", "agents", "reviewer.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`name = "reviewer"`,
		`description = "Проверяет \"сложное\""`,
		`developer_instructions = "Use \"quotes\", \\slashes and \"\"\" blocks.\n"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("native Codex TOML missing %q: %s", want, data)
		}
	}
}

func TestMaterializePortableNeverTouchesUnmanagedTargetWhenSourcesMissing(t *testing.T) {
	project := t.TempDir()
	targetPath := filepath.Join(project, ".codex", "agents", "user.md")
	writeFile(t, targetPath, "user agent\n")
	unknownMarkerPath := filepath.Join(project, ".codex", "agents", "other.md")
	writeFile(t, unknownMarkerPath, "agent\n<!-- harnest-portable-agent:other -->\n")
	indentedMarkerPath := filepath.Join(project, ".codex", "agents", "indented.md")
	writeFile(t, indentedMarkerPath, "agent\n  "+portableOwnershipMarker+"\n")

	if _, err := MaterializePortable(project, []string{"codex"}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		targetPath:         "user agent\n",
		unknownMarkerPath:  "agent\n<!-- harnest-portable-agent:other -->\n",
		indentedMarkerPath: "agent\n  " + portableOwnershipMarker + "\n",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("unmanaged target %s changed: %q", path, data)
		}
	}
}

func TestPortablePathsPreviewsCleanupWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, ".codex", "agents", "stale.toml")
	content := "name = \"stale\"\n" + portableCodexOwnershipMarker + "\n"
	writeFile(t, stale, content)

	paths, err := PortablePaths(dir, []string{"codex"})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(paths, stale+" (remove)") {
		t.Fatalf("cleanup preview missing: %v", paths)
	}
	if got, err := os.ReadFile(stale); err != nil || string(got) != content {
		t.Fatalf("preview changed stale file: %q, %v", got, err)
	}
}

func TestPortablePathsRejectsUnknownOwnershipMarker(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".agents", "agents", "architect.md"), "---\nname: architect\n---\n")
	writeFile(t, filepath.Join(dir, ".codex", "agents", "architect.toml"), "# harnest-portable-agent:future\n")
	if _, err := PortablePaths(dir, []string{"codex"}); err == nil || !strings.Contains(err.Error(), "unknown portable-agent ownership marker") {
		t.Fatalf("PortablePaths() error = %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
