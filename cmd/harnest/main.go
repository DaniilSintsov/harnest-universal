package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	agents_pkg "github.com/daniilsintsov/harnest-universal/internal/agents"
	"github.com/daniilsintsov/harnest-universal/internal/config"
	"github.com/daniilsintsov/harnest-universal/internal/converter"
	"github.com/daniilsintsov/harnest-universal/internal/detector"
	"github.com/daniilsintsov/harnest-universal/internal/doctor"
	"github.com/daniilsintsov/harnest-universal/internal/drift"
	"github.com/daniilsintsov/harnest-universal/internal/harness"
	"github.com/daniilsintsov/harnest-universal/internal/install"
	learn_pkg "github.com/daniilsintsov/harnest-universal/internal/learn"
	"github.com/daniilsintsov/harnest-universal/internal/mapping"
	"github.com/daniilsintsov/harnest-universal/internal/profile"
	"github.com/daniilsintsov/harnest-universal/internal/verify"
	"github.com/daniilsintsov/harnest-universal/internal/wizard"
	harnestYaml "github.com/daniilsintsov/harnest-universal/internal/yaml"
	goyaml "gopkg.in/yaml.v3"
)

var version = "0.12.0-universal.3"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}
	if len(os.Args) > 2 && (hasFlag("--help") || hasFlag("-h")) {
		printUsage()
		return
	}

	switch os.Args[1] {
	case "install":
		runInstall()
	case "init":
		runInit()
	case "detect":
		runDetect()
	case "profiles":
		runProfiles()
	case "agents":
		runAgents()
	case "drift":
		runDrift()
	case "generate":
		runGenerate()
	case "migrate":
		runMigrate()
	case "doctor":
		runDoctor()
	case "verify":
		runVerify()
	case "learn":
		runLearn()
	case "export":
		runExport()
	case "convert":
		runConvert()
	case "local":
		runLocal()
	case "config":
		runConfig()
	case "version", "--version", "-v":
		fmt.Printf("harnest v%s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

// --- install ---

func runInstall() {
	harnessName := parseFlag("--harness", "")
	targets := resolveInstallTargets(harnessName)

	for _, target := range targets {
		fmt.Printf("Installing Harnest framework for %s...\n", target)
		if err := install.InstallAll(target); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		globalDir, _ := harness.GlobalDir(target)
		configPath, _ := harness.GlobalConfigPath(target)
		fmt.Printf("  - %d workflow profiles → %s/profiles/\n", len(profile.BuiltinNames()), globalDir)
		fmt.Printf("  - global config        → %s\n", configPath)
	}

	fmt.Println("\nDone.")
	fmt.Println("\nNext: cd <project> && harnest init")
}

func resolveInstallTargets(requested string) []string {
	if requested != "" {
		return []string{requested}
	}
	return []string{"claude-code", "codex"}
}

// --- detect ---

func runDetect() {
	dir := parseDirArg(2)
	stacks := detector.Detect(dir)
	if len(stacks) == 0 {
		fmt.Println("No recognized stack detected.")
		return
	}
	fmt.Println("Detected stack:")
	for _, s := range stacks {
		fmt.Printf("  - %s (%s) [%s]\n", s.Name, s.Lang, s.Path)
	}
}

// --- init ---

func runInit() {
	dir := parseDirArg(2)
	harnessName := parseFlag("--harness", "")
	if harnestYaml.Exists(dir) {
		fmt.Fprintln(os.Stderr, "error: harnest.yaml already exists; use 'harnest generate'")
		os.Exit(1)
	}

	stacks := detector.Detect(dir)
	if len(stacks) == 0 {
		fmt.Println("No recognized stack detected. Creating minimal config.")
	} else {
		fmt.Println("Detected stack:")
		for _, s := range stacks {
			fmt.Printf("  - %s (%s) [%s]\n", s.Name, s.Lang, s.Path)
		}
	}

	targets := resolveInstallTargets(harnessName)

	discovered := agents_pkg.DiscoverPortable(dir)
	resolutionTarget := "codex"
	if len(targets) == 1 {
		resolutionTarget = targets[0]
		discovered = agents_pkg.DiscoverForTarget(dir, resolutionTarget)
	}
	var agentsCfg mapping.AgentConfig
	if hasFlag("--non-interactive") {
		agentsCfg = mapping.Resolve(stacks, discovered, resolutionTarget)
	} else {
		var err error
		agentsCfg, err = wizard.Run(
			os.Stdin,
			os.Stdout,
			mapping.ResolveStructure(stacks),
			mapping.GetSuggestions(stacks, discovered, resolutionTarget),
			discovered,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: interactive onboarding: %v\n", err)
			os.Exit(1)
		}
	}
	cfg := newProjectConfig(dir, stacks, agentsCfg, targets)
	if err := harnestYaml.Save(dir, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error saving harnest.yaml: %v\n", err)
		os.Exit(1)
	}
	files, err := harnestYaml.Generate(dir, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generating config: %v\n", err)
		os.Exit(1)
	}
	if _, err := harnestYaml.UpdateLocalExclude(dir, files); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not update local Git excludes: %v\n", err)
	}

	fmt.Println("\nGenerated:")
	fmt.Println("  " + filepath.Join(dir, "harnest.yaml"))
	for _, file := range files {
		fmt.Println("  " + file)
	}
	fmt.Printf("  Assigned consilium roles: %d\n", len(cfg.Agents.Consilium))
	fmt.Printf("  Assigned exec agents: %d\n", len(cfg.Agents.Executing))
}

func newProjectConfig(dir string, stacks []detector.Stack, agentsCfg mapping.AgentConfig, targets []string) *harnestYaml.HarnestConfig {
	cfg := &harnestYaml.HarnestConfig{
		Version:   harnestYaml.CurrentVersion,
		Project:   harnestYaml.ProjectInfo{Name: filepath.Base(dir)},
		Harnesses: append([]string(nil), targets...),
		Agents: harnestYaml.AgentsBlock{
			Consilium: map[string]string{},
			Models:    map[string]string{},
		},
		Context: harnestYaml.ContextBlock{Architecture: harnestYaml.ArchitectureBlock{
			Index: "docs/architecture/INDEX.md",
			State: "docs/architecture/.context-state.json",
		}},
		Rules:    harnestYaml.ResourceBlock{Root: ".harnest/rules", Index: ".harnest/rules/INDEX.yaml"},
		Skills:   harnestYaml.ResourceBlock{Root: ".agents/skills"},
		Checks:   harnestYaml.ResourceBlock{Root: ".harnest/checks"},
		Workflow: harnestYaml.WorkflowBlock{DefaultProfile: "business-feature", RoleSelection: "auto", RequireAvailableRoles: true, VerifyChanged: true},
		Settings: harnestYaml.SettingsBlock{LocalDefault: true, Language: "ru"},
	}
	for _, stack := range stacks {
		cfg.Stacks = append(cfg.Stacks, harnestYaml.StackEntry{Name: stack.Name, Lang: stack.Lang, Category: stack.Category, Path: stack.Path})
	}
	for _, role := range agentsCfg.Consilium {
		if role.Agent != "" {
			cfg.Agents.Consilium[role.Role] = role.Agent
		}
	}
	for _, agent := range agentsCfg.Exec {
		if agent.Agent != "" {
			cfg.Agents.Executing = append(cfg.Agents.Executing, harnestYaml.ExecEntry{Agent: agent.Agent, Scope: agent.Scope})
		}
	}
	for role, tier := range agentsCfg.Models {
		cfg.Agents.Models[role] = tier
	}
	return cfg
}

// --- profiles ---

func runProfiles() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: harnest profiles <list|add|edit|remove|sync> [name] [--harness <target>]")
		os.Exit(1)
	}
	if os.Args[2] == "sync" {
		runProfilesSync()
		return
	}
	baseDir, err := resolveProfilesBaseDir(parseFlag("--harness", ""))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	switch os.Args[2] {
	case "list":
		profiles, err := profile.ListIn(baseDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if len(profiles) == 0 {
			fmt.Println("No profiles installed. Run: harnest install")
			return
		}
		fmt.Println("Installed profiles:")
		for _, p := range profiles {
			marker := ""
			if profile.IsBuiltin(p) {
				marker = " (builtin)"
			}
			fmt.Printf("  - %s%s\n", p, marker)
		}

	case "add":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "usage: harnest profiles add <name>")
			os.Exit(1)
		}
		name := os.Args[3]
		reader := bufio.NewReader(os.Stdin)
		if err := profile.CreateIn(name, baseDir, reader); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

	case "edit":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "usage: harnest profiles edit <name>")
			os.Exit(1)
		}
		name := os.Args[3]
		if err := profile.EditIn(name, baseDir); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

	case "remove":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "usage: harnest profiles remove <name>")
			os.Exit(1)
		}
		name := os.Args[3]
		err = profile.RemoveIn(name, baseDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Profile '%s' removed.\n", name)

	default:
		fmt.Fprintf(os.Stderr, "unknown profiles subcommand: %s\n", os.Args[2])
		os.Exit(1)
	}
}

