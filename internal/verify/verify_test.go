package verify

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/daniilsintsov/harnest-universal/internal/rules"
)

func TestAppliesSupportsRecursiveDirectoryScope(t *testing.T) {
	if !applies([]string{"deploy/**"}, []string{"deploy/prod/app.yaml"}) {
		t.Fatal("recursive scope did not match")
	}
	if applies([]string{"src/*.go"}, []string{"src/nested/app.go"}) {
		t.Fatal("single-star scope matched nested path")
	}
}

func TestAppliesSupportsDoubleStarAtAnyDepth(t *testing.T) {
	for _, file := range []string{"src/app.ts", "src/ui/app.ts", "src/a/b/app.ts"} {
		if !applies([]string{"src/**/*.ts"}, []string{file}) {
			t.Fatalf("recursive scope did not match %s", file)
		}
	}
	if applies([]string{"src/**/*.ts"}, []string{"src/ui/app.js"}) {
		t.Fatal("recursive scope matched wrong extension")
	}
}

func TestChangedFilesSinceIncludesCommittedStagedWorkingAndUntracked(t *testing.T) {
	run := func(_ context.Context, dir string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch joined {
		case "rev-parse --show-toplevel":
			return []byte(dir + "\n"), nil
		case "merge-base -- origin/main HEAD":
			return []byte("abc123\n"), nil
		case "diff --no-relative --no-ext-diff --find-renames --name-status -z abc123 HEAD --":
			return []byte("M\x00committed.go\x00"), nil
		case "diff --no-relative --no-ext-diff --find-renames --name-status -z --cached":
			return []byte("M\x00staged.go\x00"), nil
		case "diff --no-relative --no-ext-diff --find-renames --name-status -z":
			return []byte("M\x00working.go\x00"), nil
		case "ls-files -z --full-name --others --exclude-standard":
			return []byte("new.go\x00"), nil
		default:
			return nil, errors.New("unexpected command: " + joined)
		}
	}

	changes, _, err := discoverChanges(context.Background(), t.TempDir(), "origin/main", run)
	got := uniquePaths(changes)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"committed.go", "new.go", "staged.go", "working.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedFilesSince() = %v, want %v", got, want)
	}
}

func TestChangedFilesAutoDetectsRemoteDefaultBranch(t *testing.T) {
	run := func(_ context.Context, dir string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch joined {
		case "rev-parse --show-toplevel":
			return []byte(dir + "\n"), nil
		case "symbolic-ref --quiet --short refs/remotes/origin/HEAD":
			return []byte("origin/trunk\n"), nil
		case "merge-base -- origin/trunk HEAD":
			return []byte("base123\n"), nil
		case "diff --no-relative --no-ext-diff --find-renames --name-status -z base123 HEAD --":
			return []byte("M\x00committed.go\x00"), nil
		case "diff --no-relative --no-ext-diff --find-renames --name-status -z --cached", "diff --no-relative --no-ext-diff --find-renames --name-status -z", "ls-files -z --full-name --others --exclude-standard":
			return nil, nil
		default:
			return nil, errors.New("unexpected command: " + joined)
		}
	}

	changes, _, err := discoverChanges(context.Background(), t.TempDir(), "", run)
	got := uniquePaths(changes)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"committed.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedFiles() = %v, want %v", got, want)
	}
}

func TestChangedFilesFallsBackBeforeFirstCommit(t *testing.T) {
	run := func(_ context.Context, dir string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch joined {
		case "rev-parse --show-toplevel":
			return []byte(dir + "\n"), nil
		case "symbolic-ref --quiet --short refs/remotes/origin/HEAD", "merge-base -- origin/main HEAD", "merge-base -- origin/master HEAD", "merge-base -- main HEAD", "merge-base -- master HEAD":
			return nil, errors.New("no base")
		case "diff --no-relative --no-ext-diff --find-renames --name-status -z --cached":
			return []byte("M\x00staged.go\x00"), nil
		case "diff --no-relative --no-ext-diff --find-renames --name-status -z":
			return []byte("M\x00working.go\x00"), nil
		case "ls-files -z --full-name --others --exclude-standard":
			return []byte("new.go\x00"), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}

	changes, _, err := discoverChanges(context.Background(), t.TempDir(), "", run)
	got := uniquePaths(changes)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"new.go", "staged.go", "working.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedFiles() = %v, want %v", got, want)
	}
}

