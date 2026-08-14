package drift

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/config"
)

// FixResult holds the outcome of a fix operation.
type FixResult struct {
	Fixed   []DriftItem // items that were auto-fixed
	Skipped []DriftItem // items that require interactive decision
	Errors  []error     // per-item fix errors
}

// Fix auto-fixes all auto-fixable drift items found in result by modifying the
// project config file (CLAUDE.md / .cursorrules / .windsurfrules) in-place.
//
// DriftScope items (dead exec scope) are fixed by removing their table row.
// DriftStack items (uncovered new stack) are fixed by appending a new row with
// a suggested agent and scope derived from the stack's category and language.
// DriftAgent items are not auto-fixable and are returned in Skipped.
//
// An error is returned only when the config file cannot be read or written.
// Per-item fix errors are collected in FixResult.Errors so that the caller can
// report partial success.
func Fix(dir string, result *DriftResult) (*FixResult, error) {
	configPath := config.ConfigFilePath(dir)
	if configPath == "" {
		return nil, fmt.Errorf("no config file found in %s", dir)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	content := string(data)
	fr := &FixResult{}

	for _, item := range result.Items {
		if !item.AutoFixable {
			fr.Skipped = append(fr.Skipped, item)
			continue
		}

		var fixErr error

		switch item.Type {
		case DriftScope:
			content, fixErr = fixDeadScope(content, item)
		case DriftStack:
			content, fixErr = fixMissingStack(content, item)
		default:
			// Unknown type — treat as non-fixable.
			fr.Skipped = append(fr.Skipped, item)
			continue
		}

		if fixErr != nil {
			fr.Errors = append(fr.Errors, fmt.Errorf("fix %s (%s): %w", item.Type, item.Scope, fixErr))
		} else {
			fr.Fixed = append(fr.Fixed, item)
		}
	}

	if len(fr.Fixed) == 0 {
		// Nothing changed; skip the write.
		return fr, nil
	}

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("writing config file: %w", err)
	}

	return fr, nil
}

// ---------------------------------------------------------------------------
// Fix strategies
// ---------------------------------------------------------------------------

// fixDeadScope removes the exec table row whose scope matches item.Scope.
//
// It targets a line of the form:
//
//	| <agent> | <scope> |
//
// within the ### Executing section and deletes it (along with its newline).
func fixDeadScope(content string, item DriftItem) (string, error) {
	// Build a pattern that matches the exact row — agent and scope cells.
	// We allow arbitrary surrounding whitespace inside each cell.
	pattern := fmt.Sprintf(
		`(?m)^\|\s*%s\s*\|\s*%s\s*\|[^\n]*\n?`,
		regexp.QuoteMeta(item.Current),
		regexp.QuoteMeta(item.Scope),
	)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return content, fmt.Errorf("building removal pattern: %w", err)
	}

	if !re.MatchString(content) {
		return content, fmt.Errorf("row for agent '%s' scope '%s' not found in config", item.Current, item.Scope)
	}

	return re.ReplaceAllString(content, ""), nil
}

// fixMissingStack appends a new exec agent row to the ### Executing table for
// the stack described by item.
//
// The suggested agent name is derived from the stack's category (item.Stack
// holds the stack name, item.Expected holds the stack path).  The scope is
// constructed from the stack path and the canonical file extension for the
// detected language.
func fixMissingStack(content string, item DriftItem) (string, error) {
	agentName := suggestAgent(item.Stack)
	scope := suggestScope(item.Stack, item.Expected)

	newRow := fmt.Sprintf("| %s | %s |", agentName, scope)

	// Find the ### Executing section header.
	execIdx := findExecutingSection(content)
	if execIdx == -1 {
		return content, fmt.Errorf("no ### Executing section found in config")
	}

	// Find the end of the Executing section: first occurrence of a line that
	// starts a new ## or ### heading after the section start.
	rest := content[execIdx:]
	insertOffset := findSectionEnd(rest)
	insertPos := execIdx + insertOffset

	// Insert the new row with a trailing newline.
	content = content[:insertPos] + newRow + "\n" + content[insertPos:]
	return content, nil
}

// ---------------------------------------------------------------------------
// Suggestion helpers
// ---------------------------------------------------------------------------

// categoryToAgent maps stack categories to sensible default agent names.
var categoryToAgent = map[string]string{
	"backend":  "general-purpose",
	"frontend": "general-purpose",
	"mobile":   "general-purpose",
	"shared":   "general-purpose",
	"desktop":  "general-purpose",
	"infra":    "general-purpose",
	"data":     "general-purpose",
}

