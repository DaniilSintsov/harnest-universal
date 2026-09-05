package yaml

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/agents"
	"github.com/daniilsintsov/harnest-universal/internal/detector"
	"github.com/daniilsintsov/harnest-universal/internal/harness"
	"github.com/daniilsintsov/harnest-universal/internal/ir"
	"github.com/daniilsintsov/harnest-universal/internal/managedfile"
	"github.com/daniilsintsov/harnest-universal/internal/mapping"
	projectSkills "github.com/daniilsintsov/harnest-universal/internal/skills"
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
	if err := validateTargets(project.Targets); err != nil {
		return nil, err
	}
	if err := validateAdapterOutputs(dir, project.Targets); err != nil {
		return nil, err
	}
	if _, err := projectSkills.Materialize(dir, project.Skills.Root, project.Targets, true); err != nil {
		return nil, err
	}
	if _, err := agents.PortablePaths(dir, project.Targets); err != nil {
		return nil, err
	}

	var generated []string
	skillFiles, err := projectSkills.Materialize(dir, project.Skills.Root, project.Targets, false)
	if err != nil {
		return nil, err
	}
	generated = append(generated, skillFiles...)
	portableFiles, err := agents.MaterializePortable(dir, project.Targets)
	if err != nil {
		return nil, err
	}
	generated = append(generated, portableFiles...)

	for _, harnessName := range project.Targets {
		gen, _ := harness.Get(harnessName)

		targetProject := project
		targetProject.Agents = ResolveTargetAgents(project, harnessName)
		outPath, err := gen.Generate(dir, targetProject)
		if err != nil {
			return generated, fmt.Errorf("generating %q: %w", harnessName, err)
		}

		generated = append(generated, outPath)
	}

	return generated, nil
}

// DryRunResult contains adapter content and every project path generation would
// create, update, or remove.
type DryRunResult struct {
	Adapters map[string]string
	Files    []string
}

