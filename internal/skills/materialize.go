// Package skills materializes one portable project skill source into native
// harness locations without overwriting user-owned skill directories.
package skills

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/harness"
	"github.com/daniilsintsov/harnest-universal/internal/managedfile"
)

const ownershipFile = ".harnest-managed"

type sourceFile struct {
	rel  string
	data []byte
	mode os.FileMode
}

type skillSource struct {
	name  string
	files []sourceFile
}

type mirrorPlan struct {
	targetDir string
	source    skillSource
	stale     bool
	remove    []string
}

// Materialize mirrors portable skills into native target directories. Dry-run
// returns affected paths after full validation without changing the filesystem.
func Materialize(projectDir, sourceRoot string, targets []string, dryRun bool) ([]string, error) {
	plans, err := plan(projectDir, sourceRoot, targets)
	if err != nil {
		return nil, err
	}
	paths := plannedPaths(plans)
	if dryRun {
		return paths, nil
	}
	for _, item := range plans {
		if item.stale {
			if err := os.RemoveAll(item.targetDir); err != nil {
				return paths, fmt.Errorf("removing stale managed skill %s: %w", item.targetDir, err)
			}
			continue
		}
		if err := writeMirror(item); err != nil {
			return paths, err
		}
	}
	return paths, nil
}

func plan(projectDir, sourceRoot string, targets []string) ([]mirrorPlan, error) {
	if sourceRoot == "" {
		return nil, nil
	}
	sourceDir := sourceRoot
	if !filepath.IsAbs(sourceDir) {
		sourceDir = filepath.Join(projectDir, sourceDir)
	}
	sourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return nil, err
	}
	sources, err := loadSources(sourceDir)
	if err != nil {
		return nil, err
	}

	var plans []mirrorPlan
	seenRoots := map[string]bool{}
	for _, target := range targets {
		rel, err := harness.ProjectSkillsDir(target)
		if err != nil {
			return nil, err
		}
		targetRoot, err := filepath.Abs(filepath.Join(projectDir, rel))
		if err != nil {
			return nil, err
		}
		if targetRoot == sourceDir || seenRoots[targetRoot] {
			continue
		}
		seenRoots[targetRoot] = true
		rootPlans, err := planRoot(targetRoot, sources)
		if err != nil {
			return nil, fmt.Errorf("planning project skills for %s: %w", target, err)
		}
		plans = append(plans, rootPlans...)
	}
	return plans, nil
}

func loadSources(root string) ([]skillSource, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sources []skillSource
	for _, entry := range entries {
		if !entry.IsDir() || !safeName(entry.Name()) {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if info, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil || info.IsDir() {
			continue
		}
		files, err := readTree(dir)
		if err != nil {
			return nil, fmt.Errorf("reading skill %s: %w", entry.Name(), err)
		}
		sources = append(sources, skillSource{name: entry.Name(), files: files})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].name < sources[j].name })
	return sources, nil
}

func readTree(root string) ([]sourceFile, error) {
	var files []sourceFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not portable: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("unsafe skill path %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, sourceFile{rel: rel, data: data, mode: info.Mode().Perm()})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files, err
}

func planRoot(targetRoot string, sources []skillSource) ([]mirrorPlan, error) {
	desired := make(map[string]skillSource, len(sources))
	var plans []mirrorPlan
	for _, source := range sources {
		desired[source.name] = source
		targetDir := filepath.Join(targetRoot, source.name)
		if info, err := os.Stat(targetDir); err == nil {
			if !info.IsDir() || !isManaged(targetDir) {
				return nil, fmt.Errorf("target skill is user-owned: %s", targetDir)
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		desiredFiles := map[string]bool{ownershipFile: true}
		for _, file := range source.files {
			desiredFiles[filepath.Clean(file.rel)] = true
		}
		remove, err := findStale(targetDir, desiredFiles)
		if err != nil {
			return nil, err
		}
		plans = append(plans, mirrorPlan{targetDir: targetDir, source: source, remove: remove})
	}
	entries, err := os.ReadDir(targetRoot)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || desired[entry.Name()].name != "" {
			continue
		}
		dir := filepath.Join(targetRoot, entry.Name())
		if isManaged(dir) {
			plans = append(plans, mirrorPlan{targetDir: dir, stale: true})
		}
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].targetDir < plans[j].targetDir })
	return plans, nil
}

func writeMirror(item mirrorPlan) error {
	desired := map[string]bool{ownershipFile: true}
	for _, file := range item.source.files {
		desired[filepath.Clean(file.rel)] = true
		path := filepath.Join(item.targetDir, file.rel)
		if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, file.data) {
			continue
		}
		if err := managedfile.WriteAtomic(path, file.data, file.mode); err != nil {
			return fmt.Errorf("writing managed skill file %s: %w", path, err)
		}
	}
	if err := removeStale(item.targetDir, desired); err != nil {
		return err
	}
	marker := []byte("Generated by Harnest from .agents/skills. Edit the portable source instead.\n")
	if err := managedfile.WriteAtomic(filepath.Join(item.targetDir, ownershipFile), marker, 0600); err != nil {
		return err
	}
	return nil
}

func removeStale(root string, desired map[string]bool) error {
	if !isManaged(root) {
		return nil
	}
	stale, err := findStale(root, desired)
	if err != nil {
		return err
	}
	for _, path := range stale {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func findStale(root string, desired map[string]bool) ([]string, error) {
	if !isManaged(root) {
		return nil, nil
	}
	var stale []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !desired[filepath.Clean(rel)] {
			stale = append(stale, path)
		}
		return nil
	})
	sort.Strings(stale)
	return stale, err
}

func isManaged(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ownershipFile))
	return err == nil && !info.IsDir()
}

func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, "\\/\r\n")
}

func plannedPaths(plans []mirrorPlan) []string {
	var paths []string
	for _, item := range plans {
		if item.stale {
			paths = append(paths, item.targetDir+" (remove)")
			continue
		}
		for _, file := range item.source.files {
			paths = append(paths, filepath.Join(item.targetDir, file.rel))
		}
		paths = append(paths, filepath.Join(item.targetDir, ownershipFile))
		for _, path := range item.remove {
			paths = append(paths, path+" (remove)")
		}
	}
	sort.Strings(paths)
	return paths
}
