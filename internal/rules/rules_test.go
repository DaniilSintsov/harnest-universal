package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadValidatesHardRules(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, ".harnest", "rules")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	valid := "id: protect-production\nseverity: hard\nstatement: Production files require explicit approval.\nenforcement:\n  - type: protect-path\n    paths: [deploy/**]\n"
	if err := os.WriteFile(filepath.Join(root, "production.yaml"), []byte(valid), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(dir, ".harnest/rules")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "protect-production" {
		t.Fatalf("unexpected rules: %#v", got)
	}

	invalid := Rule{ID: "semantic-only", Severity: Hard, Statement: "must hold"}
	if err := Validate(invalid); err == nil {
		t.Fatal("expected hard rule without enforcement to fail")
	}
}

func TestValidateRejectsDenyCommandForEverySeverity(t *testing.T) {
	for _, severity := range []Severity{Hard, Required, Preference} {
		t.Run(string(severity), func(t *testing.T) {
			err := Validate(Rule{
				ID:        "no-push",
				Severity:  severity,
				Statement: "Never push.",
				Enforcement: []Enforcement{{
					Type:     "deny-command",
					Commands: []string{"git push"},
				}},
			})
			if err == nil {
				t.Fatal("expected deny-command to be rejected")
			}
			for _, want := range []string{"unsupported enforcement", "protect-path", "require-check"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}
