package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniilsintsov/harnest-universal/internal/profile"
)

func TestInstallAllWritesHarnessSpecificProfiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for _, test := range []struct {
		harness   string
		dir       string
		want      []string
		forbidden []string
	}{
		{
			harness:   "claude-code",
			dir:       ".claude",
			want:      []string{"CLAUDE.md", "opus", "sonnet", "haiku"},
			forbidden: []string{"AGENTS.md", "| sol", "| terra", "| luna"},
		},
		{
			harness:   "codex",
			dir:       ".codex",
			want:      []string{"AGENTS.md", "sol", "terra", "luna"},
			forbidden: []string{"CLAUDE.md", "| opus", "| sonnet", "| haiku"},
		},
	} {
		t.Run(test.harness, func(t *testing.T) {
			if err := InstallAll(test.harness); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(home, test.dir, "profiles", "business-feature.md")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			content := string(data)
			for _, want := range test.want {
				if !strings.Contains(content, want) {
					t.Errorf("%s profile does not contain %q", test.harness, want)
				}
			}
			for _, forbidden := range test.forbidden {
				if strings.Contains(content, forbidden) {
					t.Errorf("%s profile contains %q", test.harness, forbidden)
				}
			}
		})
	}
}

func TestInstallAllMigratesClaudeProfileAlreadyInCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profilesDir := filepath.Join(home, ".codex", "profiles")
	if err := os.MkdirAll(profilesDir, 0700); err != nil {
		t.Fatal(err)
	}
	original, ok := profile.BuiltinContent("business-feature")
	if !ok {
		t.Fatal("missing business-feature")
	}
	path := filepath.Join(profilesDir, "business-feature.md")
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	if err := InstallAll("codex"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "AGENTS.md") || strings.Contains(content, "CLAUDE.md") {
		t.Fatalf("profile was not migrated:\n%s", content)
	}
	backup, err := os.ReadFile(path + ".pre-codex.bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != original {
		t.Fatal("migration backup does not match original")
	}
}

func TestInstallAllRepairsMissingMetaInModifiedBuiltinProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalDir := filepath.Join(home, ".codex")
	profilesDir := filepath.Join(globalDir, "profiles")
	if err := os.MkdirAll(profilesDir, 0700); err != nil {
		t.Fatal(err)
	}

	original := strings.Join([]string{
		"# Profile: Research",
		"",
		"## Workflow",
		"Custom body stays.",
		"",
	}, "\n")
	path := filepath.Join(profilesDir, "research.md")
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	if err := InstallAll("codex"); err != nil {
		t.Fatal(err)
	}

	repaired, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(repaired)
	for _, want := range []string{
		"## Meta",
		"**Keywords:** как устроено, как работает",
		"Custom body stays.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("installed profile missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "No Plan, no Executing, no code changes.") {
		t.Fatalf("modified profile was overwritten instead of repaired:\n%s", text)
	}

	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != original {
		t.Fatalf("backup = %q, want %q", backup, original)
	}
}

func TestInstallProfilesUpgradesPreviouslyManagedProfile(t *testing.T) {
	globalDir := filepath.Join(t.TempDir(), ".codex")
	path := filepath.Join(globalDir, "profiles", "business-feature.md")
	oldContent := []byte("previous managed profile\n")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, oldContent, 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveProfileState(globalDir, profileState{
		Version:  profileStateVersion,
		Profiles: map[string]string{"business-feature": contentHash(oldContent)},
	}); err != nil {
		t.Fatal(err)
	}

	if err := installProfiles("codex", globalDir); err != nil {
		t.Fatal(err)
	}
	want, _ := profile.BuiltinContentFor("business-feature", "codex")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatal("previously managed profile was not upgraded")
	}
	backupPath := path + ".pre-" + contentHash(oldContent)[:12] + ".bak"
	backup, err := os.ReadFile(backupPath)
	if err != nil || string(backup) != string(oldContent) {
		t.Fatalf("upgrade backup = %q, err = %v", backup, err)
	}
}

