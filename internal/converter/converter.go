package converter

import (
	"fmt"

	"github.com/daniilsintsov/harnest-universal/internal/config"
	"github.com/daniilsintsov/harnest-universal/internal/detector"
	"github.com/daniilsintsov/harnest-universal/internal/harness"
	"github.com/daniilsintsov/harnest-universal/internal/ir"
	"github.com/daniilsintsov/harnest-universal/internal/mapping"
)

// Convert reads a supported legacy agent mapping and generates a target config.
func Convert(dir, from, to string) (string, error) {
	if from != "claude-code" {
		return "", fmt.Errorf("unsupported source harness %q; supported source: claude-code", from)
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
