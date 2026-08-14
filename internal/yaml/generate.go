package yaml

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/agents"
	"github.com/daniilsintsov/harnest-universal/internal/detector"
	"github.com/daniilsintsov/harnest-universal/internal/harness"
)

const gitignoreMarker = "# Harnest generated"

const localExcludeMarker = "# Harnest local"

// Generate produces config files for all harnesses listed in cfg.Harnesses.
// For each harness it:
//  1. Resolves the generator from the harness registry.
//  2. Converts cfg to a mapping.AgentConfig via ToAgentConfig.
//  3. Obtains the stack list — either from cfg.Stacks or via auto-detection.
//  4. Calls the generator, which writes the output file and returns its path.
//
// If .harnest-local.yaml exists in dir, its overrides are merged into cfg
// before generation. The caller's cfg value is never mutated.
//
// Returns the list of file paths that were written.
func Generate(dir string, cfg *HarnestConfig) ([]string, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}

	project, err := BuildIR(dir, cfg)
	if err != nil {
		return nil, err
	}

	var generated []string
	portableFiles, err := agents.MaterializePortable(dir, project.Targets)
	if err != nil {
		return nil, err
	}
	generated = append(generated, portableFiles...)

	for _, harnessName := range project.Targets {
		gen, err := harness.Get(harnessName)
		if err != nil {
			return generated, fmt.Errorf("harness %q: %w", harnessName, err)
		}

		outPath, err := gen.Generate(dir, project)
		if err != nil {
			return generated, fmt.Errorf("generating %q: %w", harnessName, err)
		}

		generated = append(generated, outPath)
	}

	return generated, nil
}

// GenerateDryRun previews adapter outputs without writing project files. The
// returned map keys are harness names and values are adapter content strings.
// Portable-agent materialization is intentionally not previewed.
//
// If .harnest-local.yaml exists in dir, its overrides are merged into cfg
// before generation. The caller's cfg value is never mutated.
//
// Note: because harness generators currently write files as a side effect of
// Generate, this function simulates the output by temporarily redirecting
// writes to a temp directory and reading back the result.
func GenerateDryRun(dir string, cfg *HarnestConfig) (map[string]string, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}

	project, err := BuildIR(dir, cfg)
	if err != nil {
		return nil, err
	}

	tmpDir, err := os.MkdirTemp("", "harnest-dryrun-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir for dry run: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	results := make(map[string]string, len(project.Targets))

	for _, harnessName := range project.Targets {
		gen, err := harness.Get(harnessName)
		if err != nil {
			return results, fmt.Errorf("harness %q: %w", harnessName, err)
		}

		outPath, err := gen.Generate(tmpDir, project)
		if err != nil {
			return results, fmt.Errorf("dry-run generating %q: %w", harnessName, err)
		}

		data, err := os.ReadFile(outPath)
		if err != nil {
			return results, fmt.Errorf("reading dry-run output for %q: %w", harnessName, err)
		}

		results[harnessName] = string(data)
	}

	return results, nil
}

