package profile

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/managedfile"
)

var builtinProfiles = map[string]string{
	"business-feature": businessFeature,
	"bug-hunting":      bugHunting,
	"code-review":      codeReview,
	"coordinator":      coordinator,
	"e2e-testing":      e2eTesting,
	"redesign":         redesign,
	"research":         research,
	"refactoring":      refactoring,
	"strat-session":    stratSession,
	"task-creation":    taskCreation,
}

var codexProfileReplacer = strings.NewReplacer(
	"~/.claude/projects/-Users-neuradev/memory/coordinator/", "~/.codex/coordinator/",
	"~/.claude/projects/-Users-<user>/memory/…", "~/.codex/memory/…",
	"~/.claude/", "~/.codex/",
	"CLAUDE.md", "AGENTS.md",
	"`AskUserQuestion`", "`request_user_input` (если доступен) или прямой вопрос пользователю",
	"AskUserQuestion", "request_user_input",
	"Task tool", "Codex subagent workflow",
	"Read tool", "file-reading tools",
	"playwright-cli", "browser tooling",
	"Bash", "shell",
	"`Explore`", "`explorer`",
	"Explore", "explorer",
	"`general-purpose`", "`default`",
	"general-purpose", "default",
	"через `/loop`", "через recurring automation",
	"skill `/deploy`", "доступный deployment skill",
	"/test-android", "доступный Android test skill",
	"/test-ios", "доступный iOS test skill",
	"/test-desktop", "доступный Desktop test skill",
	"voltagent-biz:", "",
	"voltagent-core-dev:", "",
	"voltagent-infra:", "",
	"voltagent-dev-exp:", "",
	"voltagent-lang:", "",
	"voltagent-плагины", "Codex agents",
	"builder-spring-feature", "spring-boot-engineer",
	"kotlin-multiplatform-developer", "kotlin-specialist",
	"opus", "sol",
	"sonnet", "terra",
	"haiku", "luna",
)

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func ValidateName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid profile name %q: must match [a-zA-Z0-9][a-zA-Z0-9_-]{0,63}", name)
	}
	return nil
}

func profilesDir() (string, error) {
	baseDir, err := defaultBaseDir()
	if err != nil {
		return "", err
	}
	return profilesDirIn(baseDir), nil
}

func defaultBaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

func profilesDirIn(baseDir string) string { return filepath.Join(baseDir, "profiles") }

func safePath(name string) (string, error) {
	baseDir, err := defaultBaseDir()
	if err != nil {
		return "", err
	}
	return safePathIn(name, baseDir)
}

func safePathIn(name, baseDir string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	dir := profilesDirIn(baseDir)
	path := filepath.Join(dir, name+".md")
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absPath, absDir+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal detected: %s", name)
	}
	return absPath, nil
}

// BuiltinNames returns sorted list of builtin profile names.
func BuiltinNames() []string {
	names := make([]string, 0, len(builtinProfiles))
	for k := range builtinProfiles {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// IsBuiltin checks if name is a builtin profile.
func IsBuiltin(name string) bool {
	_, ok := builtinProfiles[name]
	return ok
}

// BuiltinContent returns builtin profile content.
func BuiltinContent(name string) (string, bool) {
	return BuiltinContentFor(name, "claude-code")
}

// BuiltinContentFor returns builtin content adapted for a target harness.
func BuiltinContentFor(name, harnessName string) (string, bool) {
	content, ok := builtinProfiles[name]
	if !ok {
		return "", false
	}
	return adaptContentFor(content, harnessName), true
}

func adaptContentFor(content, harnessName string) string {
	if harnessName == "codex" {
		return codexProfileReplacer.Replace(content)
	}
	return content
}

func List() ([]string, error) {
	dir, err := profilesDir()
	if err != nil {
		return nil, err
	}
	return listDir(dir)
}

// ListIn lists profiles installed under a harness global directory.
func ListIn(baseDir string) ([]string, error) {
	return listDir(profilesDirIn(baseDir))
}

func listDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	return names, nil
}

// Install writes a builtin profile to disk.
func Install(name string) error {
	baseDir, err := defaultBaseDir()
	if err != nil {
		return err
	}
	return InstallToFor(name, baseDir, "claude-code")
}

// InstallTo writes a builtin profile to a custom base directory (baseDir/profiles/).
func InstallTo(name, baseDir string) error {
	return InstallToFor(name, baseDir, "claude-code")
}

// InstallToFor writes a target-adapted builtin profile to baseDir/profiles/.
func InstallToFor(name, baseDir, harnessName string) error {
	content, ok := BuiltinContentFor(name, harnessName)
	if !ok {
		return fmt.Errorf("unknown builtin profile: %s", name)
	}

	dir := filepath.Join(baseDir, "profiles")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating profiles dir: %w", err)
	}

	path := filepath.Join(dir, name+".md")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0600); err != nil {
		return fmt.Errorf("writing profile: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming profile: %w", err)
	}

	fmt.Printf("  → %s\n", path)
	return nil
}

