package drift

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRejectsSchemaV2BeforeReadingLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	writeDriftFixture(t, filepath.Join(dir, "harnest.yaml"), "version: 2\n")
	writeDriftFixture(t, filepath.Join(dir, "CLAUDE.md"), driftClaudeConfig())

	_, err := Check(dir)
	if err == nil || !strings.Contains(err.Error(), "does not support schema v2") {
		t.Fatalf("Check() error = %v, want schema v2 unsupported", err)
	}
}

func TestCheckAllowsSchemaV1LegacyConfig(t *testing.T) {
	dir := t.TempDir()
	writeDriftFixture(t, filepath.Join(dir, "harnest.yaml"), "version: 1\n")
	writeDriftFixture(t, filepath.Join(dir, "CLAUDE.md"), driftClaudeConfig())

	result, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Harness != "claude-code" || result.ConfigFile != filepath.Join(dir, "CLAUDE.md") {
		t.Fatalf("unexpected legacy result: %#v", result)
	}
}

func driftClaudeConfig() string {
	return "### Consilium\n| Role | Agent |\n|---|---|\n| architect | general-purpose |\n\n### Executing\n| Agent | Scope |\n|---|---|\n"
}

func writeDriftFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
