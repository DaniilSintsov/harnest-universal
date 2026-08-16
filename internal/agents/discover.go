package agents

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/harness"
	"github.com/daniilsintsov/harnest-universal/internal/managedfile"
	"gopkg.in/yaml.v3"
)

// Discover scans all installed agents: project-local (priority), global, and plugins.
// projectDir may be empty — project scan is skipped.
// Returns agents in priority order: project-local first, then global, then plugins.
// Insertion order is preserved — project agents win dedup ties in MatchAgent.
func Discover(projectDir string) []string {
	seen := map[string]bool{}
	var result []string

	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}

	// 1. Project-local agents (priority — added first, win on dedup)
	if projectDir != "" {
		for _, name := range scanProjectAgents(projectDir) {
			add(name)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return result
	}

	// 2. Global agents from all registered harness locations
	for _, dir := range harness.AgentDirs() {
		scanFlat(filepath.Join(home, dir), "", []string{".md"}, add)
		for _, name := range scanCodexAgents(filepath.Join(home, dir), "") {
			add(name)
		}
	}

	// 3. All plugins: walk ~/.claude/plugins/cache/ for plugin.json
	for _, name := range scanPlugins(filepath.Join(home, ".claude", "plugins", "cache")) {
		add(name)
	}

	return result
}

// DiscoverPortable returns agents explicitly declared portable by the project.
func DiscoverPortable(projectDir string) []string {
	return scanWithFrontmatter(filepath.Join(projectDir, ".agents", "agents"), "")
}

const portableOwnershipMarker = "<!-- harnest-portable-agent:managed -->"
const portableCodexOwnershipMarker = "# harnest-portable-agent:managed"

type portableAgent struct {
	name         string
	description  string
	instructions string
	content      []byte
}

// MaterializePortable copies project-portable agents into only the selected
// adapters' callable agent directories. Existing user files are backed up
// before Harnest takes managed ownership.
func MaterializePortable(projectDir string, targets []string) ([]string, error) {
	sourceDir := filepath.Join(projectDir, ".agents", "agents")
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		entries = nil
	}

	seen := map[string]bool{}
	var portable []portableAgent
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || entry.Name() == "README.md" {
			continue
		}
		sourcePath := filepath.Join(sourceDir, entry.Name())
		name := parseAgentName(sourcePath)
		if name == "" {
			continue
		}
		if !safeCallableName(name) {
			return nil, fmt.Errorf("portable agent %s has unsafe callable name %q", sourcePath, name)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate portable agent callable name %q", name)
		}
		content, err := os.ReadFile(sourcePath)
		if err != nil {
			return nil, err
		}
		description, instructions := portableCodexFields(content)
		seen[name] = true
		portable = append(portable, portableAgent{
			name:         name,
			description:  description,
			instructions: instructions,
			content:      content,
		})
	}
	sort.Slice(portable, func(i, j int) bool { return portable[i].name < portable[j].name })

	var generated []string
	for _, target := range targets {
		agentDir, err := harness.AgentDir(target)
		if err != nil {
			return generated, err
		}
		if agentDir == "" {
			continue
		}
		extension := ".md"
		cleanupSuffixes := []string{".md"}
		if target == "codex" {
			extension = ".toml"
			cleanupSuffixes = []string{".toml", ".md"}
		}
		desired := make(map[string]bool, len(portable))
		for _, agent := range portable {
			outPath := filepath.Join(projectDir, agentDir, agent.name+extension)
			desired[filepath.Clean(outPath)] = true
			content := renderPortableMarkdown(agent.content)
			if target == "codex" {
				content = renderPortableCodex(agent)
			}
			if err := writePortableAgent(outPath, content); err != nil {
				return generated, fmt.Errorf("materializing portable agent %q for %s: %w", agent.name, target, err)
			}
			generated = append(generated, outPath)
		}
		if err := cleanupPortableAgents(filepath.Join(projectDir, agentDir), desired, cleanupSuffixes); err != nil {
			return generated, fmt.Errorf("cleaning stale portable agents for %s: %w", target, err)
		}
	}
	return generated, nil
}

