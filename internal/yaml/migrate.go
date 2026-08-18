package yaml

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/daniilsintsov/harnest-universal/internal/ir"
	"github.com/daniilsintsov/harnest-universal/internal/managedfile"
	"github.com/daniilsintsov/harnest-universal/internal/rules"
)

const CurrentVersion = 2

// MigrateFile upgrades harnest.yaml and keeps the original beside it.
func MigrateFile(dir string) (bool, string, error) {
	cfg, err := Load(dir)
	if err != nil {
		return false, "", err
	}
	if cfg.Version == CurrentVersion {
		if err := validateImplementedFields(cfg); err != nil {
			return false, "", err
		}
		return false, "", nil
	}

	path := filepath.Join(dir, configFileName)
	original, err := os.ReadFile(path)
	if err != nil {
		return false, "", err
	}
	backup := path + ".v1.bak"
	if err := managedfile.WriteAtomic(backup, original, 0600); err != nil {
		return false, "", fmt.Errorf("backing up config: %w", err)
	}

	upgraded, err := Migrate(cfg)
	if err != nil {
		return false, backup, err
	}
	if err := validateImplementedFields(upgraded); err != nil {
		return false, backup, err
	}
	if err := Save(dir, upgraded); err != nil {
		return false, backup, err
	}
	return true, backup, nil
}

// Migrate upgrades a supported config in memory without mutating the input.
func Migrate(cfg *HarnestConfig) (*HarnestConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	if cfg.Version != 1 && cfg.Version != CurrentVersion {
		return nil, fmt.Errorf("unsupported harnest.yaml version %d", cfg.Version)
	}

	upgraded := *cfg
	if cfg.Version == CurrentVersion {
		return &upgraded, nil
	}

	upgraded.Version = CurrentVersion
	upgraded.Context.Architecture = ArchitectureBlock{
		Index: "docs/architecture/INDEX.md",
		State: "docs/architecture/.context-state.json",
	}
	upgraded.Rules = ResourceBlock{Root: ".harnest/rules", Index: ".harnest/rules/INDEX.yaml"}
	upgraded.Skills = ResourceBlock{Root: ".agents/skills"}
	upgraded.Checks = ResourceBlock{Root: ".harnest/checks"}
	upgraded.Workflow = WorkflowBlock{
		DefaultProfile:        "business-feature",
		RoleSelection:         ir.RoleSelectionAuto,
		RequireAvailableRoles: true,
		VerifyChanged:         true,
	}
	upgraded.Settings.LocalDefault = true
	upgraded.Settings.Language = "ru"
	return &upgraded, nil
}

// BuildIR resolves config and local overrides into adapter-neutral input.
func BuildIR(dir string, cfg *HarnestConfig) (ir.Project, error) {
	if LocalExists(dir) {
		local, err := LoadLocal(dir)
		if err != nil {
			return ir.Project{}, err
		}
		cfg = Merge(cfg, local)
	}

	upgraded, err := Migrate(cfg)
	if err != nil {
		return ir.Project{}, err
	}
	if err := validateImplementedFields(upgraded); err != nil {
		return ir.Project{}, err
	}
	roleSelection := upgraded.Workflow.RoleSelection
	if roleSelection == "" || roleSelection == ir.RoleSelectionInteractive {
		roleSelection = ir.RoleSelectionAuto
	}
	if roleSelection != ir.RoleSelectionInteractive && roleSelection != ir.RoleSelectionAuto {
		return ir.Project{}, fmt.Errorf("invalid workflow.role_selection %q: use %q or %q", roleSelection, ir.RoleSelectionInteractive, ir.RoleSelectionAuto)
	}

	project := ir.Project{
		Version:     upgraded.Version,
		Name:        upgraded.Project.Name,
		Description: upgraded.Project.Description,
		Stacks:      resolveStacks(dir, upgraded),
		Agents:      upgraded.ToAgentConfig(),
		Architecture: ir.Architecture{
			Index: upgraded.Context.Architecture.Index,
			State: upgraded.Context.Architecture.State,
		},
		Rules:    ir.ResourceIndex{Root: upgraded.Rules.Root, Index: upgraded.Rules.Index},
		Skills:   ir.ResourceIndex{Root: upgraded.Skills.Root, Index: upgraded.Skills.Index},
		Checks:   ir.ResourceIndex{Root: upgraded.Checks.Root, Index: upgraded.Checks.Index},
		Targets:  append([]string(nil), upgraded.Harnesses...),
		Language: upgraded.Settings.Language,
		Workflow: ir.Workflow{
			Adaptive:              upgraded.Workflow.Adaptive,
			DefaultProfile:        upgraded.Workflow.DefaultProfile,
			RoleSelection:         roleSelection,
			RequireAvailableRoles: upgraded.Workflow.RequireAvailableRoles,
			VerifyChanged:         upgraded.Workflow.VerifyChanged,
		},
		Adapters: make(map[string]ir.AdapterSettings, len(upgraded.Adapters)),
	}
	for name, settings := range upgraded.Adapters {
		project.Adapters[name] = ir.AdapterSettings{Models: settings.Models}
	}
	project.PolicyRules, err = rules.Load(dir, upgraded.Rules.Root)
	if err != nil {
		return ir.Project{}, fmt.Errorf("loading rules: %w", err)
	}
	return project, nil
}

func validateImplementedFields(cfg *HarnestConfig) error {
	if cfg.DesignSystem != "" {
		return fmt.Errorf("design_system is not implemented; remove it from harnest.yaml and .harnest-local.yaml")
	}
	if len(cfg.Profiles.Enabled) > 0 || len(cfg.Profiles.Custom) > 0 {
		return fmt.Errorf("profiles config is not implemented; manage profiles with 'harnest profiles'")
	}
	if cfg.Settings.LockFile {
		return fmt.Errorf("settings.lock_file is not implemented; remove it from harnest.yaml")
	}
	for name, adapter := range cfg.Adapters {
		if len(adapter.Models) > 0 {
			return fmt.Errorf("adapters.%s.models is not implemented; use agents.models capability tiers", name)
		}
	}
	return nil
}
