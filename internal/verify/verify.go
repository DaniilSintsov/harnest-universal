// Package verify enforces changed-file policy and required checks.
package verify

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/checks"
	harnestYaml "github.com/daniilsintsov/harnest-universal/internal/yaml"
)

type Result struct {
	Changed []string
	Checks  []string
	Errors  []error
}

var gitOutput = func(projectDir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = projectDir
	return cmd.Output()
}

func ChangedFiles(projectDir string) ([]string, error) {
	return ChangedFilesSince(projectDir, "")
}

// ChangedFilesSince returns committed changes since the merge-base plus all
// staged, unstaged, and untracked files. An empty base auto-detects the
// repository's mainline and falls back to worktree-only mode when unavailable.
func ChangedFilesSince(projectDir, base string) ([]string, error) {
	var outputs [][]byte
	baseRevision, err := resolveBase(projectDir, base)
	if err != nil {
		return nil, err
	}
	if baseRevision != "" {
		committed, err := gitOutput(projectDir, "diff", "--name-only", baseRevision, "HEAD", "--")
		if err != nil {
			return nil, fmt.Errorf("git diff committed changes: %w", err)
		}
		outputs = append(outputs, committed)
	}
	cached, cachedErr := gitOutput(projectDir, "diff", "--name-only", "--cached")
	working, workingErr := gitOutput(projectDir, "diff", "--name-only")
	if cachedErr != nil && workingErr != nil {
		return nil, fmt.Errorf("git diff working tree: staged: %v; unstaged: %v", cachedErr, workingErr)
	}
	outputs = append(outputs, cached, working)

	untracked, err := gitOutput(projectDir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("git ls-files --others --exclude-standard: %w", err)
	}
	outputs = append(outputs, untracked)

	seen := map[string]bool{}
	for _, output := range outputs {
		scanner := bufio.NewScanner(bytes.NewReader(output))
		for scanner.Scan() {
			name := strings.TrimSpace(scanner.Text())
			if name != "" {
				seen[name] = true
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	files := make([]string, 0, len(seen))
	for file := range seen {
		files = append(files, file)
	}
	sort.Strings(files)
	return files, nil
}

func resolveBase(projectDir, explicit string) (string, error) {
	if explicit != "" {
		if strings.HasPrefix(explicit, "-") || strings.ContainsAny(explicit, "\x00\r\n") {
			return "", fmt.Errorf("invalid base ref %q", explicit)
		}
		base, err := gitOutput(projectDir, "merge-base", "--", explicit, "HEAD")
		if err != nil {
			return "", fmt.Errorf("cannot resolve base %q: %w", explicit, err)
		}
		return strings.TrimSpace(string(base)), nil
	}

	var candidates []string
	if defaultBranch, err := gitOutput(projectDir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		candidates = append(candidates, strings.TrimSpace(string(defaultBranch)))
	}
	candidates = append(candidates, "origin/main", "origin/master", "main", "master")
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		base, err := gitOutput(projectDir, "merge-base", "--", candidate, "HEAD")
		if err == nil && strings.TrimSpace(string(base)) != "" {
			return strings.TrimSpace(string(base)), nil
		}
	}
	return "", nil
}

func Run(projectDir string, allowed map[string]bool, base ...string) (Result, error) {
	cfg, err := harnestYaml.Load(projectDir)
	if err != nil {
		return Result{}, err
	}
	project, err := harnestYaml.BuildIR(projectDir, cfg)
	if err != nil {
		return Result{}, err
	}
	baseRef := ""
	if len(base) > 0 {
		baseRef = base[0]
	}
	changed, err := ChangedFilesSince(projectDir, baseRef)
	if err != nil {
		return Result{}, err
	}
	result := Result{Changed: changed}
	requiredChecks := map[string]bool{}

	for _, rule := range project.PolicyRules {
		if !applies(rule.Scope.Paths, changed) {
			continue
		}
		for _, enforcement := range rule.Enforcement {
			switch enforcement.Type {
			case "protect-path":
				if !allowed[rule.ID] && applies(enforcement.Paths, changed) {
					result.Errors = append(result.Errors, fmt.Errorf("rule %s protects a changed path; allow explicitly with --allow %s", rule.ID, rule.ID))
				}
			case "require-check":
				requiredChecks[enforcement.Check] = true
			}
		}
	}

	ids := make([]string, 0, len(requiredChecks))
	for id := range requiredChecks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		check, err := checks.Load(projectDir, project.Checks.Root, id)
		if err == nil {
			err = checks.Run(projectDir, check, changed)
		}
		if err != nil {
			result.Errors = append(result.Errors, err)
			continue
		}
		result.Checks = append(result.Checks, id)
	}
	return result, nil
}

func applies(patterns, files []string) bool {
	if len(files) == 0 {
		return false
	}
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		for _, file := range files {
			if match(pattern, file) {
				return true
			}
		}
	}
	return false
}

func match(pattern, file string) bool {
	pattern = strings.TrimPrefix(path.Clean(strings.ReplaceAll(pattern, "\\", "/")), "./")
	file = strings.TrimPrefix(path.Clean(strings.ReplaceAll(file, "\\", "/")), "./")
	return matchSegments(strings.Split(pattern, "/"), strings.Split(file, "/"))
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
	matched, err := path.Match(pattern[0], file[0])
	return err == nil && matched && matchSegments(pattern[1:], file[1:])
}
