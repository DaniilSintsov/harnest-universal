package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/ir"
)

type Generator interface {
	Generate(projectDir string, project ir.Project) (string, error)
}

type capabilityProvider interface {
	Capabilities() ir.Capabilities
}

// HarnessInfo holds metadata about a harness for agent discovery.
type HarnessInfo struct {
	Generator Generator
	// AgentDir is the project-relative path where this harness stores custom agents.
	// Empty means no custom agent dir.
	AgentDir string
	// ProjectSkillsDir is the native project-relative skill directory.
	ProjectSkillsDir string
	// ConfigEnv overrides the harness global config directory.
	ConfigEnv string
	// DefaultConfigDir is the config directory relative to the user home.
	DefaultConfigDir string
	// GlobalConfigFile is the filename for this harness's global config.
	GlobalConfigFile string
}

var registry = map[string]HarnessInfo{
	"claude-code": {Generator: &ClaudeCodeGenerator{}, AgentDir: ".claude/agents", ProjectSkillsDir: ".claude/skills", ConfigEnv: "CLAUDE_CONFIG_DIR", DefaultConfigDir: ".claude", GlobalConfigFile: "CLAUDE.md"},
	"codex":       {Generator: &CodexGenerator{}, AgentDir: ".codex/agents", ProjectSkillsDir: ".agents/skills", ConfigEnv: "CODEX_HOME", DefaultConfigDir: ".codex", GlobalConfigFile: "AGENTS.md"},
}

func Get(name string) (Generator, error) {
	h, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown harness: %s (available: %s)", name, strings.Join(Names(), ", "))
	}
	return h.Generator, nil
}

// Capabilities reports what an adapter can preserve natively.
func Capabilities(name string) (ir.Capabilities, error) {
	gen, err := Get(name)
	if err != nil {
		return ir.Capabilities{}, err
	}
	if provider, ok := gen.(capabilityProvider); ok {
		return provider.Capabilities(), nil
	}
	return ir.Capabilities{Instructions: ir.Native}, nil
}

// Names returns sorted list of all registered harness names.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GlobalDir returns the absolute path to a harness's config directory.
func GlobalDir(name string) (string, error) {
	h, ok := registry[name]
	if !ok {
		return "", fmt.Errorf("unknown harness: %s (available: %s)", name, strings.Join(Names(), ", "))
	}
	if configured := strings.TrimSpace(os.Getenv(h.ConfigEnv)); configured != "" {
		path, err := filepath.Abs(configured)
		if err != nil {
			return "", fmt.Errorf("resolving %s: %w", h.ConfigEnv, err)
		}
		return filepath.Clean(path), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, h.DefaultConfigDir), nil
}

// GlobalConfigPath returns the absolute path to a harness's global config file.
func GlobalConfigPath(name string) (string, error) {
	h, ok := registry[name]
	if !ok {
		return "", fmt.Errorf("unknown harness: %s (available: %s)", name, strings.Join(Names(), ", "))
	}
	dir, err := GlobalDir(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, h.GlobalConfigFile), nil
}

// GlobalSkillsDir returns the native global skill directory for a harness.
func GlobalSkillsDir(name string) (string, error) {
	if _, ok := registry[name]; !ok {
		return "", fmt.Errorf("unknown harness: %s (available: %s)", name, strings.Join(Names(), ", "))
	}
	if name == "claude-code" {
		dir, err := GlobalDir(name)
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "skills"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".agents", "skills"), nil
}

// GlobalAgentDir returns the native global agent directory for one harness.
func GlobalAgentDir(name string) (string, error) {
	dir, err := GlobalDir(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "agents"), nil
}

// GlobalAgentDirs returns all configured native global agent directories.
func GlobalAgentDirs() []string {
	var dirs []string
	for _, name := range Names() {
		if dir, err := GlobalAgentDir(name); err == nil {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// AgentDirs returns all agent directory paths (relative to $HOME) from registered harnesses.
func AgentDirs() []string {
	var dirs []string
	for _, h := range registry {
		if h.AgentDir != "" {
			dirs = append(dirs, h.AgentDir)
		}
	}
	sort.Strings(dirs)
	return dirs
}

// AgentDir returns the configured global agent directory relative to HOME.
func AgentDir(name string) (string, error) {
	h, ok := registry[name]
	if !ok {
		return "", fmt.Errorf("unknown harness: %s", name)
	}
	return h.AgentDir, nil
}

// ProjectSkillsDir returns the native project-relative skill directory.
func ProjectSkillsDir(name string) (string, error) {
	h, ok := registry[name]
	if !ok {
		return "", fmt.Errorf("unknown harness: %s", name)
	}
	return h.ProjectSkillsDir, nil
}

// Installed returns harnesses whose global directory already exists.
func Installed(names ...string) []string {
	var installed []string
	for _, name := range names {
		dir, err := GlobalDir(name)
		if err != nil {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			installed = append(installed, name)
		}
	}
	return installed
}
