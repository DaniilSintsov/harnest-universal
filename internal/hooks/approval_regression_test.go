package hooks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/daniilsintsov/harnest-universal/internal/checks"
)

func TestStopBindsApprovalBeforeExecutingChangedCheck(t *testing.T) {
	for _, mutation := range []string{"missing digest", "wrong digest", "command", "args", "timeout", "direct source", "argument source", "deleted source", "declared source"} {
		t.Run(mutation, func(t *testing.T) {
			if runtime.GOOS == "windows" && (mutation == "direct source" || mutation == "command") {
				t.Skip("Windows cannot execute POSIX shebang scripts directly")
			}
			shell, err := exec.LookPath("sh")
			if err != nil {
				t.Skip("shell fixture unavailable")
			}
			check := fmt.Sprintf("id: shared\ncommand: %q\nargs: [check.sh]\napproved: true\ntimeout_seconds: 5\n", shell)
			if mutation == "direct source" {
				check = "id: shared\ncommand: ./check.sh\napproved: true\ntimeout_seconds: 5\n"
			}
			if mutation == "declared source" {
				check += "sources: [dependency.sh]\n"
			}
			root := hookFixture(t, []string{"required"}, check, "id: required\nseverity: required\nstatement: test\nscope:\n  paths: ['*.go']\nenforcement:\n  - type: require-check\n    check: shared\n")
			write := func(name, content string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0755); err != nil {
					t.Fatal(err)
				}
			}
			source := "#!/bin/sh\nprintf run > marker\n"
			write("check.sh", source)
			write("dependency.sh", "# approved dependency\n")
			write("changed.go", "package changed\n")
			initGit(t, root)
			options := approvedStopOptions(t, root)
			payload := fmt.Sprintf(`{"hook_event_name":"Stop","cwd":%q,"stop_hook_active":false}`, root)
			evaluate := func() Result {
				return Evaluate(context.Background(), options, strings.NewReader(payload))
			}
			if got := evaluate(); got.Status != "passed" {
				t.Fatalf("approved definition: %#v", got)
			}
			if err := os.Remove(filepath.Join(root, "marker")); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "missing digest":
				options.ChecksDigest = ""
			case "wrong digest":
				options.ChecksDigest = strings.Repeat("0", 64)
			case "command":
				check = strings.Replace(check, fmt.Sprintf("command: %q", shell), "command: ./check.sh", 1)
			case "args":
				check = strings.Replace(check, "args: [check.sh]", "args: [check.sh, altered]", 1)
			case "timeout":
				check = strings.Replace(check, "timeout_seconds: 5", "timeout_seconds: 6", 1)
			case "direct source", "argument source":
				write("check.sh", source+"# modified while approved remains true\n")
			case "deleted source":
				if err := os.Remove(filepath.Join(root, "check.sh")); err != nil {
					t.Fatal(err)
				}
			case "declared source":
				write("dependency.sh", "# altered dependency\n")
			}
			write(".harnest/checks/shared.yaml", check)
			if got := evaluate(); got.Status != "evaluation-error" || got.Output["decision"] != "block" {
				t.Fatalf("changed approval not blocked: %#v", got)
			}
			if _, err := os.Stat(filepath.Join(root, "marker")); !os.IsNotExist(err) {
				t.Fatalf("unapproved command ran: %v", err)
			}
			write("check.sh", source)
			options = approvedStopOptions(t, root)
			if got := evaluate(); got.Status != "passed" {
				t.Fatalf("renewed approval: %#v", got)
			}
		})
	}
}

func TestStopRechecksSourcesBetweenChecks(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("shell fixture unavailable")
	}
	rule := "id: %s\nseverity: required\nstatement: test\nscope:\n  paths: ['*.go']\nenforcement:\n  - type: require-check\n    check: %s\n"
	root := hookFixture(t, []string{"a", "b"}, "", fmt.Sprintf(rule, "a", "a")+"---RULE---\n"+fmt.Sprintf(rule, "b", "b"))
	files := map[string]string{
		"a.sh":       "printf 'printf unapproved > marker-b\\n' > b.sh\nprintf run > marker-a\n",
		"b.sh":       "printf approved > marker-b\n",
		"changed.go": "package changed\n",
	}
	for _, id := range []string{"a", "b"} {
		files[".harnest/checks/"+id+".yaml"] = fmt.Sprintf("id: %s\ncommand: %q\nargs: [%s.sh]\napproved: true\n", id, shell, id)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	initGit(t, root)
	var definitions []checks.Check
	for _, id := range []string{"a", "b"} {
		check, err := checks.Load(root, ".harnest/checks", id)
		if err != nil {
			t.Fatal(err)
		}
		definitions = append(definitions, check)
	}
	digest, err := checks.Digest(root, definitions)
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"hook_event_name":"Stop","cwd":%q,"stop_hook_active":false}`, root)
	got := Evaluate(context.Background(), Options{Platform: "codex", Event: "stop", Project: root, ChecksDigest: digest}, strings.NewReader(payload))
	if got.Status != "evaluation-error" || len(got.Records) != 2 || got.Records[0].Status != "passed" || got.Records[1].Status != "evaluation-error" {
		t.Fatalf("modified second check was not blocked: %#v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "marker-a")); err != nil {
		t.Fatalf("first check never ran: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "marker-b")); !os.IsNotExist(err) {
		t.Fatalf("modified second check ran: %v", err)
	}
}