// UpdateGitignore adds the generated file paths to the project's .gitignore
// under a "# Harnest generated" block. Entries already present in the file
// are skipped. If .gitignore does not exist it is created.
//
// .harnest-local.yaml is always included so personal overrides are never
// accidentally committed, regardless of whether any other files were generated.
func UpdateGitignore(dir string, files []string) error {
	// Always gitignore the local overrides file.
	files = unionStrings(files, []string{localConfigFileName})

	gitignorePath := filepath.Join(dir, ".gitignore")

	existing, err := readGitignoreEntries(gitignorePath)
	if err != nil {
		return fmt.Errorf("reading .gitignore: %w", err)
	}

	// Compute relative paths and filter out already-present entries.
	var missing []string
	for _, f := range files {
		rel, err := filepath.Rel(dir, f)
		if err != nil {
			rel = f
		}
		if !existing[rel] {
			missing = append(missing, rel)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening .gitignore: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(gitignoreMarker + "\n")
	for _, entry := range missing {
		sb.WriteString(entry + "\n")
	}

	if _, err := f.WriteString(sb.String()); err != nil {
		return fmt.Errorf("writing .gitignore entries: %w", err)
	}

	return nil
}

// UpdateLocalExclude keeps Harnest-owned project files local without changing
// the repository's tracked .gitignore.
func UpdateLocalExclude(dir string, files []string) error {
	gitDir := filepath.Join(dir, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(gitDir)
		if err != nil {
			return err
		}
		value := strings.TrimSpace(string(data))
		if !strings.HasPrefix(value, "gitdir:") {
			return fmt.Errorf("invalid .git file")
		}
		gitDir = strings.TrimSpace(strings.TrimPrefix(value, "gitdir:"))
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(dir, gitDir)
		}
		if common, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
			commonDir := strings.TrimSpace(string(common))
			if !filepath.IsAbs(commonDir) {
				commonDir = filepath.Join(gitDir, commonDir)
			}
			gitDir = filepath.Clean(commonDir)
		}
	}

	withBackups := append([]string(nil), files...)
	for _, file := range files {
		withBackups = append(withBackups, file+".bak")
	}
	files = unionStrings(withBackups, []string{
		filepath.Join(dir, configFileName),
		filepath.Join(dir, localConfigFileName),
		filepath.Join(dir, ".harnest") + string(filepath.Separator),
		filepath.Join(dir, ".agents", "skills") + string(filepath.Separator),
		filepath.Join(dir, "docs", "architecture") + string(filepath.Separator),
		filepath.Join(dir, ".reports", "architecture-context") + string(filepath.Separator),
	})
	return updateIgnoreFile(filepath.Join(gitDir, "info", "exclude"), dir, files, localExcludeMarker)
}

func updateIgnoreFile(path, dir string, files []string, marker string) error {
	existing, err := readGitignoreEntries(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var missing []string
	for _, file := range files {
		isDir := strings.HasSuffix(file, string(filepath.Separator))
		rel, err := filepath.Rel(dir, file)
		if err != nil {
			rel = file
		}
		rel = filepath.ToSlash(rel)
		if isDir {
			rel += "/"
		}
		if !existing[rel] {
			missing = append(missing, rel)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	var b strings.Builder
	b.WriteString("\n" + marker + "\n")
	for _, entry := range missing {
		b.WriteString(entry + "\n")
	}
	_, err = f.WriteString(b.String())
	return err
}

// resolveStacks returns the stack list to use for generation. The strategy is:
//
//   - If settings.AutoDetect is true, always run the detector and (when
//     strategy is "merge") append any manually declared stacks on top.
//   - If cfg.Stacks is non-empty, convert them directly without detection.
//   - If cfg.Stacks is empty, fall back to auto-detection regardless of the
//     AutoDetect flag, so generators always have something to work with.
func resolveStacks(dir string, cfg *HarnestConfig) []detector.Stack {
	if cfg.Settings.AutoDetect {
		detected := detector.Detect(dir)
		if strings.EqualFold(cfg.Settings.StackStrategy, "merge") {
			detected = append(detected, configStacksToDetector(cfg.Stacks)...)
		}
		return detected
	}

	if len(cfg.Stacks) > 0 {
		return configStacksToDetector(cfg.Stacks)
	}

	// No explicit stacks and AutoDetect is off — fall back to detection so
	// generators are never handed an empty slice unexpectedly.
	return detector.Detect(dir)
}

// configStacksToDetector converts the YAML schema StackEntry slice to the
// detector.Stack type used by harness generators.
func configStacksToDetector(entries []StackEntry) []detector.Stack {
	stacks := make([]detector.Stack, 0, len(entries))
	for _, e := range entries {
		stacks = append(stacks, detector.Stack{
			Name:     e.Name,
			Lang:     e.Lang,
			Category: e.Category,
			Path:     e.Path,
		})
	}
	return stacks
}

// readGitignoreEntries parses the existing .gitignore and returns the set of
// non-empty, non-comment lines. Returns an empty map if the file does not exist.
func readGitignoreEntries(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]bool), nil
		}
		return nil, err
	}
	defer f.Close()

	entries := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries[line] = true
	}

	return entries, scanner.Err()
}
