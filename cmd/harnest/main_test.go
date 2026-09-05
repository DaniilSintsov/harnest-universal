package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/daniilsintsov/harnest-universal/internal/detector"
	"github.com/daniilsintsov/harnest-universal/internal/mapping"
	yamlconfig "github.com/daniilsintsov/harnest-universal/internal/yaml"
)

func TestParseDirArgDoesNotConsumeBooleanFlagValue(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	os.Args = []string{"harnest", "verify", "--changed", "/tmp/project", "--allow", "rule-id"}

	if got := parseDirArg(2); got != "/tmp/project" {
		t.Fatalf("parseDirArg() = %q", got)
	}
}

func TestForkVersionHasReleaseProvenance(t *testing.T) {
	if !strings.Contains(version, "universal") {
		t.Fatalf("fork version is indistinguishable from upstream: %s", version)
	}
	if strings.Contains(version, "+universal.local") {
		t.Fatalf("release uses local-only version metadata: %s", version)
	}
}

func TestInstallDefaultsToBothTargetsOnCleanHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	want := []string{"claude-code", "codex"}
	if got := resolveInstallTargets(""); !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveInstallTargets() = %v, want %v", got, want)
	}
}

func TestInstallDefaultsToBothTargetsWhenOneAlreadyExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	want := []string{"claude-code", "codex"}
	if got := resolveInstallTargets(""); !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveInstallTargets() = %v, want %v", got, want)
	}
}

func TestSubcommandHelpHasNoSideEffects(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()

	for _, command := range []string{"install", "init"} {
		out, err := runMainCLI(t, home, project, command, "-h")
		if err != nil {
			t.Fatalf("%s -h failed: %v\n%s", command, err, out)
		}
		if !strings.Contains(out, "Usage:") {
			t.Fatalf("%s -h missing usage:\n%s", command, out)
		}
	}
	for _, path := range []string{filepath.Join(home, ".claude"), filepath.Join(home, ".codex"), filepath.Join(project, "harnest.yaml")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("help created %s: %v", path, err)
		}
	}
}

func TestNewProjectConfigUsesStrictWorkflowAndAutoConsilium(t *testing.T) {
	stacks := []detector.Stack{{Name: "go", Lang: "go", Category: "backend", Path: "."}}
	cfg := newProjectConfig(t.TempDir(), stacks, mapping.Resolve(stacks, nil, "codex"), nil, []string{"codex"})
	if cfg.Workflow.Adaptive || cfg.Workflow.DefaultProfile != "business-feature" || cfg.Workflow.RoleSelection != "auto" {
		t.Fatalf("unexpected workflow defaults: %#v", cfg.Workflow)
	}
	if len(cfg.Agents.Consilium) == 0 {
		t.Fatal("consilium is empty")
	}
}

