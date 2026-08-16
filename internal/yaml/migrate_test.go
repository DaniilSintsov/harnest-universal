package yaml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniilsintsov/harnest-universal/internal/ir"
)

func TestMigrateV1AddsUniversalHarnessDefaults(t *testing.T) {
	t.Parallel()

	legacy := &HarnestConfig{Version: 1, Harnesses: []string{"claude-code", "codex"}}
	got, err := Migrate(legacy)
	if err != nil {
		t.Fatal(err)
	}

	if got.Version != CurrentVersion || got.Context.Architecture.Index != "docs/architecture/INDEX.md" {
		t.Fatalf("migration missing architecture defaults: %#v", got)
	}
	if got.Rules.Root != ".harnest/rules" || got.Workflow.Adaptive || got.Workflow.DefaultProfile != "business-feature" || got.Workflow.RoleSelection != ir.RoleSelectionAuto || !got.Workflow.VerifyChanged {
		t.Fatalf("migration missing policy/workflow defaults: %#v", got)
	}
	if got.Settings.Language != "ru" || !got.Settings.LocalDefault {
		t.Fatalf("migration missing local defaults: %#v", got.Settings)
	}
	if legacy.Version != 1 {
		t.Fatal("migration mutated input")
	}
}

func TestBuildIRDefaultsAndNormalizesRoleSelection(t *testing.T) {
	t.Parallel()

	cfg := &HarnestConfig{Version: CurrentVersion, Harnesses: []string{"codex"}}
	got, err := BuildIR(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Workflow.RoleSelection != ir.RoleSelectionAuto {
		t.Fatalf("role selection = %q", got.Workflow.RoleSelection)
	}

	cfg.Workflow.RoleSelection = ir.RoleSelectionInteractive
	got, err = BuildIR(t.TempDir(), cfg)
	if err != nil || got.Workflow.RoleSelection != ir.RoleSelectionAuto {
		t.Fatalf("legacy interactive role selection = %q, %v", got.Workflow.RoleSelection, err)
	}

	cfg.Workflow.RoleSelection = "sometimes"
	if _, err := BuildIR(t.TempDir(), cfg); err == nil || !strings.Contains(err.Error(), "workflow.role_selection") {
		t.Fatalf("invalid role selection error = %v", err)
	}
}

func TestMigrateFileBacksUpAndWritesV2(t *testing.T) {
	dir := t.TempDir()
	legacy := []byte("version: 1\nagents:\n  consilium: {}\n  executing: []\nharnesses: [codex]\n")
	if err := os.WriteFile(filepath.Join(dir, configFileName), legacy, 0644); err != nil {
		t.Fatal(err)
	}

	changed, backup, err := MigrateFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || backup == "" {
		t.Fatalf("expected migration and backup, got changed=%v backup=%q", changed, backup)
	}
	backedUp, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(backedUp) != string(legacy) {
		t.Fatal("backup does not match original")
	}
	updated, err := os.ReadFile(filepath.Join(dir, configFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "version: 2") || !strings.Contains(string(updated), "local_default: true") {
		t.Fatalf("unexpected migrated config:\n%s", updated)
	}
}

func TestUpdateLocalExcludeDoesNotTouchGitignore(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "info"), 0755); err != nil {
		t.Fatal(err)
	}
	gitignore := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignore, []byte("node_modules/\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := UpdateLocalExclude(dir, []string{filepath.Join(dir, "AGENTS.md")}); err != nil {
		t.Fatal(err)
	}
	exclude, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"AGENTS.md", "AGENTS.md.bak", "harnest.yaml", ".harnest/", ".agents/skills/", "docs/architecture/", localConfigFileName} {
		if !strings.Contains(string(exclude), want) {
			t.Fatalf("missing %q in exclude:\n%s", want, exclude)
		}
	}
	unchanged, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != "node_modules/\n" {
		t.Fatal(".gitignore was modified")
	}
}

func TestUpdateLocalExcludeReportsNoChangeOutsideGitRepository(t *testing.T) {
	changed, err := UpdateLocalExclude(t.TempDir(), []string{"AGENTS.md"})
	if err != nil || changed {
		t.Fatalf("UpdateLocalExclude() = %v, %v", changed, err)
	}
}

func TestUpdateLocalExcludeSupportsLinkedWorktree(t *testing.T) {
	dir := t.TempDir()
	common := filepath.Join(t.TempDir(), "repo.git")
	worktreeGitDir := filepath.Join(common, "worktrees", "feature")
	if err := os.MkdirAll(worktreeGitDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+worktreeGitDir+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeGitDir, "commondir"), []byte("../..\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := UpdateLocalExclude(dir, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(common, "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "harnest.yaml") {
		t.Fatalf("linked worktree exclude not updated: %s", data)
	}
}

func TestBuildIRCarriesControlPlanePaths(t *testing.T) {
	t.Parallel()

	cfg, err := Migrate(&HarnestConfig{Version: 1, Harnesses: []string{"codex"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := BuildIR(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	if got.Rules.Root != ".harnest/rules" || got.Architecture.Index != "docs/architecture/INDEX.md" {
		t.Fatalf("unexpected IR: %#v", got)
	}
	if len(got.Targets) != 1 || got.Targets[0] != "codex" {
		t.Fatalf("unexpected targets: %v", got.Targets)
	}
}

func TestBuildIRRejectsUnimplementedConfig(t *testing.T) {
	tests := []struct {
		name string
		edit func(*HarnestConfig)
		want string
	}{
		{"design system", func(cfg *HarnestConfig) { cfg.DesignSystem = "brand" }, "design_system is not implemented"},
		{"profiles", func(cfg *HarnestConfig) { cfg.Profiles.Enabled = []string{"research"} }, "profiles config is not implemented"},
		{"lock file", func(cfg *HarnestConfig) { cfg.Settings.LockFile = true }, "settings.lock_file is not implemented"},
		{"adapter models", func(cfg *HarnestConfig) {
			cfg.Adapters = map[string]AdapterSettings{"codex": {Models: map[string]string{"high": "gpt"}}}
		}, "adapters.codex.models is not implemented"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &HarnestConfig{Version: CurrentVersion, Harnesses: []string{"codex"}}
			test.edit(cfg)
			_, err := BuildIR(t.TempDir(), cfg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BuildIR() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestMigrateFileRejectsUnimplementedCurrentConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &HarnestConfig{Version: CurrentVersion, Harnesses: []string{"codex"}, Settings: SettingsBlock{LockFile: true}}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	changed, _, err := MigrateFile(dir)
	if err == nil || !strings.Contains(err.Error(), "settings.lock_file is not implemented") {
		t.Fatalf("MigrateFile() = %v, error %v", changed, err)
	}
}
