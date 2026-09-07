// Package verify enforces changed-file policy and required checks.
package verify

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/checks"
	"github.com/daniilsintsov/harnest-universal/internal/rules"
	harnestYaml "github.com/daniilsintsov/harnest-universal/internal/yaml"
)

type Result struct {
	Changed []string
	Checks  []string
	Errors  []error
}

type gitRunner func(context.Context, string, ...string) ([]byte, error)

func runGit(ctx context.Context, projectDir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
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
	changes, _, err := DiscoverChanges(projectDir, base)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, change := range changes {
		seen[change.Path] = true
	}
	files := make([]string, 0, len(seen))
	for file := range seen {
		files = append(files, file)
	}
	sort.Strings(files)
	return files, nil
}

// DiscoverChanges returns committed, staged, unstaged, and untracked changes.
func DiscoverChanges(projectDir, base string) ([]rules.Change, string, error) {
	return DiscoverChangesContext(context.Background(), projectDir, base)
}

// DiscoverChangesContext is DiscoverChanges bounded by ctx.
func DiscoverChangesContext(ctx context.Context, projectDir, base string) ([]rules.Change, string, error) {
	return discoverChanges(ctx, projectDir, base, runGit)
}

func discoverChanges(ctx context.Context, projectDir, base string, run gitRunner) ([]rules.Change, string, error) {
	gitRootOutput, err := run(ctx, projectDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, "", fmt.Errorf("cannot determine Git checkout root: %w", err)
	}
	gitRoot := strings.TrimSuffix(string(gitRootOutput), "\n")
	if gitRoot == "" {
		return nil, "", fmt.Errorf("cannot determine Git checkout root: empty output")
	}
	baseRevision, err := resolveBase(ctx, projectDir, base, run)
	if err != nil {
		return nil, "", err
	}
	var changes []rules.Change
	readDiff := func(label string, args ...string) error {
		output, err := run(ctx, projectDir, args...)
		if err != nil {
			return fmt.Errorf("git diff %s: %w", label, err)
		}
		parsed, err := parseNameStatus(output)
		if err != nil {
			return fmt.Errorf("git diff %s: %w", label, err)
		}
		changes = append(changes, parsed...)
		return nil
	}
	if baseRevision != "" {
		if err := readDiff("committed changes", "diff", "--no-relative", "--no-ext-diff", "--find-renames", "--name-status", "-z", baseRevision, "HEAD", "--"); err != nil {
			return nil, baseRevision, err
		}
	}
	if err := readDiff("staged changes", "diff", "--no-relative", "--no-ext-diff", "--find-renames", "--name-status", "-z", "--cached"); err != nil {
		return nil, baseRevision, err
	}
	if err := readDiff("unstaged changes", "diff", "--no-relative", "--no-ext-diff", "--find-renames", "--name-status", "-z"); err != nil {
		return nil, baseRevision, err
	}
	untracked, err := run(ctx, projectDir, "ls-files", "-z", "--full-name", "--others", "--exclude-standard")
	if err != nil {
		return nil, baseRevision, fmt.Errorf("git ls-files --others --exclude-standard: %w", err)
	}
	untrackedNames, err := splitNUL(untracked)
	if err != nil {
		return nil, baseRevision, fmt.Errorf("git ls-files --others --exclude-standard: %w", err)
	}
	for _, name := range untrackedNames {
		if name == "" {
			return nil, baseRevision, fmt.Errorf("git ls-files --others --exclude-standard: empty path")
		}
		changes = append(changes, rules.Change{Path: name, Operation: "create"})
	}
	changes, err = relativeChanges(projectDir, gitRoot, changes)
	if err != nil {
		return nil, baseRevision, err
	}
	seen := map[rules.Change]bool{}
	unique := changes[:0]
	for _, change := range changes {
		if change.Path != "" && !seen[change] {
			seen[change] = true
			unique = append(unique, change)
		}
	}
	sort.Slice(unique, func(i, j int) bool {
		if unique[i].Path == unique[j].Path {
			return unique[i].Operation < unique[j].Operation
		}
		return unique[i].Path < unique[j].Path
	})
	return unique, baseRevision, nil
}

func relativeChanges(projectDir, gitRoot string, changes []rules.Change) ([]rules.Change, error) {
	projectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, fmt.Errorf("resolving absolute project root: %w", err)
	}
	projectDir, err = filepath.EvalSymlinks(projectDir)
	if err != nil {
		return nil, fmt.Errorf("resolving project root: %w", err)
	}
	gitRoot, err = filepath.EvalSymlinks(gitRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving Git checkout root: %w", err)
	}
	result := make([]rules.Change, 0, len(changes))
	for _, change := range changes {
		if change.Path == "" || filepath.IsAbs(change.Path) {
			return nil, fmt.Errorf("invalid checkout-relative changed path %q", change.Path)
		}
		absolute := filepath.Join(gitRoot, filepath.FromSlash(change.Path))
		relative, err := filepath.Rel(projectDir, absolute)
		if err != nil {
			return nil, fmt.Errorf("normalizing changed path %q: %w", change.Path, err)
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			continue
		}
		result = append(result, rules.Change{Path: filepath.ToSlash(relative), Operation: change.Operation})
	}
	return result, nil
}

