package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	harnestYaml "github.com/daniilsintsov/harnest-universal/internal/yaml"
)

func TestDoctorSeparatesInstalledDisabledAndIncompleteHooks(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0755); err != nil {
		t.Fatal(err)
	}
	config := "version: 2\nrules: {root: rules}\nhooks: {rules: [protect]}\nharnesses: [codex]\n"
	rule := "id: protect\nseverity: hard\nstatement: protect\nenforcement:\n - type: protect-path\n   paths: ['config/**']\n"
	for name, contents := range map[string]string{"harnest.yaml": config, "rules/protect.yaml": rule} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := harnestYaml.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harnestYaml.Generate(dir, cfg); err != nil {
		t.Fatal(err)
	}
	report, err := Check(dir)
	if err != nil || !report.Healthy() {
		t.Fatalf("installed hook diagnostics: %#v, %v", report, err)
	}
	installed := false
	for _, item := range report.Items {
		installed = installed || strings.Contains(item.Message, "installed, execution not verified")
	}
	if !installed {
		t.Fatalf("doctor did not distinguish wiring from execution: %#v", report.Items)
	}
	disabled := false
	if err := harnestYaml.SaveLocal(dir, &harnestYaml.LocalConfig{Hooks: harnestYaml.LocalHooks{Enabled: &disabled}}); err != nil {
		t.Fatal(err)
	}
	report, err = Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range report.Items {
		found = found || strings.Contains(item.Message, "hooks disabled")
	}
	if !found {
		t.Fatalf("missing disabled diagnostic: %#v", report.Items)
	}
	path := filepath.Join(dir, ".codex", "hooks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	delete(doc["hooks"].(map[string]any), "Stop")
	data, _ = json.Marshal(doc)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	report, err = Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	incomplete := false
	for _, item := range report.Items {
		incomplete = incomplete || strings.Contains(item.Message, "hooks not installed")
		if strings.Contains(item.Message, "installed, execution not verified") {
			t.Fatalf("partial native wiring advertised as installed: %#v", report)
		}
	}
	if !incomplete {
		t.Fatalf("missing event was not diagnosed: %#v", report)
	}
}

func TestDoctorReturnsValidationErrorForUnsupportedDenyCommand(t *testing.T) {
	dir := t.TempDir()
	config := "version: 2\nrules:\n  root: .harnest/rules\nchecks:\n  root: .harnest/checks\nworkflow:\n  verify_changed: true\nagents:\n  consilium: {}\n  executing: []\nharnesses: [codex]\nsettings:\n  local_default: true\n  language: ru\n"
	rule := "id: no-push\nseverity: hard\nstatement: Never push.\nenforcement:\n  - type: deny-command\n    commands: [git push]\n"
	if err := os.MkdirAll(filepath.Join(dir, ".harnest", "rules"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "harnest.yaml"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".harnest", "rules", "no-push.yaml"), []byte(rule), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Check(dir)
	if err == nil || !strings.Contains(err.Error(), "unsupported enforcement") {
		t.Fatalf("doctor error = %v, want unsupported enforcement", err)
	}
}

func TestDoctorReportsUnselectedHardRuleAsManualVerification(t *testing.T) {
	dir := t.TempDir()
	config := "version: 2\nrules:\n  root: .harnest/rules\nchecks:\n  root: .harnest/checks\nworkflow:\n  verify_changed: true\nagents:\n  consilium: {}\n  executing: []\nharnesses: [claude-code, codex]\nsettings:\n  local_default: true\n  language: ru\n"
	rule := "id: protect-prod\nseverity: hard\nstatement: Protect production.\nenforcement:\n  - type: protect-path\n    paths: [deploy/**]\n"
	if err := os.MkdirAll(filepath.Join(dir, ".harnest", "rules"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "harnest.yaml"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".harnest", "rules", "protect.yaml"), []byte(rule), 0644); err != nil {
		t.Fatal(err)
	}

	report, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Healthy() {
		t.Fatal("unselected rule must not invalidate selected hook bindings")
	}
	joined := ""
	for _, item := range report.Items {
		joined += item.Message + "\n"
	}
	if !strings.Contains(joined, "manual verification") {
		t.Fatalf("doctor did not explain enforcement gap: %s", joined)
	}
}
