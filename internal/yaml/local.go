package yaml

import (
	"fmt"
	"os"
	"path/filepath"

	goyaml "gopkg.in/yaml.v3"
)

const localConfigFileName = ".harnest-local.yaml"

const localFileHeader = `# .harnest-local.yaml — personal overrides for harnest.yaml.
# This file is intentionally gitignored. It applies on top of the team config.
# Documentation: see the README bundled with this Harnest build.
`

// LocalConfig represents personal overrides stored in .harnest-local.yaml.
// All fields are optional — zero values mean "no override".
type LocalConfig struct {
	Agents       AgentsBlock `yaml:"agents,omitempty"`
	Harnesses    []string    `yaml:"harnesses,omitempty"`
	DesignSystem string      `yaml:"design_system,omitempty"`
}

// LoadLocal reads .harnest-local.yaml from dir. Returns nil, nil if not found.
func LoadLocal(dir string) (*LocalConfig, error) {
	path := filepath.Join(dir, localConfigFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var cfg LocalConfig
	if err := goyaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	return &cfg, nil
}

// SaveLocal writes cfg to .harnest-local.yaml in dir.
// The file is prefixed with a descriptive header comment.
func SaveLocal(dir string, cfg *LocalConfig) error {
	if cfg == nil {
		return fmt.Errorf("cannot save nil local config")
	}

	data, err := goyaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling local config: %w", err)
	}

	path := filepath.Join(dir, localConfigFileName)
	content := localFileHeader + "\n" + string(data)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

// LocalExists reports whether .harnest-local.yaml is present in dir.
func LocalExists(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, localConfigFileName))
	return err == nil && !info.IsDir()
}

// Merge applies local overrides on top of a team config and returns a new
// merged HarnestConfig. The original team config is never mutated.
//
// Merge rules:
//   - Scalar fields (DesignSystem): local wins when non-empty.
//   - Maps (Consilium, Models): local keys override team keys; team keys not
//     present in local are preserved.
//   - Slices (Harnesses, Executing): union — local entries are appended if not
//     already present. Existing team entries are never removed.
func Merge(team *HarnestConfig, local *LocalConfig) *HarnestConfig {
	if team == nil {
		return nil
	}
	if local == nil {
		return team
	}

	// Shallow-copy the team config so we do not mutate the original.
	merged := *team

	// --- DesignSystem ---
	if local.DesignSystem != "" {
		merged.DesignSystem = local.DesignSystem
	}

	// --- Harnesses (union) ---
	merged.Harnesses = unionStrings(team.Harnesses, local.Harnesses)

	// --- Agents ---
	merged.Agents = mergeAgents(team.Agents, local.Agents)

	return &merged
}

// mergeAgents merges a local AgentsBlock on top of a team AgentsBlock.
func mergeAgents(team, local AgentsBlock) AgentsBlock {
	result := AgentsBlock{}

	// Consilium: start from team, apply local overrides.
	result.Consilium = mergeMaps(team.Consilium, local.Consilium)

	// Models: start from team, apply local overrides.
	result.Models = mergeMaps(team.Models, local.Models)

	// Executing: union by (Agent, Scope) pair.
	result.Executing = unionExec(team.Executing, local.Executing)

	return result
}

// mergeMaps returns a new map containing all entries from base with overrides
// from overlay applied. Neither input map is mutated.
func mergeMaps(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}

	result := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overlay {
		result[k] = v
	}
	return result
}

// unionStrings returns a slice containing all elements of base followed by any
// elements of additions that are not already present in base.
func unionStrings(base, additions []string) []string {
	if len(additions) == 0 {
		return base
	}

	seen := make(map[string]bool, len(base))
	for _, s := range base {
		seen[s] = true
	}

	result := make([]string, len(base), len(base)+len(additions))
	copy(result, base)

	for _, s := range additions {
		if !seen[s] {
			result = append(result, s)
			seen[s] = true
		}
	}
	return result
}

// unionExec returns a slice containing all ExecEntry values from base followed
// by any entries from additions whose (Agent, Scope) pair is not already present.
func unionExec(base, additions []ExecEntry) []ExecEntry {
	if len(additions) == 0 {
		return base
	}

	type key struct{ agent, scope string }
	seen := make(map[key]bool, len(base))
	for _, e := range base {
		seen[key{e.Agent, e.Scope}] = true
	}

	result := make([]ExecEntry, len(base), len(base)+len(additions))
	copy(result, base)

	for _, e := range additions {
		k := key{e.Agent, e.Scope}
		if !seen[k] {
			result = append(result, e)
			seen[k] = true
		}
	}
	return result
}