// GenerateDryRun previews adapter, portable-agent, and portable-skill outputs
// without writing project files.
//
// If .harnest-local.yaml exists in dir, its overrides are merged into cfg
// before generation. The caller's cfg value is never mutated.
//
// Note: because harness generators currently write files as a side effect of
// Generate, this function simulates the output by temporarily redirecting
// writes to a temp directory and reading back the result.
func GenerateDryRun(dir string, cfg *HarnestConfig) (DryRunResult, error) {
	if cfg == nil {
		return DryRunResult{}, fmt.Errorf("config must not be nil")
	}

	project, err := BuildIR(dir, cfg)
	if err != nil {
		return DryRunResult{}, err
	}
	if err := validateTargets(project.Targets); err != nil {
		return DryRunResult{}, err
	}
	if err := validateAdapterOutputs(dir, project.Targets); err != nil {
		return DryRunResult{}, err
	}
	skillFiles, err := projectSkills.Materialize(dir, project.Skills.Root, project.Targets, true)
	if err != nil {
		return DryRunResult{}, err
	}
	agentFiles, err := agents.PortablePaths(dir, project.Targets)
	if err != nil {
		return DryRunResult{}, err
	}

	tmpDir, err := os.MkdirTemp("", "harnest-dryrun-*")
	if err != nil {
		return DryRunResult{}, fmt.Errorf("creating temp dir for dry run: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	results := DryRunResult{Adapters: make(map[string]string, len(project.Targets))}
	results.Files = append(results.Files, skillFiles...)
	results.Files = append(results.Files, agentFiles...)

	for _, harnessName := range project.Targets {
		gen, _ := harness.Get(harnessName)

		targetProject := project
		targetProject.Agents = ResolveTargetAgents(project, harnessName)
		outPath, err := gen.Generate(tmpDir, targetProject)
		if err != nil {
			return results, fmt.Errorf("dry-run generating %q: %w", harnessName, err)
		}

		data, err := os.ReadFile(outPath)
		if err != nil {
			return results, fmt.Errorf("reading dry-run output for %q: %w", harnessName, err)
		}

		results.Adapters[harnessName] = string(data)
		results.Files = append(results.Files, adapterOutputPath(dir, harnessName))
	}
	sort.Strings(results.Files)
	return results, nil
}

// ResolveTargetAgents applies one adapter's overrides to the shared agent config.
func ResolveTargetAgents(project ir.Project, target string) mapping.AgentConfig {
	adapter, ok := project.Adapters[target]
	if !ok {
		return project.Agents
	}
	return mergeAgentConfigs(project.Agents, adapter.Agents)
}

func mergeAgentConfigs(base, overlay mapping.AgentConfig) mapping.AgentConfig {
	if len(overlay.Consilium) == 0 && len(overlay.Exec) == 0 && len(overlay.Models) == 0 {
		return base
	}

	result := mapping.AgentConfig{
		Models: mergeStringMap(base.Models, overlay.Models),
	}

	roleAgents := map[string]string{}
	var roleOrder []string
	addRole := func(role, agent string) {
		if _, ok := roleAgents[role]; !ok {
			roleOrder = append(roleOrder, role)
		}
		roleAgents[role] = agent
	}
	for _, role := range base.Consilium {
		addRole(role.Role, role.Agent)
	}
	for _, role := range overlay.Consilium {
		addRole(role.Role, role.Agent)
	}
	for _, role := range roleOrder {
		if agent := roleAgents[role]; agent != "" {
			result.Consilium = append(result.Consilium, mapping.ConsiliumRole{Role: role, Agent: agent})
		}
	}

	overriddenScopes := make(map[string]bool, len(overlay.Exec))
	for _, execAgent := range overlay.Exec {
		overriddenScopes[execAgent.Scope] = true
	}
	for _, execAgent := range base.Exec {
		if !overriddenScopes[execAgent.Scope] {
			result.Exec = append(result.Exec, execAgent)
		}
	}
	overlayIndex := map[string]int{}
	for _, execAgent := range overlay.Exec {
		if index, ok := overlayIndex[execAgent.Scope]; ok {
			result.Exec[index] = execAgent
		} else {
			overlayIndex[execAgent.Scope] = len(result.Exec)
			result.Exec = append(result.Exec, execAgent)
		}
	}

	return result
}

func mergeStringMap(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	result := make(map[string]string, len(base)+len(overlay))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range overlay {
		result[key] = value
	}
	return result
}

func validateTargets(targets []string) error {
	if len(targets) == 0 {
		return fmt.Errorf("harnesses must contain at least one target")
	}
	seen := map[string]bool{}
	for _, target := range targets {
		if target == "" {
			return fmt.Errorf("harness target must not be empty")
		}
		if seen[target] {
			return fmt.Errorf("duplicate harness target %q", target)
		}
		seen[target] = true
		if _, err := harness.Get(target); err != nil {
			return fmt.Errorf("harness %q: %w", target, err)
		}
	}
	return nil
}

func adapterOutputPath(dir, target string) string {
	if target == "claude-code" {
		return filepath.Join(dir, "CLAUDE.md")
	}
	return filepath.Join(dir, "AGENTS.md")
}

func validateAdapterOutputs(dir string, targets []string) error {
	for _, target := range targets {
		if err := managedfile.ValidateUpsert(adapterOutputPath(dir, target), "harnest"); err != nil {
			return err
		}
	}
	return nil
}

// UpdateGitignore adds the generated file paths to the project's .gitignore
// under a "# Harnest generated" block. Entries already present in the file
// are skipped. If .gitignore does not exist it is created.
//
// .harnest-local.yaml is always included so personal overrides are never
// accidentally committed, regardless of whether any other files were generated.
func UpdateGitignore(dir string, files []string) (bool, error) {
	// Always gitignore the local overrides file.
	files = unionStrings(files, []string{localConfigFileName})

	gitignorePath := filepath.Join(dir, ".gitignore")

	existing, err := readGitignoreEntries(gitignorePath)
	if err != nil {
		return false, fmt.Errorf("reading .gitignore: %w", err)
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
		return false, nil
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false, fmt.Errorf("opening .gitignore: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(gitignoreMarker + "\n")
	for _, entry := range missing {
		sb.WriteString(entry + "\n")
	}

	if _, err := f.WriteString(sb.String()); err != nil {
		return false, fmt.Errorf("writing .gitignore entries: %w", err)
	}

	return true, nil
}

// UpdateLocalExclude keeps Harnest-owned project files local without changing
// the repository's tracked .gitignore.
func UpdateLocalExclude(dir string, files []string) (bool, error) {
	gitDir := filepath.Join(dir, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(gitDir)
		if err != nil {
			return false, err
		}
		value := strings.TrimSpace(string(data))
		if !strings.HasPrefix(value, "gitdir:") {
			return false, fmt.Errorf("invalid .git file")
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

func updateIgnoreFile(path, dir string, files []string, marker string) (bool, error) {
	existing, err := readGitignoreEntries(path)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
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
		return false, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false, err
	}
	defer f.Close()

	var b strings.Builder
	b.WriteString("\n" + marker + "\n")
	for _, entry := range missing {
		b.WriteString(entry + "\n")
	}
	_, err = f.WriteString(b.String())
	return err == nil, err
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