func safeCallableName(name string) bool {
	return name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, "\\/\r\n")
}

func renderPortableMarkdown(source []byte) []byte {
	return []byte(strings.TrimRight(string(source), "\r\n") + "\n\n" + portableOwnershipMarker + "\n")
}

func renderPortableCodex(agent portableAgent) []byte {
	description := strings.TrimSpace(agent.description)
	if description == "" {
		description = fmt.Sprintf("Portable agent %s generated by Harnest.", agent.name)
	}
	instructions := agent.instructions
	if strings.TrimSpace(instructions) == "" {
		instructions = description
	}
	return []byte(fmt.Sprintf(
		"name = %s\ndescription = %s\ndeveloper_instructions = %s\n\n%s\n",
		tomlBasicString(agent.name),
		tomlBasicString(description),
		tomlBasicString(instructions),
		portableCodexOwnershipMarker,
	))
}

func portableCodexFields(source []byte) (description, instructions string) {
	content := strings.ReplaceAll(string(source), "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	instructions = content
	if !strings.HasPrefix(content, "---") {
		return "", instructions
	}
	rest := strings.TrimLeft(strings.TrimPrefix(content, "---"), "\n")
	endIdx := indexClosingDelim(rest)
	if endIdx == -1 || endIdx > 4096 {
		return "", instructions
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(rest[:endIdx]), &fm); err != nil {
		return "", instructions
	}
	body := rest[endIdx+4:]
	body = strings.TrimPrefix(body, "\n")
	return fm.Description, body
}

func tomlBasicString(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func writePortableAgent(path string, desired []byte) error {
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return managedfile.WriteAtomic(path, desired, 0644)
		}
		return err
	}
	if string(existing) == string(desired) {
		return nil
	}
	if hasPortableOwnership(existing) {
		return managedfile.WriteAtomic(path, desired, infoMode(path, 0644))
	}
	if strings.Contains(string(existing), "harnest-portable-agent:") {
		return fmt.Errorf("file has unknown portable-agent ownership marker: %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err := managedfile.WriteAtomic(path+".bak", existing, info.Mode().Perm()); err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}
	return managedfile.WriteAtomic(path, desired, info.Mode().Perm())
}

func cleanupPortableAgents(dir string, desired map[string]bool, suffixes []string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !hasSuffix(entry.Name(), suffixes) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if desired[filepath.Clean(path)] {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !hasPortableOwnership(content) {
			continue
		}
		backupPath := path + ".bak"
		backup, err := os.ReadFile(backupPath)
		if err == nil {
			info, statErr := os.Stat(backupPath)
			if statErr != nil {
				return statErr
			}
			if err := managedfile.WriteAtomic(path, backup, info.Mode().Perm()); err != nil {
				return err
			}
			if err := os.Remove(backupPath); err != nil {
				return err
			}
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		deletedBackup := fmt.Sprintf("%s.deleted-%x.bak", path, sum[:6])
		if err := managedfile.WriteAtomic(deletedBackup, content, info.Mode().Perm()); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func hasPortableOwnership(content []byte) bool {
	for _, line := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
		if line == portableOwnershipMarker || line == portableCodexOwnershipMarker {
			return true
		}
	}
	return false
}

func hasSuffix(name string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func infoMode(path string, fallback os.FileMode) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return fallback
}

// DiscoverForTarget prevents platform-specific agents leaking into another adapter.
func DiscoverForTarget(projectDir, target string) []string {
	seen := map[string]bool{}
	var result []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	for _, name := range DiscoverPortable(projectDir) {
		add(name)
	}
	dir, err := harness.AgentDir(target)
	if err != nil || dir == "" {
		return result
	}
	for _, name := range scanWithFrontmatter(filepath.Join(projectDir, dir), "") {
		add(name)
	}
	if target == "codex" {
		for _, name := range scanCodexAgents(filepath.Join(projectDir, dir), "") {
			add(name)
		}
	}
	home, err := os.UserHomeDir()
	if err == nil {
		scanFlat(filepath.Join(home, dir), "", []string{".md"}, add)
		if target == "codex" {
			for _, name := range scanCodexAgents(filepath.Join(home, dir), "") {
				add(name)
			}
		}
		if target == "claude-code" {
			for _, name := range scanPlugins(filepath.Join(home, ".claude", "plugins", "cache")) {
				add(name)
			}
		}
	}
	return result
}

// scanProjectAgents scans project-local agents across all registered harness dirs
// (e.g. .claude/agents/, .cursor/agents/, .windsurf/agents/), preserving harness-dir
// order and deduplicating. Reads YAML frontmatter: if "name" is present it is used,
// otherwise the filename. Single source of truth shared by Discover (which keeps this
// order so project agents win dedup ties) and DiscoverProject.
func scanProjectAgents(projectDir string) []string {
	seen := map[string]bool{}
	var result []string
	for _, dir := range harness.AgentDirs() {
		agentsDir := filepath.Join(projectDir, dir)
		for _, name := range scanWithFrontmatter(agentsDir, "") {
			if name != "" && !seen[name] {
				seen[name] = true
				result = append(result, name)
			}
		}
		for _, name := range scanCodexAgents(agentsDir, "") {
			if name != "" && !seen[name] {
				seen[name] = true
				result = append(result, name)
			}
		}
	}
	return result
}

// DiscoverProject returns project-local agents (see scanProjectAgents), sorted.
func DiscoverProject(projectDir string) []string {
	agents := scanProjectAgents(projectDir)
	sort.Strings(agents)
	return agents
}

// Search filters agents by substring match (case-insensitive).
func Search(agents []string, query string) []string {
	if query == "" {
		return agents
	}
	q := strings.ToLower(query)
	var results []string
	for _, a := range agents {
		if strings.Contains(strings.ToLower(a), q) {
			results = append(results, a)
		}
	}
	return results
}

// --- plugin scanning ---

type pluginJSON struct {
	Name   string   `json:"name"`
	Agents []string `json:"agents"`
}

// scanPlugins walks plugins cache dir, finds plugin.json files, extracts agent names.
// Agents are discovered from:
//  1. <plugin-version>/agents/*.md — auto-discovered by Claude Code (no plugin.json entry needed)
//  2. plugin.json "agents" field — explicit agent list (backward compat)
//
// Agent names are namespaced as "pluginName:agentName".
func scanPlugins(root string) []string {
	seen := map[string]bool{}
	var result []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() || filepath.Base(path) != "plugin.json" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var p pluginJSON
		if err := json.Unmarshal(data, &p); err != nil || p.Name == "" {
			return nil
		}

		// plugin.json is at <version>/.claude-plugin/plugin.json
		// version dir = two levels up from plugin.json
		versionDir := filepath.Dir(filepath.Dir(path))

		// 1. Scan agents/ directory (primary method — Claude Code auto-discovers these)
		agentsDir := filepath.Join(versionDir, "agents")
		scanFlat(agentsDir, p.Name+":", []string{".md"}, add)

		// 2. Also process explicit "agents" field for backward compatibility
		for _, agentPath := range p.Agents {
			base := filepath.Base(agentPath)
			name := strings.TrimSuffix(base, ".md")
			if name == "" || name == "README" {
				continue
			}
			// Verify file exists relative to plugin.json dir
			agentFile := filepath.Join(filepath.Dir(path), "..", agentPath)
			if _, err := os.Stat(agentFile); err != nil {
				continue
			}
			add(p.Name + ":" + name)
		}
		return nil
	})
	sort.Strings(result)
	return result
}

// --- flat scanning ---

// scanFlat reads files with allowed suffixes from dir, adds prefix+basename.
// Skips README and empty names.
func scanFlat(dir, prefix string, suffixes []string, add func(string)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		suffix := ""
		for _, candidate := range suffixes {
			if strings.HasSuffix(e.Name(), candidate) {
				suffix = candidate
				break
			}
		}
		if suffix == "" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), suffix)
		if name == "README" || name == "" {
			continue
		}
		add(prefix + name)
	}
}