func parseNameStatus(output []byte) ([]rules.Change, error) {
	fields, err := splitNUL(output)
	if err != nil {
		return nil, err
	}
	var changes []rules.Change
	for i := 0; i < len(fields); {
		status := fields[i]
		i++
		if status == "" {
			return nil, fmt.Errorf("empty git status")
		}
		if i >= len(fields) {
			return nil, fmt.Errorf("missing path after status %q", status)
		}
		code := status[0]
		if (code == 'R' || code == 'C') && !validSimilarity(status) {
			return nil, fmt.Errorf("invalid git status %q", status)
		}
		if code != 'R' && code != 'C' && len(status) != 1 {
			return nil, fmt.Errorf("invalid git status %q", status)
		}
		if fields[i] == "" {
			return nil, fmt.Errorf("empty path after status %q", status)
		}
		switch code {
		case 'A':
			changes = append(changes, rules.Change{Path: fields[i], Operation: "create"})
			i++
		case 'D':
			changes = append(changes, rules.Change{Path: fields[i], Operation: "delete"})
			i++
		case 'M', 'T', 'U', 'X', 'B':
			changes = append(changes, rules.Change{Path: fields[i], Operation: "update"})
			i++
		case 'R':
			if i+1 >= len(fields) {
				return nil, fmt.Errorf("missing rename paths after status %q", status)
			}
			if fields[i+1] == "" {
				return nil, fmt.Errorf("empty rename path after status %q", status)
			}
			changes = append(changes, rules.Change{Path: fields[i], Operation: "move"}, rules.Change{Path: fields[i+1], Operation: "move"})
			i += 2
		case 'C':
			if i+1 >= len(fields) {
				return nil, fmt.Errorf("missing copy paths after status %q", status)
			}
			if fields[i+1] == "" {
				return nil, fmt.Errorf("empty copy path after status %q", status)
			}
			changes = append(changes, rules.Change{Path: fields[i+1], Operation: "create"})
			i += 2
		default:
			return nil, fmt.Errorf("unsupported git status %q", status)
		}
	}
	return changes, nil
}

func validSimilarity(status string) bool {
	if len(status) < 2 {
		return false
	}
	value := 0
	for _, digit := range status[1:] {
		if digit < '0' || digit > '9' {
			return false
		}
		value = value*10 + int(digit-'0')
	}
	return value <= 100
}

func splitNUL(output []byte) ([]string, error) {
	if len(output) == 0 {
		return nil, nil
	}
	if output[len(output)-1] != 0 {
		return nil, fmt.Errorf("truncated NUL-delimited git output")
	}
	parts := bytes.Split(output, []byte{0})
	result := make([]string, 0, len(parts)-1)
	for _, part := range parts[:len(parts)-1] {
		result = append(result, string(part))
	}
	return result, nil
}

func resolveBase(ctx context.Context, projectDir, explicit string, run gitRunner) (string, error) {
	if explicit != "" {
		if strings.HasPrefix(explicit, "-") || strings.ContainsAny(explicit, "\x00\r\n") {
			return "", fmt.Errorf("invalid base ref %q", explicit)
		}
		base, err := run(ctx, projectDir, "merge-base", "--", explicit, "HEAD")
		if err != nil {
			return "", fmt.Errorf("cannot resolve base %q: %w", explicit, err)
		}
		return strings.TrimSpace(string(base)), nil
	}

	var candidates []string
	if defaultBranch, err := run(ctx, projectDir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		candidates = append(candidates, strings.TrimSpace(string(defaultBranch)))
	}
	candidates = append(candidates, "origin/main", "origin/master", "main", "master")
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		base, err := run(ctx, projectDir, "merge-base", "--", candidate, "HEAD")
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
	changes, _, err := DiscoverChanges(projectDir, baseRef)
	if err != nil {
		return Result{}, err
	}
	changed := uniquePaths(changes)
	result := Result{Changed: changed}
	requiredChecks := map[string]bool{}

	for _, rule := range project.PolicyRules {
		for _, enforcement := range rule.Enforcement {
			switch enforcement.Type {
			case "protect-path":
				matched, matchErr := anyChangeMatches(rule.Scope, enforcement.Paths, changes)
				if matchErr != nil {
					return Result{}, fmt.Errorf("rule %s: %w", rule.ID, matchErr)
				}
				if !allowed[rule.ID] && matched {
					result.Errors = append(result.Errors, fmt.Errorf("rule %s protects a changed path; allow explicitly with --allow %s", rule.ID, rule.ID))
				}
			case "require-check":
				matched, matchErr := anyChangeMatches(rule.Scope, nil, changes)
				if matchErr != nil {
					return Result{}, fmt.Errorf("rule %s: %w", rule.ID, matchErr)
				}
				if matched {
					requiredChecks[enforcement.Check] = true
				}
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

func uniquePaths(changes []rules.Change) []string {
	seen := map[string]bool{}
	for _, change := range changes {
		seen[change.Path] = true
	}
	paths := make([]string, 0, len(seen))
	for file := range seen {
		paths = append(paths, file)
	}
	sort.Strings(paths)
	return paths
}

func anyChangeMatches(scope rules.Scope, protected []string, changes []rules.Change) (bool, error) {
	for _, change := range changes {
		matched, err := rules.MatchesChange(scope, protected, change)
		if err != nil || matched {
			return matched, err
		}
	}
	return false, nil
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
			if matched, _ := rules.MatchPaths([]string{pattern}, file); matched {
				return true
			}
		}
	}
	return false
}
