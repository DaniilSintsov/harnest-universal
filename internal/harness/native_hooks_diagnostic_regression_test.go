package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniilsintsov/harnest-universal/internal/rules"
)

func TestInspectHooksRequiresExactEventsAndCompleteJSON(t *testing.T) {
	for _, platform := range []string{"claude-code", "codex"} {
		for _, tc := range []struct {
			name      string
			events    []string
			suffix    string
			installed bool
		}{
			{name: "stop", events: []string{"Stop"}, installed: true},
			{name: "wrong event", events: []string{"PreToolUse"}},
			{name: "missing event"},
			{name: "extra event", events: []string{"PreToolUse", "Stop"}},
			{name: "duplicate event", events: []string{"Stop", "Stop"}},
			{name: "trailing text", events: []string{"Stop"}, suffix: "garbage"},
			{name: "trailing value", events: []string{"Stop"}, suffix: "{}"},
			{name: "trailing whitespace", events: []string{"Stop"}, suffix: "\n \t", installed: true},
		} {
			t.Run(platform+"/"+tc.name, func(t *testing.T) {
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				project := hookProject()
				project.PolicyRules[0].Severity = rules.Required
				project.PolicyRules[0].Enforcement = []rules.Enforcement{{Type: "require-check", Check: "test"}}
				hooks := map[string][]any{}
				for _, event := range tc.events {
					hooks[event] = append(hooks[event], nativeEntry(platform, event, root, "/usr/local/bin/harnest"))
				}
				data, err := json.Marshal(map[string]any{"hooks": hooks})
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, ".codex", "hooks.json")
				if platform == "claude-code" {
					path = filepath.Join(root, ".claude", "settings.local.json")
				}
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(data, tc.suffix...), 0600); err != nil {
					t.Fatal(err)
				}
				diagnostics, err := InspectHooks(root, project)
				if err != nil {
					t.Fatal(err)
				}
				for _, diagnostic := range diagnostics {
					if diagnostic.Platform != platform {
						continue
					}
					if diagnostic.Installed != tc.installed {
						t.Fatalf("diagnostic = %+v, want installed=%v", diagnostic, tc.installed)
					}
					if strings.TrimSpace(tc.suffix) != "" && !strings.Contains(diagnostic.Issue, "trailing JSON") {
						t.Fatalf("missing trailing JSON diagnostic: %+v", diagnostic)
					}
				}
			})
		}
	}
}
