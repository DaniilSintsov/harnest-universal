package checks

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDigestBindsDefinitionAndSources(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, string, *Check)
	}{
		{"id", func(t *testing.T, root string, c *Check) { c.ID = "other" }},
		{"command", func(t *testing.T, root string, c *Check) { c.Command = "./" + approvalExecutableName("other") }},
		{"args", func(t *testing.T, root string, c *Check) { c.Args = []string{"script.sh", "--changed"} }},
		{"timeout", func(t *testing.T, root string, c *Check) { c.TimeoutSeconds++ }},
		{"approval", func(t *testing.T, root string, c *Check) { c.Approved = false }},
		{"sources", func(t *testing.T, root string, c *Check) { c.Sources = nil }},
		{"executable bytes", func(t *testing.T, root string, c *Check) {
			writeApprovalFile(t, root, approvalExecutableName("check"), "changed")
		}},
		{"script argument bytes", func(t *testing.T, root string, c *Check) { writeApprovalFile(t, root, "script.sh", "changed") }},
		{"declared source bytes", func(t *testing.T, root string, c *Check) { writeApprovalFile(t, root, "helper.sh", "changed") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, name := range []string{approvalExecutableName("check"), approvalExecutableName("other"), "script.sh", "helper.sh"} {
				writeApprovalFile(t, root, name, "original")
			}
			check := Check{ID: "test", Command: "./" + approvalExecutableName("check"), Args: []string{"script.sh"}, Sources: []string{"helper.sh"}, Approved: true, TimeoutSeconds: 5}
			before, err := Digest(root, []Check{check})
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, root, &check)
			after, err := Digest(root, []Check{check})
			if err != nil {
				t.Fatal(err)
			}
			if after == before {
				t.Fatal("changed approval input retained digest")
			}
		})
	}
}

func TestDigestOrderingAndSourceErrors(t *testing.T) {
	root := t.TempDir()
	writeApprovalFile(t, root, approvalExecutableName("check"), "original")
	t.Setenv("PATH", root)
	first := Check{ID: "a", Command: "check", Args: []string{"absent-output", "."}, Approved: true}
	second := first
	second.ID = "b"
	before, err := Digest(root, []Check{first, second})
	if err != nil {
		t.Fatal(err)
	}
	after, err := Digest(root, []Check{second, first})
	if err != nil || after != before {
		t.Fatalf("definition order changed digest: %s, %v", after, err)
	}
	if empty, err := Digest(root, nil); empty != "" || err != nil {
		t.Fatalf("empty definitions: %q, %v", empty, err)
	}
	for _, name := range []string{"missing.sh", "."} {
		first.Sources = []string{name}
		if _, err := Digest(root, []Check{first}); err == nil {
			t.Fatalf("invalid declared source %q accepted", name)
		}
	}
	first.Sources = nil
	first.Command = "missing-executable"
	if _, err := Digest(root, []Check{first}); err == nil {
		t.Fatal("missing executable accepted")
	}
}

func TestDigestBindsSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	writeApprovalFile(t, root, approvalExecutableName("first"), "same bytes")
	writeApprovalFile(t, root, approvalExecutableName("second"), "same bytes")
	link := filepath.Join(root, approvalExecutableName("check"))
	if err := os.Symlink(approvalExecutableName("first"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	check := Check{ID: "test", Command: "./" + approvalExecutableName("check"), Approved: true}
	before, err := Digest(root, []Check{check})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(approvalExecutableName("second"), link); err != nil {
		t.Fatal(err)
	}
	after, err := Digest(root, []Check{check})
	if err != nil || after == before {
		t.Fatalf("symlink target did not invalidate digest: %s, %v", after, err)
	}
}

func TestDigestResolvesWindowsExecutableExtension(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PATHEXT resolution")
	}
	t.Setenv("PATHEXT", ".EXE")
	for _, absolute := range []bool{false, true} {
		root := t.TempDir()
		writeApprovalFile(t, root, "check", "decoy")
		writeApprovalFile(t, root, "check.exe", "approved executable")
		command := "./check"
		if absolute {
			command = filepath.Join(root, "check")
		}
		definitions := []Check{{ID: "test", Command: command, Approved: true}}
		before, err := Digest(root, definitions)
		if err != nil {
			t.Fatal(err)
		}
		writeApprovalFile(t, root, "check", "changed decoy")
		if digest, err := Digest(root, definitions); err != nil || digest != before {
			t.Fatalf("non-executable changed approval: %s, %v", command, err)
		}
		writeApprovalFile(t, root, "check.exe", "changed executable")
		if digest, err := Digest(root, definitions); err != nil || digest == before {
			t.Fatalf("resolved executable did not change approval: %s, %v", command, err)
		}
	}
}

func approvalExecutableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func writeApprovalFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}
