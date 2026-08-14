package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	assets "github.com/daniilsintsov/harnest-universal"
	"github.com/daniilsintsov/harnest-universal/internal/harness"
	"github.com/daniilsintsov/harnest-universal/internal/managedfile"
	"github.com/daniilsintsov/harnest-universal/internal/profile"
)

const (
	profileStateVersion  = 1
	profileStateFilename = ".harnest-state.json"
)

type profileState struct {
	Version  int               `json:"version"`
	Profiles map[string]string `json:"profiles"`
}

// Known unmodified profiles shipped by upstream v0.12.0 and the previous
// Universal preview. Exact hashes let the first managed install upgrade them
// without treating user-edited files as stock.
var legacyBuiltinHashes = map[string][]string{
	"bug-hunting":      {"7a831fa430f9cdc5f0b66b2fb6a545354b737825f0baf7c825b6788ff675075a", "55476756908925b8db12142d63cf83024a6310f8b7f48af1faa5b4d1e270f6d6"},
	"business-feature": {"35d217833bb18d908b3719ff585aeaeacda13fee64e10e624675ebb6af75012d", "e3de4848aaa5bbb124fb761492683a47b033768ac3614563ce2daee9ea254b75"},
	"e2e-testing":      {"3569e4ac1de7a2ae006b12c68186e5ef9f84ea76b3bed203a8b7662bb42e1e12", "da7a5373a6ab01d56c11bbece728c1c1bd009cf6a727381c449287722fdc3e50"},
	"refactoring":      {"936d19c7e17dd9b638e1529044b6c4589fb4cd4691ab5c2ca0ac996878f05447"},
	"research":         {"4b26a81cc5dde5018a58326abc8e42602b37973104244d13da10bd44360143b8"},
}

var retiredBuiltinHashes = map[string][]string{
	"e2e-authoring": {"35b568a83a67bd4fe2da8010fc459043074d75d1df38b2dc37be64086933ec81", "87bf16275de8576bd584d8f780c34976f9d5820ce0c96b9ccb3a3d463366b373"},
}

var retiredProfileRouterHashes = []string{
	"e488fb3324b01031f3656ae8a744e9f98fe4b4720692fb85c0b78786503b9fd4",
}

// InstallAll installs target-adapted profiles and global config for the given harness.
func InstallAll(harnessName string) error {
	globalDir, err := harness.GlobalDir(harnessName)
	if err != nil {
		return err
	}

	if err := installProfiles(harnessName, globalDir); err != nil {
		return err
	}

	configPath, err := harness.GlobalConfigPath(harnessName)
	if err != nil {
		return err
	}

	fmt.Printf("\nInstalling global config → %s ...\n", configPath)
	if err := installGlobalConfig(harnessName, globalDir, configPath); err != nil {
		return fmt.Errorf("installing global config: %w", err)
	}
	if err := installBundledSkills(globalDir); err != nil {
		return fmt.Errorf("installing skills: %w", err)
	}
	if err := cleanupLegacyProfileRouter(harnessName, globalDir); err != nil {
		return fmt.Errorf("cleaning legacy profile router: %w", err)
	}

	return nil
}

func installProfiles(harnessName, globalDir string) error {
	state, err := loadProfileState(globalDir)
	if err != nil {
		return err
	}
	next := profileState{Version: profileStateVersion, Profiles: map[string]string{}}

	fmt.Println("Installing profiles...")
	for _, name := range profile.BuiltinNames() {
		content, _ := profile.BuiltinContentFor(name, harnessName)
		path := filepath.Join(globalDir, "profiles", name+".md")
		existing, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			if err := profile.InstallToFor(name, globalDir, harnessName); err != nil {
				return fmt.Errorf("installing profile %s: %w", name, err)
			}
			next.Profiles[name] = contentHash([]byte(content))
			continue
		}
		if err != nil {
			return fmt.Errorf("reading profile %s: %w", name, err)
		}

		existingHash := contentHash(existing)
		if string(existing) == content {
			next.Profiles[name] = existingHash
			continue
		}
		if state.Profiles[name] == existingHash || hashListed(existingHash, legacyBuiltinHashes[name]) {
			backupPath, err := writeHashedBackup(path, existing)
			if err != nil {
				return fmt.Errorf("backing up profile %s: %w", name, err)
			}
			if err := managedfile.WriteAtomic(path, []byte(content), 0600); err != nil {
				return fmt.Errorf("upgrading profile %s: %w", name, err)
			}
			fmt.Printf("  → upgraded managed %s.md; backup: %s\n", name, backupPath)
			next.Profiles[name] = contentHash([]byte(content))
			continue
		}

		migrated, backupPath, err := profile.MigrateInFor(name, globalDir, harnessName)
		if err != nil {
			return fmt.Errorf("migrating profile %s: %w", name, err)
		}
		if migrated {
			fmt.Printf("  → migrated modified %s.md for %s; backup: %s\n", name, harnessName, backupPath)
		}
		repaired, _, err := profile.RepairBuiltinMetaInFor(name, globalDir, harnessName)
		if err != nil {
			return fmt.Errorf("repairing profile %s meta: %w", name, err)
		}
		if repaired {
			fmt.Printf("  → repaired missing Meta in modified %s.md and kept custom body\n", name)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(after) == content {
			next.Profiles[name] = contentHash(after)
		} else if !migrated && !repaired {
			fmt.Printf("  → preserved modified %s.md; review legacy instructions manually\n", name)
		}
	}

	if err := retireObsoleteProfiles(globalDir, state); err != nil {
		return err
	}
	return saveProfileState(globalDir, next)
}

