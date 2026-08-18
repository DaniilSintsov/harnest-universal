package converter

import (
	"fmt"

	"github.com/daniilsintsov/harnest-universal/internal/config"
	"github.com/daniilsintsov/harnest-universal/internal/detector"
	"github.com/daniilsintsov/harnest-universal/internal/harness"
	"github.com/daniilsintsov/harnest-universal/internal/ir"
	"github.com/daniilsintsov/harnest-universal/internal/mapping"
	harnestYaml "github.com/daniilsintsov/harnest-universal/internal/yaml"
)

// Convert reads a supported legacy agent mapping and generates a target config.
func Convert(dir, from, to string) (string, error) {
	if from != "claude-code" {
		return "", fmt.Errorf("unsupported source harness %q; supported source: claude-code", from)
	}
	if harnestYaml.Exists(dir) {
		return convertManagedProject(dir, from, to)
	}

	cfg, err := config.ReadClaudeProject(dir)
	if err != nil {
		return "", fmt.Errorf("reading %s source: %w", from, err)
	}

	agentsCfg := mapping.AgentConfig{
		Consilium: cfg.Consilium,
		Exec:      cfg.Exec,
		Models:    cfg.Models,
	}
	// Fill default tiers for roles without explicit model.
	if agentsCfg.Models == nil {
		agentsCfg.Models = mapping.DefaultModelTiers()
	} else {
		defaults := mapping.DefaultModelTiers()
		for role, tier := range defaults {
			if _, ok := agentsCfg.Models[role]; !ok {
				agentsCfg.Models[role] = tier
			}
		}
	}

	stacks := detector.Detect(dir)

	gen, err := harness.Get(to)
	if err != nil {
		return "", fmt.Errorf("target harness: %w", err)
	}

	outPath, err := gen.Generate(dir, ir.Project{
		Version:  2,
		Stacks:   stacks,
		Agents:   agentsCfg,
		Targets:  []string{to},
		Language: "ru",
	})
	if err != nil {
		return "", fmt.Errorf("generating %s config: %w", to, err)
	}

	return outPath, nil
}

func convertManagedProject(dir, from, to string) (string, error) {
	if _, err := harness.Get(to); err != nil {
		return "", fmt.Errorf("target harness: %w", err)
	}
	cfg, err := harnestYaml.Load(dir)
	if err != nil {
		return "", err
	}
	configured := false
	for _, target := range cfg.Harnesses {
		if target == from {
			configured = true
			break
		}
	}
	if !configured {
		return "", fmt.Errorf("source harness %q is not configured in harnest.yaml", from)
	}

	cfg, err = harnestYaml.Migrate(cfg)
	if err != nil {
		return "", err
	}
	cfg.Harnesses = []string{to}
	if err := harnestYaml.Save(dir, cfg); err != nil {
		return "", err
	}
	files, err := harnestYaml.Generate(dir, cfg)
	if err != nil {
		return "", fmt.Errorf("generating %s config: %w", to, err)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("generating %s config produced no files", to)
	}
	return files[len(files)-1], nil
}
