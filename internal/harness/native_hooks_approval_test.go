package harness

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniilsintsov/harnest-universal/internal/rules"
)

func TestNativeHooksPersistCheckApprovalAndDetectChanges(t *testing.T) {
	requireNativeHookInstallation(t)
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell unavailable")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project := hookProject()
	project.Checks.Root = ".harnest/checks"
	project.Hooks.Rules = append(project.Hooks.Rules, "check")
	project.PolicyRules = append(project.PolicyRules, rules.Rule{
		ID: "check", Statement: "run check", Severity: rules.Required,
		Enforcement: []rules.Enforcement{{Type: "require-check", Check: "test"}},
	})
	definition := fmt.Sprintf("id: test\ncommand: %q\nargs: [scripts/check.sh]\napproved: true\n", shell)
	checkPath := filepath.Join(root, project.Checks.Root, "test.yaml")
	sourcePath := filepath.Join(root, "scripts/check.sh")
	installFile(t, checkPath, definition)
	installFile(t, sourcePath, "exit 0\n")

	var previousDigest string
	for _, change := range []string{"initial", "args", "source"} {
		switch change {
		case "args":
			installFile(t, checkPath, strings.Replace(definition, "[scripts/check.sh]", "[scripts/check.sh, changed]", 1))
		case "source":
			installFile(t, sourcePath, "exit 1\n")
		}
		if change != "initial" {
			diagnostics, err := InspectHooks(root, project)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range diagnostics {
				if diagnostic.Installed || !strings.Contains(diagnostic.Issue, "approval digest") {
					t.Fatalf("%s change accepted: %+v", change, diagnostic)
				}
			}
		}
		plan, err := PlanHooks(root, project, "/usr/local/bin/harnest")
		if err != nil {
			t.Fatal(err)
		}
		digest := ""
		configs := 0
		for _, artifact := range plan {
			if filepath.Ext(artifact.Path) != ".json" {
				continue
			}
			configs++
			var doc map[string]any
			if err := json.Unmarshal(artifact.Content, &doc); err != nil {
				t.Fatal(err)
			}
			for event, entries := range doc["hooks"].(map[string]any) {
				handler := entries.([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
				args, _, ok := hookCommand(handler, "codex")
				if !ok {
					t.Fatalf("unrecognized command: %v", handler)
				}
				flags, ok := parseFlags(args)
				if !ok {
					t.Fatalf("invalid flags: %v", args)
				}
				got := flags["checks-digest"]
				if event == "PreToolUse" {
					if got != "" {
						t.Fatal("PreToolUse command contains check approval")
					}
					continue
				}
				if len(got) != 64 || got == previousDigest || (digest != "" && digest != got) {
					t.Fatalf("%s digest=%q previous=%q other platform=%q", change, got, previousDigest, digest)
				}
				digest = got
			}
		}
		if configs != 2 {
			t.Fatalf("%s changed %d native configs, want 2", change, configs)
		}
		if _, err := ApplyHooks(plan); err != nil {
			t.Fatal(err)
		}
		if again, err := PlanHooks(root, project, "/usr/local/bin/harnest"); err != nil || len(again) != 0 {
			t.Fatalf("%s idempotency: %d artifacts, %v", change, len(again), err)
		}
		diagnostics, err := InspectHooks(root, project)
		if err != nil {
			t.Fatal(err)
		}
		for _, diagnostic := range diagnostics {
			if !diagnostic.Installed {
				t.Fatalf("%s installed config rejected: %+v", change, diagnostic)
			}
		}
		previousDigest = digest
	}
	for _, digest := range []string{"", "invalid", strings.Repeat("0", 64)} {
		doc := map[string]any{"hooks": map[string]any{
			"PreToolUse": []any{nativeEntry("codex", "PreToolUse", root, "/usr/local/bin/harnest", "")},
			"Stop":       []any{nativeEntry("codex", "Stop", root, "/usr/local/bin/harnest", digest)},
		}}
		data, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		installFile(t, filepath.Join(root, ".codex/hooks.json"), string(data))
		diagnostics, err := InspectHooks(root, project)
		if err != nil {
			t.Fatal(err)
		}
		for _, diagnostic := range diagnostics {
			if diagnostic.Platform == "codex" && (diagnostic.Installed || diagnostic.Issue == "") {
				t.Fatalf("bad approval %q accepted: %+v", digest, diagnostic)
			}
		}
	}
}

func TestParseFlagsAcceptsOnlyOptionalSHA256CheckApproval(t *testing.T) {
	legacy := []string{"--platform", "codex", "--event", "stop", "--project", t.TempDir()}
	for _, tc := range []struct {
		name  string
		extra []string
		valid bool
	}{
		{"legacy", nil, true},
		{"approval", []string{"--checks-digest", strings.Repeat("a", 64)}, true},
		{"short", []string{"--checks-digest", "abc"}, false},
		{"upper", []string{"--checks-digest", strings.Repeat("A", 64)}, false},
		{"non-hex", []string{"--checks-digest", strings.Repeat("g", 64)}, false},
		{"unknown", []string{"--other", "value"}, false},
		{"duplicate", []string{"--event", "stop"}, false},
		{"duplicate-digest", []string{"--checks-digest", strings.Repeat("a", 64), "--checks-digest", strings.Repeat("a", 64)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, valid := parseFlags(append(append([]string(nil), legacy...), tc.extra...)); valid != tc.valid {
				t.Fatalf("valid=%v, want %v", valid, tc.valid)
			}
		})
	}
}