func runProfilesSync() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: harnest profiles sync <name> --from <claude-code|codex>")
		os.Exit(1)
	}
	from := parseFlag("--from", "")
	to := ""
	switch from {
	case "claude-code":
		to = "codex"
	case "codex":
		to = "claude-code"
	default:
		fmt.Fprintln(os.Stderr, "error: --from must be claude-code or codex")
		os.Exit(1)
	}
	fromDir, err := harness.GlobalDir(from)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	toDir, err := harness.GlobalDir(to)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	destination, backup, err := profile.SyncIn(os.Args[3], fromDir, from, toDir, to)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Profile '%s' synced %s → %s.\n  → %s\n", os.Args[3], from, to, destination)
	if backup != "" {
		fmt.Printf("  backup: %s\n", backup)
	}
}

func resolveProfilesBaseDir(target string) (string, error) {
	if target == "" {
		installed := harness.Installed("claude-code", "codex")
		switch len(installed) {
		case 0:
			return "", fmt.Errorf("Claude Code or Codex installation not found; use --harness explicitly")
		case 1:
			target = installed[0]
		default:
			return "", fmt.Errorf("multiple profile targets installed (%s); use --harness explicitly", strings.Join(installed, ", "))
		}
	}
	return harness.GlobalDir(target)
}