// IsModifiedIn checks if an installed builtin profile in a custom dir differs from its template.
func IsModifiedIn(name, baseDir string) (bool, error) {
	return IsModifiedInFor(name, baseDir, "claude-code")
}

// IsModifiedInFor compares an installed profile with its target-adapted template.
func IsModifiedInFor(name, baseDir, harnessName string) (bool, error) {
	builtin, ok := BuiltinContentFor(name, harnessName)
	if !ok {
		return false, nil
	}
	path := filepath.Join(baseDir, "profiles", name+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return string(data) != builtin, nil
}

// MigrateInFor adapts an installed profile in place while preserving its source.
func MigrateInFor(name, baseDir, harnessName string) (bool, string, error) {
	path, err := safePathIn(name, baseDir)
	if err != nil {
		return false, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "", nil
		}
		return false, "", err
	}

	migrated := adaptContentFor(string(data), harnessName)
	if migrated == string(data) {
		return false, "", nil
	}

	backupPath := path + ".pre-" + harnessName + ".bak"
	if err := writeBackupOnce(backupPath, data); err != nil {
		return false, "", fmt.Errorf("creating migration backup: %w", err)
	}
	if err := managedfile.WriteAtomic(path, []byte(migrated), 0600); err != nil {
		return false, "", fmt.Errorf("migrating profile: %w", err)
	}
	return true, backupPath, nil
}

// IsModified checks if an installed builtin profile differs from its template.
func IsModified(name string) (bool, error) {
	baseDir, err := defaultBaseDir()
	if err != nil {
		return false, err
	}
	return IsModifiedInFor(name, baseDir, "claude-code")
}

// RepairBuiltinMeta inserts the builtin ## Meta block into a modified profile
// while preserving the profile body and creating a recoverable backup.
func RepairBuiltinMeta(name string) (bool, string, error) {
	baseDir, err := defaultBaseDir()
	if err != nil {
		return false, "", err
	}
	return RepairBuiltinMetaInFor(name, baseDir, "claude-code")
}

// RepairBuiltinMetaIn inserts the builtin ## Meta block into a modified profile
// in a custom harness base directory. No-op when Meta already exists.
func RepairBuiltinMetaIn(name, baseDir string) (bool, string, error) {
	return RepairBuiltinMetaInFor(name, baseDir, "claude-code")
}