// --- frontmatter-aware scanning ---

// frontmatter represents YAML frontmatter in agent .md files.
type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// scanWithFrontmatter reads *.md files from dir, parses YAML frontmatter.
// If frontmatter has "name" field → uses it. Otherwise falls back to filename.
// Only unreadable/empty/binary/oversized files are skipped.
// Agent names are prefixed with prefix (e.g. plugin name + ":").
func scanWithFrontmatter(dir, prefix string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var agents []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}

		base := strings.TrimSuffix(e.Name(), ".md")
		if base == "README" || base == "" {
			continue
		}

		filePath := filepath.Join(dir, e.Name())
		name := parseAgentName(filePath)
		if name == "" {
			continue
		}
		agents = append(agents, prefix+name)
	}
	sort.Strings(agents)
	return agents
}

// scanCodexAgents reads native Codex TOML agents. The name field is the
// callable identity; filename is only a fallback for malformed legacy files.
func scanCodexAgents(dir, prefix string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var agents []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		name := parseCodexAgentName(filepath.Join(dir, entry.Name()))
		if name != "" {
			agents = append(agents, prefix+name)
		}
	}
	sort.Strings(agents)
	return agents
}

func parseCodexAgentName(path string) string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 || info.Size() > maxAgentFileSize {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil || bytesContainsNull(data) {
		return ""
	}
	fallback := strings.TrimSuffix(filepath.Base(path), ".toml")
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "name") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "name"))
		if !strings.HasPrefix(rest, "=") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(rest, "="))
		if parsed, err := strconv.Unquote(value); err == nil && parsed != "" {
			return parsed
		}
		if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1]
		}
	}
	return fallback
}