// --- agents ---

func runAgents() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: harnest agents <list|set|set-model> [role] [agent|tier]")
		os.Exit(1)
	}

	switch os.Args[2] {
	case "list":
		dir := parseDirArg(3)
		if harnestYaml.Exists(dir) {
			cfg, err := harnestYaml.Load(dir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			project, err := harnestYaml.BuildIR(dir, cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Project agent config:")
			printAgentConfig(project.Agents)
			return
		}
		cfg, err := config.ReadProject(dir)
		if err != nil {
			// No project config — show what would be generated
			fmt.Println("No project config found. Showing suggestions from detection:")
			stacks := detector.Detect(dir)
			disc := agents_pkg.Discover(dir)
			resolved := mapping.Resolve(stacks, disc, "claude-code")
			printAgentConfig(resolved)
			return
		}
		fmt.Println("Project agent config:")
		fmt.Println("\nConsilium:")
		for _, c := range cfg.Consilium {
			tier := ""
			if cfg.Models != nil {
				if t, ok := cfg.Models[c.Role]; ok {
					tier = fmt.Sprintf(" [%s]", t)
				}
			}
			fmt.Printf("  %-15s → %s%s\n", c.Role, c.Agent, tier)
		}
		fmt.Println("\nExecuting:")
		for _, e := range cfg.Exec {
			fmt.Printf("  %-40s → %s\n", e.Scope, e.Agent)
		}

	case "set":
		if len(os.Args) < 5 {
			fmt.Fprintln(os.Stderr, "usage: harnest agents set <role> <agent>")
			os.Exit(1)
		}
		role := os.Args[3]
		agent := os.Args[4]
		dir, _ := os.Getwd()
		// Optional --dir flag
		if d := parseFlag("--dir", ""); d != "" {
			dir = d
		}
		if harnestYaml.Exists(dir) {
			cfg, err := harnestYaml.Load(dir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			cfg, err = harnestYaml.Migrate(cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			if cfg.Agents.Consilium == nil {
				cfg.Agents.Consilium = map[string]string{}
			}
			cfg.Agents.Consilium[role] = agent
			if err := saveAndGenerate(dir, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Set %s → %s\n", role, agent)
			return
		}
		err := config.SetAgent(dir, role, agent)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Set %s → %s\n", role, agent)

	case "set-model":
		if len(os.Args) < 5 {
			fmt.Fprintln(os.Stderr, "usage: harnest agents set-model <role> <tier>")
			fmt.Fprintln(os.Stderr, "  tier: high, medium, low")
			os.Exit(1)
		}
		role := os.Args[3]
		tier := os.Args[4]
		dir, _ := os.Getwd()
		if d := parseFlag("--dir", ""); d != "" {
			dir = d
		}
		if harnestYaml.Exists(dir) {
			if tier != "high" && tier != "medium" && tier != "low" {
				fmt.Fprintf(os.Stderr, "error: invalid tier %q\n", tier)
				os.Exit(1)
			}
			cfg, err := harnestYaml.Load(dir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			cfg, err = harnestYaml.Migrate(cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			if cfg.Agents.Models == nil {
				cfg.Agents.Models = map[string]string{}
			}
			cfg.Agents.Models[role] = tier
			if err := saveAndGenerate(dir, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Set model for %s → %s\n", role, tier)
			return
		}
		err := config.SetModel(dir, role, tier)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Set model for %s → %s\n", role, tier)

	default:
		fmt.Fprintf(os.Stderr, "unknown agents subcommand: %s\n", os.Args[2])
		os.Exit(1)
	}
}

func saveAndGenerate(dir string, cfg *harnestYaml.HarnestConfig) error {
	if err := harnestYaml.Save(dir, cfg); err != nil {
		return err
	}
	files, err := harnestYaml.Generate(dir, cfg)
	if err != nil {
		return err
	}
	if cfg.Settings.LocalDefault {
		_, err := harnestYaml.UpdateLocalExclude(dir, files)
		return err
	}
	return nil
}

func printAgentConfig(agents mapping.AgentConfig) {
	fmt.Println("\nConsilium:")
	for _, c := range agents.Consilium {
		fmt.Printf("  %-15s → %s\n", c.Role, c.Agent)
	}
	fmt.Println("\nExecuting:")
	for _, e := range agents.Exec {
		fmt.Printf("  %-40s → %s\n", e.Scope, e.Agent)
	}
}

// --- convert ---

func runConvert() {
	from := parseFlag("--from", "")
	to := parseFlag("--to", "")
	dir := parseDirArg(2)

	if from == "" || to == "" {
		fmt.Fprintln(os.Stderr, "usage: harnest convert --from claude-code --to <harness> [dir]")
		os.Exit(1)
	}

	outPath, err := converter.Convert(dir, from, to)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Converted %s → %s: %s\n", from, to, outPath)
}

// --- helpers ---

func parseDirArg(startIdx int) string {
	dir, _ := os.Getwd()
	for i := startIdx; i < len(os.Args); i++ {
		arg := os.Args[i]
		if strings.HasPrefix(arg, "-") {
			// skip flag + its value (unless it's a boolean flag)
			if isBooleanFlag(arg) {
				continue
			}
			i++
			continue
		}
		// Check if it's a subcommand keyword, skip those
		if isSubcommand(arg) {
			continue
		}
		dir = arg
		break
	}
	return dir
}

func isBooleanFlag(flag string) bool {
	switch flag {
	case "--non-interactive", "--changed", "--dry-run", "--json", "--ci", "--fix":
		return true
	default:
		return false
	}
}

func parseFlag(flag, defaultVal string) string {
	for i, arg := range os.Args {
		if arg == flag && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return defaultVal
}

func hasFlag(flag string) bool {
	for _, arg := range os.Args {
		if arg == flag {
			return true
		}
	}
	return false
}

func repeatedFlag(flag string) []string {
	var values []string
	for i, arg := range os.Args {
		if arg == flag && i+1 < len(os.Args) {
			values = append(values, os.Args[i+1])
		}
	}
	return values
}

func isSubcommand(s string) bool {
	subs := []string{"list", "add", "edit", "remove", "set", "set-model", "unset", "show", "diff"}
	for _, sub := range subs {
		if s == sub {
			return true
		}
	}
	return false
}

// --- drift ---

func runDrift() {
	dir := "."
	if len(os.Args) > 2 && !strings.HasPrefix(os.Args[2], "-") {
		dir = os.Args[2]
	}

	jsonOutput := hasFlag("--json") || hasFlag("--ci")
	ciMode := hasFlag("--ci")

	result, err := drift.Check(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	if jsonOutput {
		data, _ := drift.FormatJSON(result)
		fmt.Println(string(data))
	} else {
		fmt.Print(drift.FormatTerminal(result))
	}

	// --fix: auto-resolve fixable drift items before applying CI exit codes.
	if hasFlag("--fix") {
		fixResult, err := drift.Fix(dir, result)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\nFixed %d issue(s).\n", len(fixResult.Fixed))
		if len(fixResult.Skipped) > 0 {
			fmt.Printf("Skipped %d issue(s) (require manual decision).\n", len(fixResult.Skipped))
		}
		for _, fixErr := range fixResult.Errors {
			fmt.Fprintf(os.Stderr, "fix error: %v\n", fixErr)
		}
	}

	if ciMode && len(result.Items) > 0 {
		// Determine exit code based on --fail-on level
		failOn := parseFlag("--fail-on", "error")
		for _, item := range result.Items {
			if string(item.Severity) == failOn || (failOn == "warning" && item.Severity == drift.SeverityError) {
				os.Exit(1)
			}
		}
	}
}

// --- generate ---

func runGenerate() {
	dir := "."
	if len(os.Args) > 2 && !strings.HasPrefix(os.Args[2], "-") {
		dir = os.Args[2]
	}

	if !harnestYaml.Exists(dir) {
		fmt.Fprintln(os.Stderr, "error: no harnest.yaml found")
		fmt.Fprintln(os.Stderr, "Run 'harnest init' to create one, or 'harnest export' to generate from existing config.")
		os.Exit(1)
	}

	cfg, err := harnestYaml.Load(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if hasFlag("--dry-run") {
		preview, err := harnestYaml.GenerateDryRun(dir, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Would generate adapter outputs:")
		names := make([]string, 0, len(preview.Adapters))
		for name := range preview.Adapters {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			content := preview.Adapters[name]
			lines := strings.Count(content, "\n")
			fmt.Printf("  %s (%d lines)\n", name, lines)
		}
		fmt.Println("\nWould affect project files:")
		for _, path := range preview.Files {
			fmt.Printf("  %s\n", path)
		}
		fmt.Println("No files written. Remove --dry-run to generate.")
		return
	}

	files, err := harnestYaml.Generate(dir, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Generated:")
	for _, f := range files {
		fmt.Printf("  %s\n", f)
	}

	effective, err := harnestYaml.Migrate(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	changed := false
	if effective.Settings.LocalDefault {
		changed, err = harnestYaml.UpdateLocalExclude(dir, files)
	} else {
		changed, err = harnestYaml.UpdateGitignore(dir, files)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not update ignore rules: %v\n", err)
	} else if changed && effective.Settings.LocalDefault {
		fmt.Println("\nUpdated local Git exclude rules")
	} else if changed {
		fmt.Println("\nUpdated .gitignore")
	}
}

// --- migrate ---

func runMigrate() {
	dir := parseDirArg(2)
	changed, backup, err := harnestYaml.MigrateFile(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if !changed {
		fmt.Printf("harnest.yaml already uses schema v%d\n", harnestYaml.CurrentVersion)
		return
	}
	fmt.Printf("Migrated harnest.yaml to schema v%d\nBackup: %s\n", harnestYaml.CurrentVersion, backup)
}

// --- doctor ---

func runDoctor() {
	dir := parseDirArg(2)
	report, err := doctor.Check(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	targets := make([]string, 0, len(report.Capabilities))
	for target := range report.Capabilities {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		caps := report.Capabilities[target]
		fmt.Printf("%s: instructions=%s rules=%s skills=%s agents=%s pre-hook=%s post-hook=%s permissions=%s verify=%s\n",
			target, caps.Instructions, caps.ScopedRules, caps.Skills, caps.Agents, caps.PreToolHook, caps.PostToolHook, caps.Permissions, caps.Verification)
	}
	for _, item := range report.Items {
		fmt.Printf("[%s] %s\n", item.Level, item.Message)
	}
	if !report.Healthy() {
		os.Exit(1)
	}
	fmt.Println("Harnest doctor: healthy")
}

// --- verify ---

func runVerify() {
	if !hasFlag("--changed") {
		fmt.Fprintln(os.Stderr, "usage: harnest verify --changed [dir] [--base <ref>] [--allow <rule-id>]")
		os.Exit(1)
	}
	dir := parseDirArg(2)
	allowed := map[string]bool{}
	for _, id := range repeatedFlag("--allow") {
		allowed[id] = true
	}
	result, err := verify.Run(dir, allowed, parseFlag("--base", ""))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Changed files: %d\n", len(result.Changed))
	for _, id := range result.Checks {
		fmt.Printf("[pass] check %s\n", id)
	}
	for _, verificationErr := range result.Errors {
		fmt.Fprintf(os.Stderr, "[fail] %v\n", verificationErr)
	}
	if len(result.Errors) > 0 {
		os.Exit(1)
	}
	fmt.Println("Harnest verify: passed")
}

// --- learn ---

func runLearn() {
	dir := parseDirArg(2)
	id := parseFlag("--id", "")
	statement := parseFlag("--statement", "")
	if id == "" || statement == "" {
		fmt.Fprintln(os.Stderr, "usage: harnest learn [dir] --id <candidate-id> --statement <rule>")
		os.Exit(1)
	}
	path, err := learn_pkg.Propose(dir, id, statement)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Candidate created: %s\nReview and move it from rules/candidates/ to rules/ to activate it.\n", path)
}

// --- export ---

func runExport() {
	dir := "."
	if len(os.Args) > 2 && !strings.HasPrefix(os.Args[2], "-") {
		dir = os.Args[2]
	}

	if harnestYaml.Exists(dir) {
		fmt.Fprintln(os.Stderr, "error: harnest.yaml already exists in this directory")
		fmt.Fprintln(os.Stderr, "Delete it first if you want to re-export.")
		os.Exit(1)
	}

	// Read existing project config
	projectCfg, err := config.ReadProject(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Detect stacks
	stacks := detector.Detect(dir)

	// Build harnest.yaml config
	cfg := &harnestYaml.HarnestConfig{
		Version:   harnestYaml.CurrentVersion,
		Harnesses: []string{"claude-code"}, // default, inferred from found config file
		Agents: harnestYaml.AgentsBlock{
			Consilium: make(map[string]string),
			Models:    make(map[string]string),
		},
		Settings: harnestYaml.SettingsBlock{
			AutoDetect:    true,
			StackStrategy: "merge",
			LocalDefault:  true,
			Language:      "ru",
		},
		Context: harnestYaml.ContextBlock{Architecture: harnestYaml.ArchitectureBlock{
			Index: "docs/architecture/INDEX.md",
			State: "docs/architecture/.context-state.json",
		}},
		Rules:    harnestYaml.ResourceBlock{Root: ".harnest/rules", Index: ".harnest/rules/INDEX.yaml"},
		Skills:   harnestYaml.ResourceBlock{Root: ".agents/skills"},
		Checks:   harnestYaml.ResourceBlock{Root: ".harnest/checks"},
		Workflow: harnestYaml.WorkflowBlock{DefaultProfile: "business-feature", RequireAvailableRoles: true, VerifyChanged: true},
	}

	// Convert stacks
	for _, s := range stacks {
		cfg.Stacks = append(cfg.Stacks, harnestYaml.StackEntry{
			Name:     s.Name,
			Lang:     s.Lang,
			Category: s.Category,
			Path:     s.Path,
		})
	}

	// Convert consilium
	for _, c := range projectCfg.Consilium {
		cfg.Agents.Consilium[c.Role] = c.Agent
	}

	// Convert exec
	for _, e := range projectCfg.Exec {
		cfg.Agents.Executing = append(cfg.Agents.Executing, harnestYaml.ExecEntry{
			Agent: e.Agent,
			Scope: e.Scope,
		})
	}

	// Convert models
	for role, tier := range projectCfg.Models {
		cfg.Agents.Models[role] = tier
	}

	if err := harnestYaml.Save(dir, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Exported to harnest.yaml")
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Review harnest.yaml")
	fmt.Println("  2. Run 'harnest generate' to verify output")
	fmt.Println("  3. Harnest files stay local by default")
}

// --- local ---

func runLocal() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: harnest local <set|unset|show>")
		os.Exit(1)
	}

	dir, _ := os.Getwd()
	if d := parseFlag("--dir", ""); d != "" {
		dir = d
	}

	switch os.Args[2] {
	case "set":
		runLocalSet(dir)
	case "unset":
		runLocalUnset(dir)
	case "show":
		runLocalShow(dir)
	default:
		fmt.Fprintf(os.Stderr, "unknown local subcommand: %s\n", os.Args[2])
		fmt.Fprintln(os.Stderr, "usage: harnest local <set|unset|show>")
		os.Exit(1)
	}
}

// runLocalSet handles: harnest local set <key> <value>
//
// Supported key paths:
//   - agents.consilium.<role>   — override a consilium agent
//   - agents.models.<role>      — override a model tier
//   - harnesses                 — add a harness to the list
func runLocalSet(dir string) {
	if len(os.Args) < 5 {
		fmt.Fprintln(os.Stderr, "usage: harnest local set <key> <value>")
		fmt.Fprintln(os.Stderr, "  keys: agents.consilium.<role>, agents.models.<role>, harnesses")
		os.Exit(1)
	}

	key := os.Args[3]
	value := os.Args[4]

	local, err := loadOrNewLocal(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	parts := strings.SplitN(key, ".", 3)

	switch {
	case len(parts) == 3 && parts[0] == "agents" && parts[1] == "consilium":
		role := parts[2]
		if local.Agents.Consilium == nil {
			local.Agents.Consilium = make(map[string]string)
		}
		local.Agents.Consilium[role] = value
		fmt.Printf("Set agents.consilium.%s = %s\n", role, value)

	case len(parts) == 3 && parts[0] == "agents" && parts[1] == "models":
		role := parts[2]
		if local.Agents.Models == nil {
			local.Agents.Models = make(map[string]string)
		}
		local.Agents.Models[role] = value
		fmt.Printf("Set agents.models.%s = %s\n", role, value)

	case key == "harnesses":
		for _, h := range local.Harnesses {
			if h == value {
				fmt.Printf("Harness %q already present in local config.\n", value)
				return
			}
		}
		local.Harnesses = append(local.Harnesses, value)
		fmt.Printf("Added harness: %s\n", value)

	default:
		fmt.Fprintf(os.Stderr, "unknown key %q\n", key)
		fmt.Fprintln(os.Stderr, "  supported: agents.consilium.<role>, agents.models.<role>, harnesses")
		os.Exit(1)
	}

	if err := harnestYaml.SaveLocal(dir, local); err != nil {
		fmt.Fprintf(os.Stderr, "error saving local config: %v\n", err)
		os.Exit(1)
	}
}

// runLocalUnset handles: harnest local unset <key>
func runLocalUnset(dir string) {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: harnest local unset <key>")
		os.Exit(1)
	}

	key := os.Args[3]

	local, err := loadOrNewLocal(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	parts := strings.SplitN(key, ".", 3)

	switch {
	case len(parts) == 3 && parts[0] == "agents" && parts[1] == "consilium":
		role := parts[2]
		delete(local.Agents.Consilium, role)
		if len(local.Agents.Consilium) == 0 {
			local.Agents.Consilium = nil
		}
		fmt.Printf("Unset agents.consilium.%s\n", role)

	case len(parts) == 3 && parts[0] == "agents" && parts[1] == "models":
		role := parts[2]
		delete(local.Agents.Models, role)
		if len(local.Agents.Models) == 0 {
			local.Agents.Models = nil
		}
		fmt.Printf("Unset agents.models.%s\n", role)

	case key == "harnesses":
		local.Harnesses = nil
		fmt.Println("Cleared harnesses override.")

	default:
		fmt.Fprintf(os.Stderr, "unknown key %q\n", key)
		os.Exit(1)
	}

	if err := harnestYaml.SaveLocal(dir, local); err != nil {
		fmt.Fprintf(os.Stderr, "error saving local config: %v\n", err)
		os.Exit(1)
	}
}

// runLocalShow handles: harnest local show
func runLocalShow(dir string) {
	if !harnestYaml.LocalExists(dir) {
		fmt.Println("No .harnest-local.yaml found. Nothing to show.")
		return
	}

	local, err := harnestYaml.LoadLocal(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if local == nil {
		fmt.Println("Empty local config.")
		return
	}

	data, err := goyaml.Marshal(local)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(string(data))
}

// loadOrNewLocal loads the existing local config or returns a blank one.
func loadOrNewLocal(dir string) (*harnestYaml.LocalConfig, error) {
	if harnestYaml.LocalExists(dir) {
		return harnestYaml.LoadLocal(dir)
	}
	return &harnestYaml.LocalConfig{}, nil
}

// --- config ---

func runConfig() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: harnest config <show|diff> [dir]")
		os.Exit(1)
	}

	switch os.Args[2] {
	case "show":
		runConfigShow()
	case "diff":
		runConfigDiff()
	default:
		fmt.Fprintf(os.Stderr, "unknown config subcommand: %s\n", os.Args[2])
		fmt.Fprintln(os.Stderr, "usage: harnest config <show|diff> [dir]")
		os.Exit(1)
	}
}

// runConfigShow prints the fully merged (team + local) effective configuration.
func runConfigShow() {
	dir := parseDirArg(3)

	if !harnestYaml.Exists(dir) {
		fmt.Fprintln(os.Stderr, "error: no harnest.yaml found")
		os.Exit(1)
	}

	team, err := harnestYaml.Load(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading harnest.yaml: %v\n", err)
		os.Exit(1)
	}

	var effective *harnestYaml.HarnestConfig
	if harnestYaml.LocalExists(dir) {
		local, err := harnestYaml.LoadLocal(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading .harnest-local.yaml: %v\n", err)
			os.Exit(1)
		}
		effective = harnestYaml.Merge(team, local)
		fmt.Println("# Effective config (team + local overrides)")
	} else {
		effective = team
		fmt.Println("# Effective config (team only — no local overrides)")
	}

	data, err := goyaml.Marshal(effective)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(string(data))
}

// runConfigDiff prints only the local overrides, showing what differs from the
// team config.
func runConfigDiff() {
	dir := parseDirArg(3)

	if !harnestYaml.LocalExists(dir) {
		fmt.Println("No .harnest-local.yaml found — no local overrides.")
		return
	}

	local, err := harnestYaml.LoadLocal(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading .harnest-local.yaml: %v\n", err)
		os.Exit(1)
	}
	if local == nil {
		fmt.Println("Empty local config — no overrides.")
		return
	}

	fmt.Println("# Local overrides (.harnest-local.yaml)")
	data, err := goyaml.Marshal(local)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(string(data))
}

func printUsage() {
	hlist := strings.Join(harness.Names(), "|")
	fmt.Printf(`harnest - AI coding assistant configurator

Usage:
  harnest install [--harness %s]
  harnest init [dir] [--harness %s] [--non-interactive]
  harnest detect [dir]
  harnest profiles list [--harness <target>]
  harnest profiles add <name> [--harness <target>]
  harnest profiles edit <name> [--harness <target>]
  harnest profiles remove <name> [--harness <target>]
  harnest profiles sync <name> --from claude-code|codex
  harnest agents list [dir]
  harnest agents set <role> <agent>
  harnest agents set-model <role> <tier>
  harnest drift [dir] [--json] [--ci] [--fail-on error|warning] [--fix]
  harnest generate [dir] [--dry-run]
  harnest migrate [dir]
  harnest doctor [dir]
  harnest verify --changed [dir] [--base <ref>] [--allow <rule-id>]
  harnest learn [dir] --id <candidate-id> --statement <rule>
  harnest export [dir]
  harnest convert --from claude-code --to <harness> [dir]
  harnest local set <key> <value>
  harnest local unset <key>
  harnest local show
  harnest config show [dir]
  harnest config diff [dir]
  harnest version

Commands:
  install    Install Harnest framework (profiles + global config) for a harness
  init       Detect stack and generate project config
  detect     Show detected stack without generating
  drift      Detect legacy schema v1 config drift
  generate   Generate config files from harnest.yaml
  migrate    Upgrade harnest.yaml to current schema with backup
  doctor     Check adapter capabilities and hard-rule enforcement
  verify     Enforce applicable rules against changed files
  learn      Create an inactive rule candidate for review
  export     Export existing config to harnest.yaml
  profiles   Manage workflow profiles (create, edit, list, remove, sync)
  agents     View/modify agent role mappings
  local      Manage personal config overrides (.harnest-local.yaml)
  config     View effective (merged) configuration
  convert    Convert a legacy Claude Code agent mapping to another assistant

Local key paths (harnest local set/unset):
  agents.consilium.<role>  Override consilium agent for a role
  agents.models.<role>     Override model tier for a role (high|medium|low)
  harnesses                Add a harness to the local list

Flags:
  --harness          Target harness (%s)
  --non-interactive  Accept suggested agents without prompts
`, hlist, hlist, hlist)
}