// RepairBuiltinMetaInFor inserts target-adapted builtin Meta into a modified profile.
func RepairBuiltinMetaInFor(name, baseDir, harnessName string) (bool, string, error) {
	builtin, ok := BuiltinContentFor(name, harnessName)
	if !ok {
		return false, "", fmt.Errorf("unknown builtin profile: %s", name)
	}

	path := filepath.Join(baseDir, "profiles", name+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "", nil
		}
		return false, "", err
	}
	if hasMarkdownHeading(string(data), "## Meta") {
		return false, "", nil
	}

	metaBlock, err := extractBuiltinMetaBlock(builtin)
	if err != nil {
		return false, "", err
	}
	repaired, ok := insertMetaAfterTitle(string(data), metaBlock)
	if !ok || repaired == string(data) {
		return false, "", nil
	}

	backupPath := path + ".bak"
	if err := writeBackupOnce(backupPath, data); err != nil {
		return false, "", fmt.Errorf("creating backup: %w", err)
	}
	if err := managedfile.WriteAtomic(path, []byte(repaired), 0600); err != nil {
		return false, "", fmt.Errorf("repairing profile: %w", err)
	}
	return true, backupPath, nil
}

func writeBackupOnce(path string, data []byte) error {
	existing, err := os.ReadFile(path)
	if err == nil {
		if bytes.Equal(existing, data) {
			return nil
		}
		return fmt.Errorf("backup already exists with different content: %s", path)
	}
	if !os.IsNotExist(err) {
		return err
	}
	return managedfile.WriteAtomic(path, data, 0600)
}

// Remove deletes a profile from disk.
func Remove(name string) error {
	path, err := safePath(name)
	if err != nil {
		return err
	}
	return removePath(name, path)
}

// RemoveIn deletes a profile from a harness global directory.
func RemoveIn(name, baseDir string) error {
	path, err := safePathIn(name, baseDir)
	if err != nil {
		return err
	}
	return removePath(name, path)
}

func removePath(name, path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("profile not found: %s", name)
	}
	return os.Remove(path)
}

// Edit opens a profile in $EDITOR.
func Edit(name string) error {
	path, err := safePath(name)
	if err != nil {
		return err
	}
	return editPath(name, path)
}

// EditIn opens a profile from a harness global directory in $EDITOR.
func EditIn(name, baseDir string) error {
	path, err := safePathIn(name, baseDir)
	if err != nil {
		return err
	}
	return editPath(name, path)
}

func editPath(name, path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("profile not found: %s", name)
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}
	parts := strings.Fields(editor)
	bin := parts[0]
	args := append(parts[1:], path)

	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Create runs an interactive wizard to create a custom profile.
func Create(name string, r *bufio.Reader) error {
	baseDir, err := defaultBaseDir()
	if err != nil {
		return err
	}
	return CreateIn(name, baseDir, r)
}