func TestInitNonInteractiveStoresTargetSpecificAgents(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	writeTestFile(t, filepath.Join(home, ".claude", "agents", "claude-security.md"), "---\nname: claude-security\n---\n")
	writeTestFile(t, filepath.Join(home, ".codex", "agents", "codex-security.md"), "---\nname: codex-security\n---\n")

	out, err := runMainCLI(t, home, project, "init", "--non-interactive")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	harnestYaml, err := os.ReadFile(filepath.Join(project, "harnest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"harnesses:",
		"  - claude-code",
		"  - codex",
		"adapters:",
		"claude-code:",
		"codex:",
		"claude-security",
		"codex-security",
	} {
		if !strings.Contains(string(harnestYaml), want) {
			t.Fatalf("harnest.yaml missing %q:\n%s", want, harnestYaml)
		}
	}

	claudeConfig, err := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(claudeConfig), "claude-security") || strings.Contains(string(claudeConfig), "codex-security") {
		t.Fatalf("unexpected CLAUDE.md:\n%s", claudeConfig)
	}

	codexConfig, err := os.ReadFile(filepath.Join(project, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(codexConfig), "codex-security") || strings.Contains(string(codexConfig), "claude-security") {
		t.Fatalf("unexpected AGENTS.md:\n%s", codexConfig)
	}

	listed, err := runMainCLI(t, home, project, "agents", "list")
	if err != nil {
		t.Fatalf("agents list failed: %v\n%s", err, listed)
	}
	for _, want := range []string{"[claude-code]", "claude-security", "[codex]", "codex-security"} {
		if !strings.Contains(listed, want) {
			t.Fatalf("agents list missing %q:\n%s", want, listed)
		}
	}
}

func TestInitInteractiveReadsBothTargetWizards(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	promptCount := len(mapping.ResolveStructure(nil).Roles) * 2

	out, err := runMainCLIWithInput(t, home, project, strings.Repeat("\n", promptCount), "init")
	if err != nil {
		t.Fatalf("interactive init failed: %v\n%s", err, out)
	}
	if got := strings.Count(out, "── Agent Wizard:"); got != 2 {
		t.Fatalf("target wizard count = %d, want 2:\n%s", got, out)
	}
	if _, err := os.Stat(filepath.Join(project, "harnest.yaml")); err != nil {
		t.Fatalf("harnest.yaml missing: %v", err)
	}
}

func TestAgentsSetTargetsAdapterOverride(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		model bool
		want  string
	}{
		{name: "agent", args: []string{"agents", "set", "security", "new-codex-security", "--harness", "codex"}, want: "new-codex-security"},
		{name: "model", args: []string{"agents", "set-model", "security", "low", "--harness", "codex"}, model: true, want: "low"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			projectDir := t.TempDir()
			writeSplitAgentConfig(t, projectDir)

			out, err := runMainCLI(t, home, projectDir, tc.args...)
			if err != nil {
				t.Fatalf("target update failed: %v\n%s", err, out)
			}
			cfg, err := yamlconfig.Load(projectDir)
			if err != nil {
				t.Fatal(err)
			}
			project, err := yamlconfig.BuildIR(projectDir, cfg)
			if err != nil {
				t.Fatal(err)
			}
			claude := yamlconfig.ResolveTargetAgents(project, "claude-code")
			codex := yamlconfig.ResolveTargetAgents(project, "codex")
			if tc.model {
				if got := codex.Models["security"]; got != tc.want {
					t.Fatalf("codex model = %q, want %q", got, tc.want)
				}
				if got := claude.Models["security"]; got != "medium" {
					t.Fatalf("claude model changed to %q", got)
				}
			} else {
				if got := consiliumAgentForRole(codex, "security"); got != tc.want {
					t.Fatalf("codex agent = %q, want %q", got, tc.want)
				}
				if got := consiliumAgentForRole(claude, "security"); got != "shared-security" {
					t.Fatalf("claude agent changed to %q", got)
				}
			}
		})
	}
}

