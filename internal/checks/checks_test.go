package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsUnapprovedCommand(t *testing.T) {
	err := Run(t.TempDir(), Check{ID: "dangerous", Command: "false"}, nil)
	if err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("expected approval error, got %v", err)
	}
}

func TestRunPassesChangedFiles(t *testing.T) {
	t.Setenv("GO_WANT_HARNEST_CHECK_HELPER", "1")
	output := filepath.Join(t.TempDir(), "changed.txt")
	check := Check{
		ID:       "helper",
		Command:  os.Args[0],
		Args:     []string{"-test.run=TestCheckHelper", "--", output},
		Approved: true,
	}
	if err := Run(t.TempDir(), check, []string{"admin/a.ts", "backend/b.go"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "admin/a.ts\nbackend/b.go"; got != want {
		t.Fatalf("changed files = %q, want %q", got, want)
	}
}

func TestCheckHelper(t *testing.T) {
	if os.Getenv("GO_WANT_HARNEST_CHECK_HELPER") != "1" {
		return
	}
	output := os.Args[len(os.Args)-1]
	if err := os.WriteFile(output, []byte(os.Getenv("HARNEST_CHANGED_FILES")), 0o600); err != nil {
		t.Fatal(err)
	}
}
