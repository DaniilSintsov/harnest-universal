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
	cfg := newProjectConfig(t.TempDir(), stacks, mapping.Resolve(stacks, nil, "codex"), []string{"codex"})
	if cfg.Workflow.Adaptive || cfg.Workflow.DefaultProfile != "business-feature" || cfg.Workflow.RoleSelection != "auto" {
		t.Fatalf("unexpected workflow defaults: %#v", cfg.Workflow)
	}
	if len(cfg.Agents.Consilium) == 0 {
		t.Fatal("consilium is empty")
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

func runProfilesCLI(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=TestProfilesCLIHelper", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "GO_WANT_HARNEST_PROFILES_HELPER=1", "HOME="+home)
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
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=TestMainCLIHelper", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "GO_WANT_HARNEST_MAIN_HELPER=1", "HOME="+home)
	cmd.Dir = dir
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