func TestAgentsSetRejectsAmbiguousSharedUpdate(t *testing.T) {
	for _, args := range [][]string{
		{"agents", "set", "security", "replacement"},
		{"agents", "set-model", "security", "low"},
	} {
		t.Run(args[1], func(t *testing.T) {
			home := t.TempDir()
			project := t.TempDir()
			writeSplitAgentConfig(t, project)
			before, err := os.ReadFile(filepath.Join(project, "harnest.yaml"))
			if err != nil {
				t.Fatal(err)
			}

			out, err := runMainCLI(t, home, project, args...)
			if err == nil || !strings.Contains(out, "use --harness <target>") {
				t.Fatalf("ambiguous update error = %v, output:\n%s", err, out)
			}
			after, err := os.ReadFile(filepath.Join(project, "harnest.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("ambiguous update modified harnest.yaml")
			}
		})
	}
}

func TestAgentsSetRejectsUnconfiguredHarness(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeSplitAgentConfig(t, project)

	out, err := runMainCLI(t, home, project, "agents", "set", "security", "replacement", "--harness", "unknown")
	if err == nil || !strings.Contains(out, "unknown harness") {
		t.Fatalf("unconfigured harness error = %v, output:\n%s", err, out)
	}
}

func TestProfilesListUsesExplicitHarnessTarget(t *testing.T) {
	home := t.TempDir()
	writeTestProfile(t, filepath.Join(home, ".claude"), "claude-only")
	writeTestProfile(t, filepath.Join(home, ".codex"), "codex-only")

	out, err := runProfilesCLI(t, home, "profiles", "list", "--harness", "codex")
	if err != nil {
		t.Fatalf("profiles list failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "codex-only") || strings.Contains(out, "claude-only") {
		t.Fatalf("unexpected profiles output:\n%s", out)
	}
}

func TestProfilesListSelectsOnlyInstalledSupportedTarget(t *testing.T) {
	home := t.TempDir()
	writeTestProfile(t, filepath.Join(home, ".codex"), "codex-only")

	out, err := runProfilesCLI(t, home, "profiles", "list")
	if err != nil {
		t.Fatalf("profiles list failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "codex-only") {
		t.Fatalf("unexpected profiles output:\n%s", out)
	}
}

func TestProfilesListRejectsAmbiguousInstalledTargets(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}

	out, err := runProfilesCLI(t, home, "profiles", "list")
	if err == nil {
		t.Fatalf("profiles list unexpectedly succeeded:\n%s", out)
	}
	if !strings.Contains(out, "multiple profile targets installed (claude-code, codex); use --harness explicitly") {
		t.Fatalf("unexpected error:\n%s", out)
	}
}

func TestProfilesListRequiresTargetWhenNothingInstalled(t *testing.T) {
	out, err := runProfilesCLI(t, t.TempDir(), "profiles", "list")
	if err == nil {
		t.Fatalf("profiles list unexpectedly succeeded:\n%s", out)
	}
	if !strings.Contains(out, "Claude Code or Codex installation not found; use --harness explicitly") {
		t.Fatalf("unexpected error:\n%s", out)
	}
}

func writeTestProfile(t *testing.T, baseDir, name string) {
	t.Helper()
	dir := filepath.Join(baseDir, "profiles")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte("profile"), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeSplitAgentConfig(t *testing.T, dir string) {
	t.Helper()
	cfg := &yamlconfig.HarnestConfig{
		Version:   yamlconfig.CurrentVersion,
		Harnesses: []string{"claude-code", "codex"},
		Agents: yamlconfig.AgentsBlock{
			Consilium: map[string]string{"security": "shared-security"},
			Models:    map[string]string{"security": "medium"},
		},
		Adapters: map[string]yamlconfig.AdapterSettings{
			"codex": {Agents: &yamlconfig.AgentsBlock{
				Consilium: map[string]string{"security": "codex-security"},
				Models:    map[string]string{"security": "high"},
			}},
		},
	}
	if err := yamlconfig.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
}

func runProfilesCLI(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=TestProfilesCLIHelper", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "GO_WANT_HARNEST_PROFILES_HELPER=1", "HOME="+home, "USERPROFILE="+home, "CODEX_HOME=", "CLAUDE_CONFIG_DIR=")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestProfilesCLIHelper(t *testing.T) {
	if os.Getenv("GO_WANT_HARNEST_PROFILES_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"harnest"}, os.Args[i+1:]...)
			runProfiles()
			os.Exit(0)
		}
	}
	os.Exit(2)
}

func runMainCLI(t *testing.T, home, dir string, args ...string) (string, error) {
	return runMainCLIWithInput(t, home, dir, "", args...)
}

func runMainCLIWithInput(t *testing.T, home, dir, input string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=TestMainCLIHelper", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "GO_WANT_HARNEST_MAIN_HELPER=1", "HOME="+home, "USERPROFILE="+home, "CODEX_HOME=", "CLAUDE_CONFIG_DIR=")
	cmd.Dir = dir
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestMainCLIHelper(t *testing.T) {
	if os.Getenv("GO_WANT_HARNEST_MAIN_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"harnest"}, os.Args[i+1:]...)
			main()
			os.Exit(0)
		}
	}
	os.Exit(2)
}