// stackToExtension maps well-known stack names to their primary file extension.
// Falls back to a wildcard when the stack is not listed.
var stackToExtension = map[string]string{
	// Go
	"go":    "*.go",
	"gin":   "*.go",
	"echo":  "*.go",
	"fiber": "*.go",
	"chi":   "*.go",
	// Kotlin / JVM
	"spring-boot": "*.kt",
	"ktor":        "*.kt",
	"kotlin":      "*.kt",
	"java":        "*.java",
	"gradle":      "*.kt",
	"maven":       "*.java",
	// Swift / Apple
	"swift":     "*.swift",
	"swiftui":   "*.swift",
	"uikit":     "*.swift",
	"xcodeproj": "*.swift",
	// JavaScript / TypeScript
	"vue":        "*.{ts,vue}",
	"react":      "*.{ts,tsx}",
	"next":       "*.{ts,tsx}",
	"angular":    "*.ts",
	"svelte":     "*.svelte",
	"nuxt":       "*.{ts,vue}",
	"vite":       "*.{ts,tsx}",
	"remix":      "*.{ts,tsx}",
	"express":    "*.ts",
	"fastify":    "*.ts",
	"nestjs":     "*.ts",
	"node":       "*.ts",
	"javascript": "*.js",
	"typescript": "*.ts",
	// Python
	"python":  "*.py",
	"django":  "*.py",
	"fastapi": "*.py",
	"flask":   "*.py",
	"celery":  "*.py",
	// Rust
	"rust":  "*.rs",
	"axum":  "*.rs",
	"actix": "*.rs",
	// Flutter / Dart
	"flutter": "*.dart",
	"dart":    "*.dart",
	// Ruby
	"ruby":    "*.rb",
	"rails":   "*.rb",
	"sinatra": "*.rb",
	// PHP
	"php":     "*.php",
	"laravel": "*.php",
	"symfony": "*.php",
	// .NET / C#
	"dotnet": "*.cs",
	"aspnet": "*.cs",
	"csharp": "*.cs",
	"blazor": "*.cs",
	// Elixir / Erlang
	"elixir":  "*.ex",
	"phoenix": "*.ex",
	"erlang":  "*.erl",
	// Scala
	"scala": "*.scala",
	// Clojure
	"clojure": "*.clj",
	// Haskell
	"haskell": "*.hs",
	// C / C++
	"c":   "*.c",
	"cpp": "*.cpp",
	// Infra
	"terraform":  "*.tf",
	"docker":     "Dockerfile",
	"kubernetes": "*.yaml",
	"helm":       "*.yaml",
	"ansible":    "*.yaml",
	"pulumi":     "*.ts",
	// Data
	"jupyter": "*.ipynb",
	"r":       "*.R",
	"julia":   "*.jl",
}

// suggestAgent returns a default agent name for a given stack name.
// Currently always returns "general-purpose"; callers can refine later.
func suggestAgent(stackName string) string {
	// Future: derive from detected installed agents or category mapping.
	// For now, a safe universal default is used so the file always compiles.
	return "general-purpose"
}

// suggestScope constructs a glob scope for a stack given its name and path.
//
// Examples:
//
//	stack="go", path="."   → "**/*.go"
//	stack="vue", path="web" → "web/**/*.{ts,vue}"
func suggestScope(stackName, stackPath string) string {
	stackLower := strings.ToLower(stackName)

	ext, ok := stackToExtension[stackLower]
	if !ok {
		ext = "*"
	}

	base := "**/" + ext

	stackPath = strings.TrimSuffix(stackPath, "/")
	if stackPath == "" || stackPath == "." || stackPath == "./" {
		return base
	}

	return stackPath + "/" + base
}

// ---------------------------------------------------------------------------
// Section location helpers
// ---------------------------------------------------------------------------

// findExecutingSection returns the index in content immediately after the
// "### Executing" (or localised equivalent) header line.
// Returns -1 if the section is not found.
func findExecutingSection(content string) int {
	for _, header := range []string{"### Executing"} {
		idx := strings.Index(content, header)
		if idx != -1 {
			// Advance past the header line (to the first character of the next line).
			nl := strings.Index(content[idx:], "\n")
			if nl == -1 {
				return len(content)
			}
			return idx + nl + 1
		}
	}
	return -1
}

// findSectionEnd returns the offset within s where the Executing section ends.
// The end is defined as the start of the first line that begins a new heading
// (## or ###) after the current section. If no such line exists, the end of
// the string is returned.
//
// The returned offset is intended to be used as an insertion point so that new
// rows are appended at the bottom of the section, just before the next heading.
func findSectionEnd(s string) int {
	lines := strings.Split(s, "\n")
	pos := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip the very first line (it is the header or the blank line after it).
		if i > 0 && (strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ")) {
			return pos
		}
		pos += len(line) + 1 // +1 for the \n that was removed by Split
	}
	return len(s)
}