// maxAgentFileSize limits how much of an agent .md file we read for frontmatter.
const maxAgentFileSize = 64 * 1024 // 64 KB

// parseAgentName reads a .md file, extracts "name" from YAML frontmatter.
// Frontmatter is YAML between --- delimiters at the start of the file.
// Falls back to filename (without .md) when:
//   - No frontmatter at all
//   - No closing "---"
//   - Frontmatter YAML is invalid
//   - Frontmatter has no "name" field
//
// Returns empty string only for unreadable files, empty files, binary files, or oversized files.
func parseAgentName(filePath string) string {
	// Stat first so oversized/empty files are rejected without buffering them in RAM.
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() || info.Size() == 0 || info.Size() > maxAgentFileSize {
		return ""
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}

	// Reject binary files (null bytes).
	if bytesContainsNull(data) {
		return ""
	}

	// Fallback to filename (used when frontmatter is absent or unparseable).
	base := filepath.Base(filePath)
	fallback := strings.TrimSuffix(base, ".md")
	if fallback == "README" || fallback == "" {
		return ""
	}

	content := string(data)
	// No frontmatter → fallback to filename.
	if !strings.HasPrefix(content, "---") {
		return fallback
	}

	// Strip opening "---" and following newline(s)
	content = strings.TrimPrefix(content, "---")
	content = strings.TrimLeft(content, "\r\n")

	// Find closing "---" on its own line (delimiter must be followed by EOL or EOF,
	// so a value containing "----" or "\n---text" does not truncate frontmatter early).
	endIdx := indexClosingDelim(content)
	if endIdx == -1 {
		return fallback
	}

	// Limit frontmatter size — anything beyond 4KB is not a real agent definition.
	fm := content[:endIdx]
	if len(fm) > 4096 {
		return fallback
	}

	var fmData frontmatter
	if err := yaml.Unmarshal([]byte(fm), &fmData); err != nil {
		return fallback
	}

	if fmData.Name != "" {
		return fmData.Name
	}

	return fallback
}

// indexClosingDelim returns the index of a "\n---" frontmatter terminator whose
// "---" stands on its own line (next byte is \n, \r, or EOF), or -1 if none.
func indexClosingDelim(content string) int {
	base := 0
	for {
		i := strings.Index(content[base:], "\n---")
		if i == -1 {
			return -1
		}
		abs := base + i
		after := abs + 4 // byte past "\n---"
		if after >= len(content) || content[after] == '\n' || content[after] == '\r' {
			return abs
		}
		base = after
	}
}

// bytesContainsNull checks if data contains a null byte (binary content).
func bytesContainsNull(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
