package harness

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/ir"
	"github.com/daniilsintsov/harnest-universal/internal/managedfile"
)

type CodexGenerator struct{}

func describeRole(role string) string {
	descriptions := map[string]string{
		"architect":   "Architecture, modules, dependencies, SOLID",
		"frontend":    "UI/UX review, frontend patterns",
		"ui":          "Visual design, UX, components",
		"security":    "OWASP, vulnerabilities, auth",
		"devops":      "Infrastructure, CI/CD, deployment",
		"api":         "API contracts, REST/GraphQL",
		"diagnostics": "Logs, stacktraces, debugging",
		"test":        "Test coverage, quality",
	}
	if description, ok := descriptions[role]; ok {
		return description
	}
	return role
}

func (g *CodexGenerator) Capabilities() ir.Capabilities {
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

func (g *CodexGenerator) Generate(projectDir string, project ir.Project) (string, error) {
	var b strings.Builder
	stacks, agents := project.Stacks, project.Agents

	b.WriteString("# Project Instructions\n\n")

	// Stack context
	b.WriteString("## Tech Stack\n")
	for _, s := range stacks {
		b.WriteString(fmt.Sprintf("- %s (%s) at %s\n", s.Name, s.Lang, s.Path))
	}
	b.WriteString("\n")

	if hasAssignedConsilium(agents) {
		b.WriteString("## Expert Perspectives\n")
		b.WriteString("When analyzing code, consider these perspectives:\n\n")
		for _, c := range agents.Consilium {
			if c.Agent != "" {
				b.WriteString(fmt.Sprintf("- **%s**: %s (%s)\n", c.Role, describeRole(c.Role), c.Agent))
			}
		}
		b.WriteString("\n")
	}

	if hasAssignedExec(agents) {
		b.WriteString("## File Ownership\n")
		b.WriteString("Match code style and patterns for each area:\n\n")
		for _, e := range agents.Exec {
			if e.Agent != "" {
				b.WriteString(fmt.Sprintf("- `%s` → %s patterns\n", e.Scope, e.Agent))
			}
		}
		b.WriteString("\n")
	}

	// Model recommendations
	if len(agents.Models) > 0 && hasAssignedConsilium(agents) {
		b.WriteString("## Model Recommendations\n")
		b.WriteString("For best results, use higher-capability models for these roles:\n\n")
		var high, standard []string
		for _, c := range agents.Consilium {
			if c.Agent == "" {
				continue
			}
			tier := agents.Models[c.Role]
			if tier == "high" {
				high = append(high, c.Role)
			} else {
				standard = append(standard, c.Role)
			}
		}
		if len(high) > 0 {
			b.WriteString(fmt.Sprintf("- %s — use the most capable model available\n", strings.Join(high, ", ")))
		}
		if len(standard) > 0 {
			b.WriteString(fmt.Sprintf("- %s — standard model is sufficient\n", strings.Join(standard, ", ")))
		}
		b.WriteString("\n")
	}

	b.WriteString(renderControlPlane(project))

	outPath := filepath.Join(projectDir, "AGENTS.md")
	if err := managedfile.UpsertWithMode(outPath, "harnest", b.String(), 0644); err != nil {
		return "", fmt.Errorf("writing %s: %w", outPath, err)
	}

	return outPath, nil
}