func TestDiscoverChangesPreservesRenamePathsAndNewlines(t *testing.T) {
	run := func(_ context.Context, dir string, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "rev-parse --show-toplevel":
			return []byte(dir + "\n"), nil
		case "merge-base -- main HEAD":
			return []byte("base\n"), nil
		case "diff --no-relative --no-ext-diff --find-renames --name-status -z base HEAD --":
			return []byte("R100\x00old\nname.go\x00new name.go\x00"), nil
		case "diff --no-relative --no-ext-diff --find-renames --name-status -z --cached", "diff --no-relative --no-ext-diff --find-renames --name-status -z", "ls-files -z --full-name --others --exclude-standard":
			return nil, nil
		default:
			return nil, errors.New("unexpected command")
		}
	}
	got, base, err := discoverChanges(context.Background(), t.TempDir(), "main", run)
	if err != nil {
		t.Fatal(err)
	}
	want := []rules.Change{{Path: "new name.go", Operation: "move"}, {Path: "old\nname.go", Operation: "move"}}
	if base != "base" || !reflect.DeepEqual(got, want) {
		t.Fatalf("DiscoverChanges() = %#v, %q, want %#v, base", got, base, want)
	}
}

func TestDiscoverChangesRejectsPartialDiffFailure(t *testing.T) {
	run := func(_ context.Context, dir string, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "rev-parse --show-toplevel":
			return []byte(dir + "\n"), nil
		case "symbolic-ref --quiet --short refs/remotes/origin/HEAD", "merge-base -- origin/main HEAD", "merge-base -- origin/master HEAD", "merge-base -- main HEAD", "merge-base -- master HEAD":
			return nil, errors.New("no base")
		case "diff --no-relative --no-ext-diff --find-renames --name-status -z --cached":
			return []byte("M\x00staged.go\x00"), nil
		case "diff --no-relative --no-ext-diff --find-renames --name-status -z":
			return nil, errors.New("worktree unavailable")
		default:
			return nil, errors.New("unexpected command")
		}
	}
	if _, _, err := discoverChanges(context.Background(), t.TempDir(), "", run); err == nil {
		t.Fatal("partial diff failure was ignored")
	}
}

func TestParseNameStatusRejectsMalformedNULRecords(t *testing.T) {
	for _, output := range [][]byte{
		[]byte("M\x00file.go"),
		[]byte("M\x00\x00"),
		[]byte("M100\x00file.go\x00"),
		[]byte("Rfoo\x00old\x00new\x00"),
		[]byte("R101\x00old\x00new\x00"),
		[]byte("R100\x00old\x00"),
	} {
		if _, err := parseNameStatus(output); err == nil {
			t.Fatalf("accepted malformed output %q", output)
		}
	}
}

func TestDiscoverChangesUsesRegisteredSubprojectRoot(t *testing.T) {
	root := t.TempDir()
	subproject := filepath.Join(root, "services", "api")
	sibling := filepath.Join(root, "services", "web")
	for _, dir := range []string{subproject, sibling} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{filepath.Join(subproject, "base.go"), filepath.Join(sibling, "base.go")} {
		if err := os.WriteFile(name, []byte("base\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	gitTest(t, root, "init", "-q")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "-c", "user.name=Harnest Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	if err := os.WriteFile(filepath.Join(subproject, "base.go"), []byte("api\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "base.go"), []byte("web\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", ".")
	gitTest(t, root, "-c", "user.name=Harnest Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "branch changes")
	got, _, err := DiscoverChanges(subproject, "HEAD~1")
	if err != nil {
		t.Fatal(err)
	}
	want := []rules.Change{{Path: "base.go", Operation: "update"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subproject changes = %#v, want %#v", got, want)
	}

	if err := os.Rename(filepath.Join(subproject, "base.go"), filepath.Join(sibling, "moved.go")); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", "-A")
	got, _, err = DiscoverChanges(subproject, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	want = []rules.Change{{Path: "base.go", Operation: "move"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("boundary rename = %#v, want %#v", got, want)
	}
}

func gitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
