// Package doctor checks whether configured policy can actually be enforced.
package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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
	Info    Level = "info"
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

	selected, err := rules.SelectHooks(project.PolicyRules, project.Hooks.Rules)
	if err != nil {
		return report, err
	}
	selectedIDs := map[string]bool{}
	for _, rule := range selected {
		selectedIDs[rule.ID] = true
		scope := strings.Join(rule.Scope.Paths, ", ")
		if scope == "" {
			scope = "entire project"
		}
		report.Items = append(report.Items, Item{Info, fmt.Sprintf("hook rule %s: scope=%s; operations=%v; applicability is evaluated at each event", rule.ID, scope, rule.Scope.Operations)})
		for _, enforcement := range rule.Enforcement {
			if enforcement.Type != "require-check" {
				continue
			}
			check, err := checks.Load(projectDir, project.Checks.Root, enforcement.Check)
			if err != nil {
				report.Items = append(report.Items, Item{Error, fmt.Sprintf("hook rule %s: %v", rule.ID, err)})
			} else if !check.Approved {
				report.Items = append(report.Items, Item{Error, fmt.Sprintf("hook rule %s: check %s is unapproved", rule.ID, check.ID)})
			} else if !executableAvailable(projectDir, check.Command) {
				report.Items = append(report.Items, Item{Error, fmt.Sprintf("hook rule %s: executable for check %s is unavailable", rule.ID, check.ID)})
			}
		}
	}
	for _, rule := range project.PolicyRules {
		if rule.Severity != rules.Hard {
			continue
		}
		if !selectedIDs[rule.ID] {
			report.Items = append(report.Items, Item{Warning, fmt.Sprintf("hard rule %s is not selected in hooks.rules; manual verification only", rule.ID)})
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
	if len(selected) > 0 {
		if !project.Hooks.Enabled {
			report.Items = append(report.Items, Item{Warning, "native hooks disabled by .harnest-local.yaml; wiring remains installed"})
		}
		diagnostics, err := harness.InspectHooks(projectDir, project)
		if err != nil {
			report.Items = append(report.Items, Item{Error, "cannot inspect native hook sources: " + err.Error()})
		} else {
			for _, target := range project.Targets {
				for _, diagnostic := range diagnostics {
					if target != diagnostic.Platform {
						continue
					}
					switch {
					case diagnostic.Issue != "":
						report.Items = append(report.Items, Item{Error, target + " hooks: " + diagnostic.Issue})
					case !diagnostic.Installed:
						report.Items = append(report.Items, Item{Warning, target + " hooks not installed: " + diagnostic.Path + "; run harnest generate"})
					case !executableAvailable(projectDir, diagnostic.Executable):
						report.Items = append(report.Items, Item{Error, target + " hook executable unavailable; regenerate with an installed harnest binary"})
					default:
						report.Items = append(report.Items, Item{Warning, target + " hooks installed, execution not verified: " + diagnostic.Path + "; native trust/reload unknown, review in host and run smoke"})
					}
				}
			}
		}
		report.Items = append(report.Items, Item{Info, "hook coverage: supported file tools only before execution; Stop discovers branch/staged/unstaged/untracked Git changes; shell/MCP only after changes, ignored/external actions outside coverage; CI remains the acceptance gate"})
	}
	return report, nil
}

func executableAvailable(dir, name string) bool {
	if name == "" {
		return false
	}
	if !filepath.IsAbs(name) && !strings.ContainsAny(name, `/\`) {
		_, err := exec.LookPath(name)
		return err == nil
	}
	if !filepath.IsAbs(name) {
		name = filepath.Join(dir, name)
	}
	info, err := os.Stat(name)
	return err == nil && info.Mode().IsRegular() && (runtime.GOOS == "windows" || info.Mode()&0111 != 0)
}

func (r Report) Healthy() bool {
	for _, item := range r.Items {
		if item.Level == Error {
			return false
		}
	}
	return true
}
