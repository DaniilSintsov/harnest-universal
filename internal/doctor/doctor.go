// Package doctor checks whether configured policy can actually be enforced.
package doctor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/daniilsintsov/harnest-universal/internal/checks"
	"github.com/daniilsintsov/harnest-universal/internal/harness"
	"github.com/daniilsintsov/harnest-universal/internal/ir"
	"github.com/daniilsintsov/harnest-universal/internal/rules"
	harnestYaml "github.com/daniilsintsov/harnest-universal/internal/yaml"
)

type Level string

const (
	Error   Level = "error"
	Warning Level = "warning"
)

type Item struct {
	Level   Level
	Message string
}

type Report struct {
	Capabilities map[string]ir.Capabilities
	Items        []Item
}

func Check(projectDir string) (Report, error) {
	cfg, err := harnestYaml.Load(projectDir)
	if err != nil {
		return Report{}, err
	}
	project, err := harnestYaml.BuildIR(projectDir, cfg)
	if err != nil {
		return Report{}, err
	}
	report := Report{Capabilities: map[string]ir.Capabilities{}}
	if len(project.Targets) == 0 {
		report.Items = append(report.Items, Item{Error, "no target adapters configured"})
	}

	if project.Architecture.Index != "" {
		path := project.Architecture.Index
		if !filepath.IsAbs(path) {
			path = filepath.Join(projectDir, path)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			report.Items = append(report.Items, Item{Warning, fmt.Sprintf("architecture index missing: %s", project.Architecture.Index)})
		}
	}

	for _, target := range project.Targets {
		caps, err := harness.Capabilities(target)
		if err != nil {
			report.Items = append(report.Items, Item{Error, err.Error()})
			continue
		}
		report.Capabilities[target] = caps
		if target != "claude-code" && target != "codex" {
			report.Items = append(report.Items, Item{Warning, fmt.Sprintf("%s is legacy compatibility only in v1", target)})
		}
	}

	for _, rule := range project.PolicyRules {
		if rule.Severity != rules.Hard {
			continue
		}
		for _, enforcement := range rule.Enforcement {
			switch enforcement.Type {
			case "protect-path":
				for target, caps := range report.Capabilities {
					if caps.Verification != ir.Native {
						report.Items = append(report.Items, Item{Error, fmt.Sprintf("hard rule %s cannot protect paths mechanically on %s; adapter provides manual verification only", rule.ID, target)})
					}
				}
			case "require-check":
				check, err := checks.Load(projectDir, project.Checks.Root, enforcement.Check)
				if err != nil {
					report.Items = append(report.Items, Item{Error, fmt.Sprintf("hard rule %s: %v", rule.ID, err)})
				} else if !check.Approved {
					report.Items = append(report.Items, Item{Error, fmt.Sprintf("hard rule %s uses unapproved check %s", rule.ID, check.ID)})
				}
				for target, caps := range report.Capabilities {
					if caps.Verification != ir.Native {
						report.Items = append(report.Items, Item{Error, fmt.Sprintf("hard rule %s cannot require check %s mechanically on %s; adapter provides manual verification only", rule.ID, enforcement.Check, target)})
					}
				}
			}
		}
	}
	return report, nil
}

func (r Report) Healthy() bool {
	for _, item := range r.Items {
		if item.Level == Error {
			return false
		}
	}
	return true
}