// CreateIn runs the profile wizard for a harness global directory.
func CreateIn(name, baseDir string, r *bufio.Reader) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	path, err := safePathIn(name, baseDir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("profile already exists: %s\nUse 'harnest profiles edit %s' to modify", name, name)
	}

	fmt.Printf("\nCreating profile: %s\n", name)

	allRoles := []string{"architect", "frontend", "ui", "security", "devops", "api", "diagnostics", "test", "mobile"}
	allStages := []string{"Research", "Plan", "Executing", "Validation", "Report", "Done",
		"Reproduce", "Diagnose", "Fix", "Smoke Test",
		"Audit", "Prepare", "Deploy", "Run", "Re-run",
		"Propose", "Approve", "Save", "Verify"}

	// Step 1: Add stages one at a time
	var stages []stage
	for {
		stageNum := len(stages) + 1
		fmt.Printf("\n--- Stage %d ---\n", stageNum)
		fmt.Printf("Available stages:\n")
		for i, s := range allStages {
			fmt.Printf("  %2d) %s\n", i+1, s)
		}
		fmt.Println()

		picked := prompt(r, "Stage (number or custom name)")
		picked = strings.TrimSpace(picked)
		if picked == "" {
			if len(stages) == 0 {
				fmt.Println("At least one stage required.")
				continue
			}
			break
		}

		stageName := picked
		idx := 0
		if _, err := fmt.Sscanf(picked, "%d", &idx); err == nil && idx >= 1 && idx <= len(allStages) {
			stageName = allStages[idx-1]
		}

		// Agent type
		agentType := promptChoice(r, "Agent type", []string{"single", "consilium", "bash", "none"})
		s := stage{Name: stageName, AgentType: agentType}

		// Role(s)
		switch agentType {
		case "consilium":
			fmt.Printf("Available roles: %s\n", strings.Join(allRoles, ", "))
			rolesStr := prompt(r, "Roles (comma-separated)")
			for _, role := range strings.Split(rolesStr, ",") {
				role = strings.TrimSpace(role)
				if role != "" {
					s.Roles = append(s.Roles, role)
				}
			}
		case "single":
			fmt.Printf("Available roles: %s\n", strings.Join(allRoles, ", "))
			s.Role = prompt(r, "Role (or Enter for 'general-purpose')")
			if s.Role == "" {
				s.Role = "general-purpose"
			}
		}

		stages = append(stages, s)
		fmt.Printf("  ✓ Added: %s (%s)\n", stageName, agentType)

		more := promptChoice(r, "Add another stage?", []string{"y", "n"})
		if more == "n" {
			break
		}
	}

	if len(stages) == 0 {
		return fmt.Errorf("at least one stage required")
	}

	// Step 2: Auto-generate transitions
	generateTransitions(stages)

	fmt.Printf("\n--- Auto-generated transitions ---\n")
	for _, s := range stages {
		if len(s.Transitions) > 0 {
			fmt.Printf("  %s → %s\n", s.Name, strings.Join(s.Transitions, ", "))
		}
	}
	fmt.Println("(edit the profile file to customize transitions)")

	// Step 3: Meta
	keywords := prompt(r, "\nKeywords (comma-separated)")
	description := prompt(r, "Description")

	content := renderProfile(name, keywords, description, stages)

	dir := profilesDirIn(baseDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating profiles dir: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0600); err != nil {
		return fmt.Errorf("writing profile: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming profile: %w", err)
	}

	fmt.Printf("\nProfile '%s' created.\n", name)
	fmt.Printf("  → %s\n", path)
	return nil
}

// generateTransitions builds transitions heuristic:
// - Linear chain: each stage → next stage
// - Validation/Smoke Test → rollback to Executing, Fix, or Research
// - Report → Done (if both exist)
func generateTransitions(stages []stage) {
	nameSet := make(map[string]bool)
	for _, s := range stages {
		nameSet[s.Name] = true
	}

	for i := range stages {
		// Forward: current → next
		if i+1 < len(stages) {
			stages[i].Transitions = append(stages[i].Transitions, stages[i+1].Name)
		}

		// Rollback heuristics
		switch stages[i].Name {
		case "Validation", "Smoke Test", "Verify":
			for _, target := range []string{"Executing", "Fix", "Research"} {
				if nameSet[target] && target != stages[i].Name {
					stages[i].Transitions = append(stages[i].Transitions, target)
				}
			}
		case "Re-run":
			for _, target := range []string{"Fix", "Run"} {
				if nameSet[target] {
					stages[i].Transitions = append(stages[i].Transitions, target)
				}
			}
		case "Report":
			if nameSet["Done"] {
				// Already added via linear chain if Done is next,
				// but add explicitly if not adjacent
				hasDone := false
				for _, t := range stages[i].Transitions {
					if t == "Done" {
						hasDone = true
						break
					}
				}
				if !hasDone {
					stages[i].Transitions = append(stages[i].Transitions, "Done")
				}
			}
		}
	}
}

type stage struct {
	Name        string
	AgentType   string
	Role        string   // for single
	Roles       []string // for consilium
	Transitions []string
}

