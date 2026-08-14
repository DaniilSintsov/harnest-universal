// Package rules loads and validates declarative project rules.
package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	goyaml "gopkg.in/yaml.v3"
)

type Severity string

const (
	Hard       Severity = "hard"
	Required   Severity = "required"
	Preference Severity = "preference"
)

type Rule struct {
	ID          string        `yaml:"id"`
	Title       string        `yaml:"title,omitempty"`
	Severity    Severity      `yaml:"severity"`
	Statement   string        `yaml:"statement"`
	Scope       Scope         `yaml:"scope,omitempty"`
	Enforcement []Enforcement `yaml:"enforcement,omitempty"`
	Source      Source        `yaml:"source,omitempty"`
}

type Scope struct {
	Paths      []string `yaml:"paths,omitempty"`
	Domains    []string `yaml:"domains,omitempty"`
	Operations []string `yaml:"operations,omitempty"`
}

type Enforcement struct {
	Type     string   `yaml:"type"`
	Paths    []string `yaml:"paths,omitempty"`
	Commands []string `yaml:"commands,omitempty"`
	Check    string   `yaml:"check,omitempty"`
}

type Source struct {
	Type     string   `yaml:"type,omitempty"`
	Evidence []string `yaml:"evidence,omitempty"`
}

func Load(projectDir, root string) ([]Rule, error) {
	if root == "" {
		return nil, nil
	}
	dir := root
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(projectDir, root)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var loaded []Rule
	seen := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml")) || strings.HasPrefix(strings.ToUpper(entry.Name()), "INDEX.") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var rule Rule
		if err := goyaml.Unmarshal(data, &rule); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		if err := Validate(rule); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if previous := seen[rule.ID]; previous != "" {
			return nil, fmt.Errorf("duplicate rule id %q in %s and %s", rule.ID, previous, path)
		}
		seen[rule.ID] = path
		loaded = append(loaded, rule)
	}
	sort.Slice(loaded, func(i, j int) bool { return loaded[i].ID < loaded[j].ID })
	return loaded, nil
}

func Validate(rule Rule) error {
	if strings.TrimSpace(rule.ID) == "" || strings.ContainsAny(rule.ID, " \t\r\n") {
		return fmt.Errorf("rule id must be non-empty and contain no whitespace")
	}
	if strings.TrimSpace(rule.Statement) == "" {
		return fmt.Errorf("rule %q has empty statement", rule.ID)
	}
	switch rule.Severity {
	case Hard, Required, Preference:
	default:
		return fmt.Errorf("rule %q has invalid severity %q", rule.ID, rule.Severity)
	}
	if rule.Severity == Hard && len(rule.Enforcement) == 0 {
		return fmt.Errorf("hard rule %q requires mechanical enforcement", rule.ID)
	}
	for _, enforcement := range rule.Enforcement {
		switch enforcement.Type {
		case "protect-path":
			if len(enforcement.Paths) == 0 {
				return fmt.Errorf("protect-path enforcement for %q requires paths", rule.ID)
			}
		case "require-check":
			if enforcement.Check == "" {
				return fmt.Errorf("require-check enforcement for %q requires check", rule.ID)
			}
		case "deny-command":
			return fmt.Errorf("rule %q uses unsupported enforcement %q; supported enforcement types: protect-path, require-check", rule.ID, enforcement.Type)
		default:
			return fmt.Errorf("rule %q uses unknown enforcement %q", rule.ID, enforcement.Type)
		}
	}
	return nil
}
