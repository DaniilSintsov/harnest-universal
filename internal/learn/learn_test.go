package learn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProposeCreatesInactiveCandidate(t *testing.T) {
	dir := t.TempDir()
	path, err := Propose(dir, "prefer-small-diffs", "Предпочитать минимальные изменения.")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "learned-candidate") || !strings.Contains(filepath.ToSlash(path), "/candidates/") {
		t.Fatalf("unexpected candidate: %s\n%s", path, data)
	}
}
