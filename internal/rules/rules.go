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

// Change describes one observable filesystem change.
type Change struct {
	Path      string
	Operation string
}

var validOperations = map[string]bool{
	"change": true,
	"create": true,
	"update": true,
	"delete": true,
	"move":   true,
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

// SelectHooks resolves and validates the explicitly selected hook rules.
func SelectHooks(all []Rule, ids []string) ([]Rule, error) {
	byID := make(map[string]Rule, len(all))
	for _, rule := range all {
		byID[rule.ID] = rule
	}
	selected := make([]Rule, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return nil, fmt.Errorf("duplicate hook rule id %q", id)
		}
		seen[id] = true
		rule, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("unknown hook rule id %q", id)
		}
		if err := validateHookRule(rule); err != nil {
			return nil, err
		}
		selected = append(selected, rule)
	}
	return selected, nil
}

func validateHookRule(rule Rule) error {
	if err := Validate(rule); err != nil {
		return fmt.Errorf("hook rule %q: %w", rule.ID, err)
	}
	if len(rule.Scope.Paths) == 0 && len(rule.Scope.Domains) > 0 {
		return fmt.Errorf("hook rule %q has domain-only scope", rule.ID)
	}
	if err := validatePatterns(rule.Scope.Paths); err != nil {
		return fmt.Errorf("hook rule %q scope: %w", rule.ID, err)
	}
	for _, operation := range rule.Scope.Operations {
		if !validOperations[operation] {
			return fmt.Errorf("hook rule %q has invalid operation %q", rule.ID, operation)
		}
	}
	if rule.Severity == Preference {
		return fmt.Errorf("hook rule %q has unsupported preference severity", rule.ID)
	}
	for _, enforcement := range rule.Enforcement {
		if err := validatePatterns(enforcement.Paths); err != nil {
			return fmt.Errorf("hook rule %q enforcement: %w", rule.ID, err)
		}
		switch {
		case enforcement.Type == "protect-path" && (rule.Severity == Hard || rule.Severity == Required):
		case enforcement.Type == "require-check" && rule.Severity == Required:
		default:
			return fmt.Errorf("hook rule %q has unsupported %s + %s enforcement", rule.ID, rule.Severity, enforcement.Type)
		}
	}
	if len(rule.Enforcement) == 0 {
		return fmt.Errorf("hook rule %q has no enforcement", rule.ID)
	}
	return nil
}

func validatePatterns(patterns []string) error {
	for _, pattern := range patterns {
		for _, segment := range strings.Split(normalizePath(pattern), "/") {
			if segment == "**" {
				continue
			}
			if _, err := filepath.Match(segment, ""); err != nil {
				return fmt.Errorf("invalid path glob %q: %w", pattern, err)
			}
		}
	}
	return nil
}

// MatchPaths reports whether file matches any glob. Empty patterns match all files.
func MatchPaths(patterns []string, file string) (bool, error) {
	if err := validatePatterns(patterns); err != nil {
		return false, err
	}
	if len(patterns) == 0 {
		return true, nil
	}
	for _, pattern := range patterns {
		if matchSegments(strings.Split(normalizePath(pattern), "/"), strings.Split(normalizePath(file), "/")) {
			return true, nil
		}
	}
	return false, nil
}

// MatchesChange applies scope and enforcement paths to the same changed path.
func MatchesChange(scope Scope, protectedPaths []string, change Change) (bool, error) {
	operationMatches := len(scope.Operations) == 0
	for _, operation := range scope.Operations {
		if !validOperations[operation] {
			return false, fmt.Errorf("invalid operation %q", operation)
		}
		if operation == "change" || operation == change.Operation {
			operationMatches = true
		}
	}
	if !validOperations[change.Operation] {
		return false, fmt.Errorf("invalid change operation %q", change.Operation)
	}
	if !operationMatches {
		return false, nil
	}
	scopeMatch, err := MatchPaths(scope.Paths, change.Path)
	if err != nil || !scopeMatch {
		return false, err
	}
	return MatchPaths(protectedPaths, change.Path)
}

func normalizePath(value string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(value)), "./")
}

func matchSegments(pattern, file []string) bool {
	if len(pattern) == 0 {
		return len(file) == 0
	}
	if pattern[0] == "**" {
		return matchSegments(pattern[1:], file) || (len(file) > 0 && matchSegments(pattern, file[1:]))
	}
	if len(file) == 0 {
		return false
	}
	matched, _ := filepath.Match(pattern[0], file[0])
	return matched && matchSegments(pattern[1:], file[1:])
}
