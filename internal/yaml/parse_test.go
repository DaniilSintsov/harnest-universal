package yaml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveUsesBundledReadmeHeader(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, &HarnestConfig{Version: CurrentVersion}); err != nil {
		t.Fatal(err)
	}
	assertBundledReadmeHeader(t, filepath.Join(dir, configFileName))
}

func TestSaveLocalUsesBundledReadmeHeader(t *testing.T) {
	dir := t.TempDir()
	if err := SaveLocal(dir, &LocalConfig{}); err != nil {
		t.Fatal(err)
	}
	assertBundledReadmeHeader(t, filepath.Join(dir, localConfigFileName))
}

func assertBundledReadmeHeader(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "Documentation: see the README bundled with this Harnest build.") {
		t.Fatalf("missing fork-neutral documentation header:\n%s", content)
	}
	if strings.Contains(content, "github.com/daniilsintsov/harnest-universal") {
		t.Fatalf("generated header points to upstream:\n%s", content)
	}
}
