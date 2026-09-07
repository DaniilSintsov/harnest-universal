package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/daniilsintsov/harnest-universal/internal/rules"
)

func TestEvaluateCaseAliases(t *testing.T) {
	root := hookFixture(t, []string{"protect"}, "", "id: protect\nseverity: hard\nstatement: protect\nenforcement:\n  - type: protect-path\n    paths: ['config/production/**', 'alias/**']\n")
	for _, dir := range []string{"config/production", "allowed"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "config/production/app.yaml"), []byte("old\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "allowed"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "CONFIG/PRODUCTION/APP.YAML"))
	if os.IsNotExist(err) {
		t.Skip("case-sensitive filesystem; alias does not name the protected file")
	}
	original, statErr := os.Stat(filepath.Join(root, "config/production/app.yaml"))
	if err != nil || statErr != nil || !os.SameFile(info, original) {
		t.Fatalf("case alias identity: %v, %v", err, statErr)
	}
	for _, platform := range []string{"claude-code", "codex"} {
		for _, test := range []struct{ name, project, cwd, path string }{
			{"existing", root, root, "CONFIG/PRODUCTION/APP.YAML"},
			{"new child", root, root, "CONFIG/PRODUCTION/new.yaml"},
			{"cwd", root, filepath.Join(root, "CONFIG"), "PRODUCTION/APP.YAML"},
			{"root", strings.ToUpper(root), root, "config/production/app.yaml"},
			{"symlink alias", root, root, "ALIAS/new.yaml"},
		} {
			t.Run(platform+"/"+test.name, func(t *testing.T) {
				tool, input := "Write", map[string]string{"file_path": test.path}
				if platform == "codex" {
					tool, input = "apply_patch", map[string]string{"command": "*** Begin Patch\n*** Add File: " + test.path + "\n+new\n*** End Patch"}
				}
				encoded, _ := json.Marshal(input)
				payload := fmt.Sprintf(`{"hook_event_name":"PreToolUse","cwd":%q,"tool_name":%q,"tool_input":%s}`, test.cwd, tool, encoded)
				got := Evaluate(context.Background(), Options{Platform: platform, Event: "pre-tool-use", Project: test.project}, strings.NewReader(payload))
				if got.Status != "violation" {
					t.Fatalf("case alias bypassed policy: %#v", got)
				}
			})
		}
	}
}

func TestCanonicalKeepsDistinctCaseSensitivePaths(t *testing.T) {
	root := t.TempDir()
	upper, lower := filepath.Join(root, "File"), filepath.Join(root, "file")
	if err := os.WriteFile(upper, []byte("upper"), 0644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(lower, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if os.IsExist(err) {
		t.Skip("case-insensitive filesystem")
	}
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	left, err := canonical(upper)
	if err != nil {
		t.Fatal(err)
	}
	right, err := canonical(lower)
	if err != nil || left == right {
		t.Fatalf("distinct paths collapsed: %q, %q, %v", left, right, err)
	}
}

func TestPatchAddUsesFilesystemAndEarlierOperations(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "existing"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, body string
		want       []rules.Change
	}{
		{"overwrite", "*** Add File: existing\n+new", []rules.Change{{Path: "existing", Operation: "update"}}},
		{"create twice", "*** Add File: new\n+one\n*** Add File: ./new\n+two", []rules.Change{{Path: "new", Operation: "create"}, {Path: "./new", Operation: "update"}}},
		{"delete then add", "*** Delete File: existing\n*** Add File: existing\n+new", []rules.Change{{Path: "existing", Operation: "delete"}, {Path: "existing", Operation: "create"}}},
		{"move then add", "*** Update File: existing\n*** Move to: moved\n@@\n-old\n+new\n*** Add File: moved\n+again\n*** Add File: existing\n+restored", []rules.Change{{Path: "moved", Operation: "move"}, {Path: "existing", Operation: "move"}, {Path: "moved", Operation: "update"}, {Path: "existing", Operation: "create"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input, _ := json.Marshal(map[string]string{"command": "*** Begin Patch\n" + test.body + "\n*** End Patch"})
			got, supported, err := toolChanges("codex", cwd, Event{ToolName: "apply_patch", ToolInput: input})
			if err != nil || !supported || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("changes = %#v, supported=%v, err=%v, want %#v", got, supported, err, test.want)
			}
		})
	}
}

func TestPatchDeletingSymlinkDoesNotDeleteTarget(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "target"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(cwd, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	patch := "*** Begin Patch\n*** Delete File: alias\n*** Add File: target\n+new\n*** Add File: alias\n+separate\n*** End Patch"
	input, _ := json.Marshal(map[string]string{"command": patch})
	got, _, err := toolChanges("codex", cwd, Event{ToolName: "apply_patch", ToolInput: input})
	want := []rules.Change{{Path: "alias", Operation: "delete"}, {Path: "target", Operation: "update"}, {Path: "alias", Operation: "create"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("changes = %#v, err=%v, want %#v", got, err, want)
	}
}

func TestPatchRejectsAmbiguousNewCaseSpellings(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"command": "*** Begin Patch\n*** Add File: new\n+one\n*** Add File: NEW\n+two\n*** End Patch"})
	if _, _, err := toolChanges("codex", t.TempDir(), Event{ToolName: "apply_patch", ToolInput: input}); err == nil {
		t.Fatal("new case aliases have no filesystem identity yet; must not assume separate creates")
	}
}

func TestEvaluatePatchOverwriteAndBlankContext(t *testing.T) {
	root := hookFixture(t, []string{"protect"}, "", "id: protect\nseverity: hard\nstatement: protect\nscope:\n  operations: [update, move]\nenforcement:\n  - type: protect-path\n    paths: ['sub/protected']\n")
	cwd := filepath.Join(root, "sub")
	if err := os.Mkdir(cwd, 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"protected", "allowed"} {
		if err := os.WriteFile(filepath.Join(cwd, path), []byte("line1\n\nline3\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct{ name, body, want string }{
		{"overwrite", "*** Add File: protected\n+new", "violation"},
		{"blank protected", "*** Update File: protected\n@@\n line1\n\n-line3\n+line4", "violation"},
		{"blank allowed", "*** Update File: allowed\n@@\n line1\n\n-line3\n+line4", "not-applicable"},
		{"blank move allowed", "*** Update File: allowed\n*** Move to: other\n@@\n line1\n\n-line3\n+line4", "not-applicable"},
		{"blank move protected", "*** Update File: allowed\n*** Move to: protected\n@@\n line1\n\n-line3\n+line4", "violation"},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{"hook_event_name":"PreToolUse","cwd":%q,"tool_name":"apply_patch","tool_input":{"command":%q}}`, cwd, "*** Begin Patch\n"+test.body+"\n*** End Patch")
			got := Evaluate(context.Background(), Options{Platform: "codex", Event: "pre-tool-use", Project: root}, strings.NewReader(payload))
			if got.Status != test.want {
				t.Fatalf("status = %s (%s), want %s", got.Status, got.Reason, test.want)
			}
		})
	}
}