func TestInstallProfilesPreservesUserChangeAfterManagedInstall(t *testing.T) {
	globalDir := filepath.Join(t.TempDir(), ".codex")
	path := filepath.Join(globalDir, "profiles", "business-feature.md")
	previousManaged := []byte("previous managed profile\n")
	custom := []byte("# Profile: Business Feature\n\n## Meta\n\ncustom body\n")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, custom, 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveProfileState(globalDir, profileState{
		Version:  profileStateVersion,
		Profiles: map[string]string{"business-feature": contentHash(previousManaged)},
	}); err != nil {
		t.Fatal(err)
	}

	if err := installProfiles("codex", globalDir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(custom) {
		t.Fatalf("custom profile = %q, err = %v", got, err)
	}
	state, err := loadProfileState(globalDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, managed := state.Profiles["business-feature"]; managed {
		t.Fatal("modified profile remains marked as managed")
	}
}

func TestInstallProfilesUpgradesKnownLegacyProfile(t *testing.T) {
	originalHashes := legacyBuiltinHashes
	t.Cleanup(func() { legacyBuiltinHashes = originalHashes })
	legacy := []byte("known legacy profile\n")
	legacyBuiltinHashes = map[string][]string{"business-feature": {contentHash(legacy)}}

	globalDir := filepath.Join(t.TempDir(), ".claude")
	path := filepath.Join(globalDir, "profiles", "business-feature.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	if err := installProfiles("claude-code", globalDir); err != nil {
		t.Fatal(err)
	}
	want, _ := profile.BuiltinContentFor("business-feature", "claude-code")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatal("known legacy profile was not upgraded")
	}
}

func TestRetireObsoleteProfileKeepsRecoverableBackup(t *testing.T) {
	originalHashes := retiredBuiltinHashes
	t.Cleanup(func() { retiredBuiltinHashes = originalHashes })
	legacy := []byte("retired builtin\n")
	retiredBuiltinHashes = map[string][]string{"old-profile": {contentHash(legacy)}}

	globalDir := filepath.Join(t.TempDir(), ".claude")
	path := filepath.Join(globalDir, "profiles", "old-profile.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	if err := retireObsoleteProfiles(globalDir, profileState{Profiles: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("retired profile remains active: %v", err)
	}
	backupPath := path + ".retired-" + contentHash(legacy)[:12] + ".bak"
	backup, err := os.ReadFile(backupPath)
	if err != nil || string(backup) != string(legacy) {
		t.Fatalf("retirement backup = %q, err = %v", backup, err)
	}
}

func TestRetireObsoleteProfilePreservesModifiedFile(t *testing.T) {
	originalHashes := retiredBuiltinHashes
	t.Cleanup(func() { retiredBuiltinHashes = originalHashes })
	retiredBuiltinHashes = map[string][]string{"old-profile": {contentHash([]byte("stock\n"))}}

	globalDir := filepath.Join(t.TempDir(), ".claude")
	path := filepath.Join(globalDir, "profiles", "old-profile.md")
	modified := []byte("user-modified retired profile\n")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, modified, 0600); err != nil {
		t.Fatal(err)
	}
	if err := retireObsoleteProfiles(globalDir, profileState{Profiles: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(modified) {
		t.Fatalf("modified retired profile = %q, err = %v", got, err)
	}
}

func TestCleanupLegacyProfileRouterKeepsRecoverableBackup(t *testing.T) {
	originalHashes := retiredProfileRouterHashes
	t.Cleanup(func() { retiredProfileRouterHashes = originalHashes })
	legacy := []byte("legacy router\n")
	retiredProfileRouterHashes = []string{contentHash(legacy)}

	globalDir := filepath.Join(t.TempDir(), ".claude")
	path := filepath.Join(globalDir, "agents", "profile-router.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	if err := cleanupLegacyProfileRouter("claude-code", globalDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy router remains active: %v", err)
	}
	backupPath := path + ".retired-" + contentHash(legacy)[:12] + ".bak"
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupLegacyProfileRouterPreservesModifiedFile(t *testing.T) {
	originalHashes := retiredProfileRouterHashes
	t.Cleanup(func() { retiredProfileRouterHashes = originalHashes })
	retiredProfileRouterHashes = []string{contentHash([]byte("stock router\n"))}

	globalDir := filepath.Join(t.TempDir(), ".claude")
	path := filepath.Join(globalDir, "agents", "profile-router.md")
	modified := []byte("user-modified router\n")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, modified, 0600); err != nil {
		t.Fatal(err)
	}
	if err := cleanupLegacyProfileRouter("claude-code", globalDir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(modified) {
		t.Fatalf("modified router = %q, err = %v", got, err)
	}
}
