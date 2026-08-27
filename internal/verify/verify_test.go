package verify

import (
	"errors"
	"reflect"
	"strings"
	"testing"
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
	original := gitOutput
	t.Cleanup(func() { gitOutput = original })
	gitOutput = func(_ string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch joined {
		case "merge-base -- origin/main HEAD":
			return []byte("abc123\n"), nil
		case "diff --name-only abc123 HEAD --":
			return []byte("committed.go\n"), nil
		case "diff --name-only --cached":
			return []byte("staged.go\n"), nil
		case "diff --name-only":
			return []byte("working.go\n"), nil
		case "ls-files --others --exclude-standard":
			return []byte("new.go\n"), nil
		default:
			return nil, errors.New("unexpected command: " + joined)
		}
	}

	got, err := ChangedFilesSince(t.TempDir(), "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"committed.go", "new.go", "staged.go", "working.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedFilesSince() = %v, want %v", got, want)
	}
}

func TestChangedFilesAutoDetectsRemoteDefaultBranch(t *testing.T) {
	original := gitOutput
	t.Cleanup(func() { gitOutput = original })
	gitOutput = func(_ string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch joined {
		case "symbolic-ref --quiet --short refs/remotes/origin/HEAD":
			return []byte("origin/trunk\n"), nil
		case "merge-base -- origin/trunk HEAD":
			return []byte("base123\n"), nil
		case "diff --name-only base123 HEAD --":
			return []byte("committed.go\n"), nil
		case "diff --name-only --cached", "diff --name-only", "ls-files --others --exclude-standard":
			return nil, nil
		default:
			return nil, errors.New("unexpected command: " + joined)
		}
	}

	got, err := ChangedFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"committed.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedFiles() = %v, want %v", got, want)
	}
}

func TestChangedFilesFallsBackBeforeFirstCommit(t *testing.T) {
	original := gitOutput
	t.Cleanup(func() { gitOutput = original })
	gitOutput = func(_ string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch joined {
		case "symbolic-ref --quiet --short refs/remotes/origin/HEAD", "merge-base -- origin/main HEAD", "merge-base -- origin/master HEAD", "merge-base -- main HEAD", "merge-base -- master HEAD":
			return nil, errors.New("no base")
		case "diff --name-only --cached":
			return []byte("staged.go\n"), nil
		case "diff --name-only":
			return []byte("working.go\n"), nil
		case "ls-files --others --exclude-standard":
			return []byte("new.go\n"), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}

	got, err := ChangedFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"new.go", "staged.go", "working.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedFiles() = %v, want %v", got, want)
	}
}
