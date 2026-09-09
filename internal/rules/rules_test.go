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

func TestMatchesChangeUsesSamePathAndOperation(t *testing.T) {
	scope := Scope{Paths: []string{"src/**"}, Operations: []string{"update"}}
	matched, err := MatchesChange(scope, []string{"src/protected/**"}, Change{Path: "src/protected/app.go", Operation: "update"})
	if err != nil || !matched {
		t.Fatalf("MatchesChange() = %v, %v", matched, err)
	}
	matched, err = MatchesChange(scope, []string{"config/**"}, Change{Path: "src/protected/app.go", Operation: "update"})
	if err != nil || matched {
		t.Fatalf("different enforcement path matched: %v, %v", matched, err)
	}
	matched, err = MatchesChange(scope, nil, Change{Path: "src/protected/app.go", Operation: "create"})
	if err != nil || matched {
		t.Fatalf("wrong operation matched: %v, %v", matched, err)
	}
}

func TestSelectHooksRejectsInvalidSelection(t *testing.T) {
	rule := Rule{ID: "semantic", Severity: Preference, Statement: "review", Enforcement: []Enforcement{{Type: "require-check", Check: "review"}}}
	if _, err := SelectHooks([]Rule{rule}, []string{"semantic"}); err == nil {
		t.Fatal("preference hook was accepted")
	}
	domainOnly := Rule{ID: "backend", Severity: Required, Statement: "test", Scope: Scope{Domains: []string{"backend"}}, Enforcement: []Enforcement{{Type: "require-check", Check: "test"}}}
	if _, err := SelectHooks([]Rule{domainOnly}, []string{"backend"}); err == nil {
		t.Fatal("domain-only hook was accepted")
	}
}

func TestMatchPathsRejectsInvalidGlob(t *testing.T) {
	if _, err := MatchPaths([]string{"src/["}, "src/app.go"); err == nil {
		t.Fatal("invalid glob was accepted")
	}
}
