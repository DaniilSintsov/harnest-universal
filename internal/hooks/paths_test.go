package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daniilsintsov/harnest-universal/internal/rules"
)

func TestCanonicalNewPathWithForwardSlashes(t *testing.T) {
	root, err := canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, err := canonical(filepath.ToSlash(root) + "/new/nested/file.go")
	if want := filepath.Join(root, "new", "nested", "file.go"); err != nil || got != want {
		t.Fatalf("canonical new path = %q, %v; want %q", got, err, want)
	}
}

func TestNormalizeChangesRejectsEscapeAndKeepsSymlinkAlias(t *testing.T) {
	root := t.TempDir()
	root, err := canonical(root)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := normalizeChanges(root, root, []rules.Change{{Path: "escape/new.go", Operation: "create"}}); err == nil {
		t.Fatal("symlink escape was accepted")
	}

	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	got, err := normalizeChanges(root, root, []rules.Change{{Path: "alias/new.go", Operation: "create"}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"alias/new.go": true, "real/new.go": true}
	if len(got) != 2 || !want[got[0].Path] || !want[got[1].Path] {
		t.Fatalf("aliases = %#v, want alias and resolved path", got)
	}
}

func TestPatchChangesHandlesMultipleFilesAndMove(t *testing.T) {
	patch := "*** Begin Patch\n*** Add File: new.go\n+package p\n*** Update File: old.go\n*** Move to: moved.go\n@@\n-old\n+new\n*** End Patch"
	got, err := patchChanges(t.TempDir(), patch)
	if err != nil {
		t.Fatal(err)
	}
	want := map[rules.Change]bool{
		{Path: "new.go", Operation: "create"}: true,
		{Path: "old.go", Operation: "move"}:   true,
		{Path: "moved.go", Operation: "move"}: true,
	}
	if len(got) != len(want) {
		t.Fatalf("changes = %#v", got)
	}
	for _, change := range got {
		if !want[change] {
			t.Fatalf("unexpected change %#v", change)
		}
	}
}

func TestNormalizeResolvesSymlinkBeforeParentTraversal(t *testing.T) {
	root, err := canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "protected", "nested")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	changes, err := normalizeChanges(root, root, []rules.Change{{Path: "alias/../new.go", Operation: "create"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range changes {
		if change.Path == "protected/new.go" {
			return
		}
	}
	t.Fatalf("symlink traversal lost actual protected path: %#v", changes)
}

func TestPatchChangesRejectsMalformedSupportedInput(t *testing.T) {
	for _, patch := range []string{"", "*** Begin Patch\n*** End Patch", "*** Begin Patch\n*** Update File: x\n*** End Patch"} {
		if _, err := patchChanges(t.TempDir(), patch); err == nil {
			t.Fatalf("accepted malformed patch %q", patch)
		}
	}
}