func loadProfileState(globalDir string) (profileState, error) {
	state := profileState{Version: profileStateVersion, Profiles: map[string]string{}}
	data, err := os.ReadFile(profileStatePath(globalDir))
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("parsing profile state: %w", err)
	}
	if state.Version != profileStateVersion {
		return state, fmt.Errorf("unsupported profile state version %d", state.Version)
	}
	if state.Profiles == nil {
		state.Profiles = map[string]string{}
	}
	return state, nil
}

func saveProfileState(globalDir string, state profileState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := managedfile.WriteAtomic(profileStatePath(globalDir), data, 0600); err != nil {
		return fmt.Errorf("writing profile state: %w", err)
	}
	return nil
}

func profileStatePath(globalDir string) string {
	return filepath.Join(globalDir, "profiles", profileStateFilename)
}

func retireObsoleteProfiles(globalDir string, state profileState) error {
	for name, hashes := range retiredBuiltinHashes {
		path := filepath.Join(globalDir, "profiles", name+".md")
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		hash := contentHash(data)
		if state.Profiles[name] != hash && !hashListed(hash, hashes) {
			fmt.Printf("  → preserved modified retired profile %s.md\n", name)
			continue
		}
		backupPath, err := retireFile(path, data)
		if err != nil {
			return err
		}
		fmt.Printf("  → retired %s.md; backup: %s\n", name, backupPath)
	}
	return nil
}

func cleanupLegacyProfileRouter(harnessName, globalDir string) error {
	if harnessName != "claude-code" {
		return nil
	}
	path := filepath.Join(globalDir, "agents", "profile-router.md")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !hashListed(contentHash(data), retiredProfileRouterHashes) {
		fmt.Println("  → preserved modified legacy agents/profile-router.md")
		return nil
	}
	backupPath, err := retireFile(path, data)
	if err != nil {
		return err
	}
	fmt.Printf("  → retired legacy profile-router; backup: %s\n", backupPath)
	return nil
}

func writeHashedBackup(path string, data []byte) (string, error) {
	backupPath := path + ".pre-" + contentHash(data)[:12] + ".bak"
	if existing, err := os.ReadFile(backupPath); err == nil {
		if !bytes.Equal(existing, data) {
			return "", fmt.Errorf("backup already exists with different content: %s", backupPath)
		}
		return backupPath, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return backupPath, managedfile.WriteAtomic(backupPath, data, 0600)
}

func retireFile(path string, data []byte) (string, error) {
	backupPath := path + ".retired-" + contentHash(data)[:12] + ".bak"
	if existing, err := os.ReadFile(backupPath); err == nil {
		if !bytes.Equal(existing, data) {
			return "", fmt.Errorf("retirement backup already exists with different content: %s", backupPath)
		}
		return backupPath, os.Remove(path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return backupPath, os.Rename(path, backupPath)
}

func contentHash(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func hashListed(hash string, hashes []string) bool {
	for _, known := range hashes {
		if hash == known {
			return true
		}
	}
	return false
}

func installBundledSkills(globalDir string) error {
	return fs.WalkDir(assets.Skills, "skills", func(source string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel("skills", source)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("invalid embedded skill path %q", source)
		}
		data, err := assets.Skills.ReadFile(source)
		if err != nil {
			return err
		}
		target := filepath.Join(globalDir, "skills", rel)
		if existing, err := os.ReadFile(target); err == nil {
			if bytes.Equal(existing, data) {
				return nil
			}
			if _, err := writeHashedBackup(target, existing); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		return managedfile.WriteAtomic(target, data, 0600)
	})
}

func installGlobalConfig(harnessName, dir, path string) error {
	if err := managedfile.Upsert(path, "harnest", globalTemplateFor(harnessName, dir)); err != nil {
		return err
	}
	fmt.Printf("  → updated managed block in %s\n", path)
	return nil
}
