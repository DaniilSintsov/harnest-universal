package harness

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/ir"
	"github.com/daniilsintsov/harnest-universal/internal/managedfile"
)

type ClaudeCodeGenerator struct{}

func (g *ClaudeCodeGenerator) Capabilities() ir.Capabilities {
	return ir.Capabilities{
		Instructions: ir.Native,
		ScopedRules:  ir.Fallback,
		Skills:       ir.Native,
		Agents:       ir.Native,
		PreToolHook:  ir.Unsupported,
		PostToolHook: ir.Unsupported,
		Permissions:  ir.Fallback,
		Verification: ir.Fallback,
	}
}

func (g *ClaudeCodeGenerator) Generate(projectDir string, project ir.Project) (string, error) {
	var b strings.Builder
	stacks, agents := project.Stacks, project.Agents

	projectName := filepath.Base(projectDir)

	b.WriteString(fmt.Sprintf("# %s\n\n", projectName))

	// Stack section
	b.WriteString("## Stack\n")
	for _, s := range stacks {
		b.WriteString(fmt.Sprintf("- %s (%s)\n", s.Name, s.Path))
	}
	b.WriteString("\n")

	if hasAssignedConsilium(agents) || hasAssignedExec(agents) {
		b.WriteString("## Agents\n\n")
		if hasAssignedConsilium(agents) {
			b.WriteString("### Consilium\n")
			b.WriteString("| Role | Agent |\n")
			b.WriteString("|------|-------|\n")
			for _, c := range agents.Consilium {
				if c.Agent != "" {
					b.WriteString(fmt.Sprintf("| %s | %s |\n", c.Role, c.Agent))
				}
			}
			b.WriteString("\n")
		}
		if hasAssignedExec(agents) {
			b.WriteString("### Executing\n")
			b.WriteString("| Agent | Scope |\n")
			b.WriteString("|-------|-------|\n")
			for _, e := range agents.Exec {
				if e.Agent != "" {
					b.WriteString(fmt.Sprintf("| %s | %s |\n", e.Agent, e.Scope))
				}
			}
			b.WriteString("\n")
		}
	}

	// Models
	if len(agents.Models) > 0 && hasAssignedConsilium(agents) {
		b.WriteString("### Model tiers\n")
		b.WriteString("Concrete models come from the user adapter mapping; otherwise the platform default is used.\n\n")
		b.WriteString("| Role | Tier |\n")
		b.WriteString("|------|-------|\n")
		for _, c := range agents.Consilium {
			if c.Agent == "" {
				continue
			}
			tier := agents.Models[c.Role]
			if tier == "" {
				tier = "medium"
			}
			b.WriteString(fmt.Sprintf("| %s | %s |\n", c.Role, tier))
		}
		b.WriteString("\n")
	}

	b.WriteString(renderControlPlane(project))

	outPath := filepath.Join(projectDir, "CLAUDE.md")
	if err := managedfile.UpsertWithMode(outPath, "harnest", b.String(), 0644); err != nil {
		return "", fmt.Errorf("writing %s: %w", outPath, err)
	}

	return outPath, nil
}