func renderProfile(name, keywords, description string, stages []stage) string {
	title := strings.ReplaceAll(name, "-", " ")
	title = strings.Title(title)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Profile: %s\n\n", title))

	b.WriteString("## Meta\n")
	b.WriteString(fmt.Sprintf("- **Keywords:** %s\n", keywords))
	b.WriteString(fmt.Sprintf("- **Description:** %s\n", description))

	b.WriteString("\n## Workflow (STRICT)\n\n")
	b.WriteString("### Stages\n")
	for i, s := range stages {
		desc := stageDescription(s)
		b.WriteString(fmt.Sprintf("%d. **%s** — %s\n", i+1, s.Name, desc))
	}

	b.WriteString("\n### Allowed transitions\n```\n")
	for _, s := range stages {
		for _, t := range s.Transitions {
			b.WriteString(fmt.Sprintf("%-15s -> %s\n", s.Name, t))
		}
	}
	b.WriteString("```\n")

	b.WriteString("\n### Agents per stage\n\n")
	b.WriteString("| Stage | Agents | Model |\n")
	b.WriteString("|-------|--------|-------|\n")
	for _, s := range stages {
		agents, model := stageAgentInfo(s)
		b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", s.Name, agents, model))
	}

	for _, s := range stages {
		if s.AgentType == "consilium" && len(s.Roles) > 0 {
			b.WriteString(fmt.Sprintf("\n### %s — Agent consilium\n\n", s.Name))
			b.WriteString("| Role | Responsibility |\n")
			b.WriteString("|------|----------------|\n")
			for _, role := range s.Roles {
				b.WriteString(fmt.Sprintf("| `%s` | |\n", role))
			}
		}
	}

	return b.String()
}

func stageDescription(s stage) string {
	switch s.AgentType {
	case "consilium":
		return "consilium analyzes task"
	case "bash":
		return "bash execution"
	case "single":
		return s.Role
	case "none":
		return "terminal stage"
	default:
		return s.AgentType
	}
}

func stageAgentInfo(s stage) (string, string) {
	switch s.AgentType {
	case "consilium":
		return "CONSILIUM (see below)", "high"
	case "bash":
		return "Bash", "medium"
	case "single":
		return s.Role, "high"
	case "none":
		return "—", "—"
	default:
		return s.AgentType, "high"
	}
}

func prompt(r *bufio.Reader, label string) string {
	fmt.Printf("%s: ", label)
	input, _ := r.ReadString('\n')
	return strings.TrimSpace(input)
}

func promptChoice(r *bufio.Reader, label string, options []string) string {
	for {
		fmt.Printf("%s (%s): ", label, strings.Join(options, "/"))
		input, _ := r.ReadString('\n')
		input = strings.TrimSpace(input)
		for _, opt := range options {
			if input == opt {
				return input
			}
		}
		fmt.Printf("Invalid choice. Options: %s\n", strings.Join(options, ", "))
	}
}

func hasMarkdownHeading(content, heading string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(strings.TrimSuffix(line, "\r")) == heading {
			return true
		}
	}
	return false
}

func extractBuiltinMetaBlock(content string) (string, error) {
	lines := strings.SplitAfter(content, "\n")
	start := -1
	end := len(lines)

	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if start == -1 {
			if trimmed == "## Meta" {
				start = i
			}
			continue
		}
		if strings.HasPrefix(trimmed, "## ") && trimmed != "## Meta" {
			end = i
			break
		}
	}
	if start == -1 {
		return "", fmt.Errorf("builtin profile is missing ## Meta")
	}
	return strings.TrimRight(strings.Join(lines[start:end], ""), "\r\n"), nil
}

func insertMetaAfterTitle(content, metaBlock string) (string, bool) {
	lines := strings.SplitAfter(content, "\n")
	if len(lines) == 0 {
		return "", false
	}

	titleEnd := -1
	offset := 0
	for _, line := range lines {
		offset += len(line)
		if strings.HasPrefix(strings.TrimSpace(strings.TrimSuffix(line, "\r")), "# ") {
			titleEnd = offset
			break
		}
	}
	if titleEnd == -1 {
		return "", false
	}

	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
	}
	metaBlock = strings.ReplaceAll(metaBlock, "\n", newline)
	rest := strings.TrimLeft(content[titleEnd:], "\r\n")
	repaired := content[:titleEnd] + newline + metaBlock
	if rest == "" {
		return repaired + newline, true
	}
	return repaired + newline + newline + rest, true
}
