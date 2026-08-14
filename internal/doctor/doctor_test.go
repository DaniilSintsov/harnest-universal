package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestDoctorRejectsHardRuleBackedOnlyByManualVerify(t *testing.T) {
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
	if report.Healthy() {
		t.Fatal("doctor treated manual verify as mechanical hard-rule enforcement")
	}
	joined := ""
	for _, item := range report.Items {
		joined += item.Message + "\n"
	}
	if !strings.Contains(joined, "manual verification") {
		t.Fatalf("doctor did not explain enforcement gap: %s", joined)
	}
}
