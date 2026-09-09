package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/checks"
	"github.com/daniilsintsov/harnest-universal/internal/ir"
	"github.com/daniilsintsov/harnest-universal/internal/rules"
)

const nativeHookStatus = "Harnest native hooks"

// HookArtifact is one optimistic, atomic native-config update.
type HookArtifact struct {
	Path    string
	Content []byte
	Before  []byte
	Mode    os.FileMode
	Exists  bool
	Remove  bool
}

// HookDiagnostic describes installed wiring without claiming host trust or execution.
type HookDiagnostic struct {
	Platform   string
	Path       string
	Installed  bool
	Executable string
	Events     []string
	Issue      string
}

// InspectHooks reads supported native config layers and verifies Harnest ownership.
func InspectHooks(dir string, project ir.Project) ([]HookDiagnostic, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	platforms := []string{"claude-code", "codex"}
	selected, err := rules.SelectHooks(project.PolicyRules, project.Hooks.Rules)
	if err != nil {
		return nil, err
	}
	wantPre, wantStop := hookEvents(selected)
	checksDigest, checksErr := selectedChecksDigest(root, project, selected)
	result := make([]HookDiagnostic, 0, len(platforms))
	for _, platform := range platforms {
		path := filepath.Join(root, ".codex", "hooks.json")
		if platform == "claude-code" {
			path = filepath.Join(claudeSettingsRoot(root), ".claude", "settings.local.json")
		}
		diagnostic := HookDiagnostic{Platform: platform, Path: path}
		if checksErr != nil {
			diagnostic.Issue = checksErr.Error()
		}
		data, readErr := os.ReadFile(path)
		if os.IsNotExist(readErr) {
			result = append(result, diagnostic)
			continue
		}
		if readErr != nil {
			return nil, readErr
		}
		var doc map[string]any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&doc); err != nil {
			diagnostic.Issue = err.Error()
			result = append(result, diagnostic)
			continue
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			diagnostic.Issue = "trailing JSON value"
			result = append(result, diagnostic)
			continue
		}
		hooks, _ := doc["hooks"].(map[string]any)
		counts := map[string]int{}
		for _, event := range []string{"PreToolUse", "Stop"} {
			entries, entryErr := objectSlice(hooks[event])
			if entryErr != nil {
				diagnostic.Issue = entryErr.Error()
				continue
			}
			for _, entry := range entries {
				handlers, handlerErr := objectSlice(entry["hooks"])
				if handlerErr != nil {
					continue
				}
				for _, handler := range handlers {
					owned, ownerRoot, ownerErr := ownedHandler(handler, platform, event)
					if ownerErr != nil {
						if handler["statusMessage"] == nativeHookStatus {
							diagnostic.Issue = ownerErr.Error()
						}
						continue
					}
					if !owned || ownerRoot != root {
						continue
					}
					expectedMatcher := ""
					if event == "PreToolUse" {
						if platform == "claude-code" {
							expectedMatcher = "Edit|Write|NotebookEdit"
						} else {
							expectedMatcher = "apply_patch"
						}
					}
					matcher, _ := entry["matcher"].(string)
					if matcher != expectedMatcher {
						diagnostic.Issue = "owned handler has unexpected matcher"
					}
					if async, ok := handler["async"].(bool); ok && async {
						diagnostic.Issue = "owned handler must be synchronous"
					}
					if !validHookTimeout(handler["timeout"]) {
						diagnostic.Issue = "owned handler timeout must be at least 120 seconds"
					}
					args, executable, _ := hookCommand(handler, platform)
					flags, _ := parseFlags(args)
					expectedDigest := ""
					if event == "Stop" {
						expectedDigest = checksDigest
					}
					if flags["checks-digest"] != expectedDigest {
						diagnostic.Issue = "owned handler has missing or stale check approval digest; sync hooks and review the changed checks"
					}
					if diagnostic.Executable != "" && diagnostic.Executable != executable {
						diagnostic.Issue = "owned handlers use different executables"
					}
					diagnostic.Executable = executable
					diagnostic.Events = append(diagnostic.Events, event)
					counts[event]++
				}
			}
		}
		for event, count := range counts {
			if count != 1 {
				diagnostic.Issue = fmt.Sprintf("owned %s handler count is %d", event, count)
			}
		}
		expectedPre, expectedStop := 0, 0
		if wantPre {
			expectedPre = 1
		}
		if wantStop {
			expectedStop = 1
		}
		diagnostic.Installed = counts["PreToolUse"] == expectedPre && counts["Stop"] == expectedStop && (wantPre || wantStop) && diagnostic.Issue == ""
		result = append(result, diagnostic)
	}
	return result, nil
}

func validHookTimeout(value any) bool {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Int64()
		return err == nil && parsed >= 120
	case float64:
		return number >= 120
	case int:
		return number >= 120
	default:
		return false
	}
}

// PlanHooks validates and renders all native hook config updates without writing.
func PlanHooks(dir string, project ir.Project, executable string) ([]HookArtifact, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolving project root: %w", err)
	}
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("locating harnest executable: %w", err)
		}
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, fmt.Errorf("resolving harnest executable: %w", err)
	}

	selected, err := rules.SelectHooks(project.PolicyRules, project.Hooks.Rules)
	if err != nil {
		return nil, err
	}
	active := len(selected) > 0
	checksDigest, err := selectedChecksDigest(root, project, selected)
	if err != nil {
		return nil, err
	}
	if active && runtime.GOOS == "windows" {
		return nil, fmt.Errorf("native hook installation is not verified on Windows")
	}
	pre, stop := hookEvents(selected)

	targets := append([]string(nil), project.Targets...)
	if !active { // Empty selection also removes stale bindings from either supported host.
		targets = []string{"claude-code", "codex"}
	}
	sort.Strings(targets)
	var artifacts []HookArtifact
	for _, target := range targets {
		if target != "claude-code" && target != "codex" {
			if active {
				return nil, fmt.Errorf("native hooks are unsupported for target %q", target)
			}
			continue
		}
		artifact, changed, err := planPlatformHooks(root, project, executable, target, checksDigest, active && (pre || stop), pre, stop)
		if err != nil {
			return nil, fmt.Errorf("planning %s hooks: %w", target, err)
		}
		if changed {
			artifacts = append(artifacts, artifact)
		}
	}
	if active {
		ignoreArtifacts, err := planNativeIgnores(root, targets)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, ignoreArtifacts...)
	}
	return artifacts, nil
}

func selectedChecksDigest(root string, project ir.Project, selected []rules.Rule) (string, error) {
	seen := map[string]bool{}
	var definitions []checks.Check
	for _, rule := range selected {
		for _, enforcement := range rule.Enforcement {
			if enforcement.Type != "require-check" || seen[enforcement.Check] {
				continue
			}
			check, err := checks.Load(root, project.Checks.Root, enforcement.Check)
			if err != nil {
				return "", fmt.Errorf("hook rule %q: %w", rule.ID, err)
			}
			if !check.Approved {
				return "", fmt.Errorf("hook rule %q uses unapproved check %q", rule.ID, check.ID)
			}
			definitions = append(definitions, check)
			seen[enforcement.Check] = true
		}
	}
	return checks.Digest(root, definitions)
}

const nativeIgnoreMarker = "# Harnest native hooks"

func planNativeIgnores(projectRoot string, targets []string) ([]HookArtifact, error) {
	type ignorePlan struct {
		entries map[string]bool
		checks  map[string][]string
	}
	plans := map[string]*ignorePlan{}
	add := func(root, target string, directory bool) error {
		ignorePath := filepath.Join(root, ".gitignore")
		patternRoot := root
		gitRepo := false
		if common, ok := gitPath(root, "--git-common-dir"); ok {
			gitRepo = true
			ignorePath = filepath.Join(common, "info", "exclude")
			if top, topOK := gitPath(root, "--show-toplevel"); topOK {
				patternRoot = top
			}
		}
		if err := validateLocalConfigPath(filepath.Dir(ignorePath), ignorePath); err != nil {
			return err
		}
		plan := plans[ignorePath]
		if plan == nil {
			plan = &ignorePlan{entries: map[string]bool{}, checks: map[string][]string{}}
			plans[ignorePath] = plan
		}
		rel, err := filepath.Rel(patternRoot, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("ignore target escapes source root: %s", target)
		}
		entry, err := literalGitignorePath(rel)
		if err != nil {
			return err
		}
		entry = "/" + entry
		if directory {
			entry = strings.TrimSuffix(entry, "/") + "/"
		}
		plan.entries[entry] = true
		if gitRepo {
			checkPath := filepath.ToSlash(rel)
			if directory {
				checkPath += "/"
			}
			plan.checks[patternRoot] = append(plan.checks[patternRoot], checkPath)
		}
		return nil
	}
	for _, target := range targets {
		switch target {
		case "claude-code":
			root := claudeSettingsRoot(projectRoot)
			if err := add(root, filepath.Join(root, ".claude", "settings.local.json"), false); err != nil {
				return nil, err
			}
		case "codex":
			if err := add(projectRoot, filepath.Join(projectRoot, ".codex", "hooks.json"), false); err != nil {
				return nil, err
			}
		}
	}
	if err := add(projectRoot, filepath.Join(projectRoot, ".harnest", "state"), true); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(plans))
	for path := range plans {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var artifacts []HookArtifact
	for _, path := range paths {
		plan := plans[path]
		before, err := os.ReadFile(path)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		existing := map[string]bool{}
		for _, line := range strings.Split(string(before), "\n") {
			existing[strings.TrimSpace(line)] = true
		}
		var missing []string
		for entry := range plan.entries {
			if !existing[entry] {
				missing = append(missing, entry)
			}
		}
		sort.Strings(missing)
		content := append([]byte(nil), before...)
		if len(missing) > 0 {
			if len(content) > 0 && content[len(content)-1] != '\n' {
				content = append(content, '\n')
			}
			if len(content) > 0 {
				content = append(content, '\n')
			}
			content = append(content, nativeIgnoreMarker...)
			content = append(content, '\n')
			for _, entry := range missing {
				content = append(content, entry...)
				content = append(content, '\n')
			}
		}
		for root, checkPaths := range plan.checks {
			if err := validateNativeIgnores(root, content, checkPaths); err != nil {
				return nil, err
			}
		}
		if len(missing) == 0 {
			continue
		}
		mode := os.FileMode(0644)
		if exists {
			info, err := os.Stat(path)
			if err != nil {
				return nil, err
			}
			mode = info.Mode().Perm()
		}
		artifacts = append(artifacts, HookArtifact{Path: path, Content: content, Before: before, Exists: exists, Mode: mode})
	}
	return artifacts, nil
}

// Check the proposed exclude with Git's real .gitignore precedence, without
// changing the checkout or its Git metadata, including during dry-run.
func validateNativeIgnores(root string, exclude []byte, paths []string) error {
	gitDir, err := os.MkdirTemp("", "harnest-hook-ignore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(gitDir)
	if output, err := exec.Command("git", "init", "--bare", "--quiet", "--template=", gitDir).CombinedOutput(); err != nil {
		return fmt.Errorf("preparing native ignore check: %w: %s", err, output)
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "info"), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(gitDir, "info", "exclude"), exclude, 0600); err != nil {
		return err
	}
	args := []string{"-C", root, "--git-dir=" + gitDir, "--work-tree=" + root, "-c", "core.bare=false"}
	for _, setting := range []string{"core.ignoreCase", "core.excludesFile"} {
		value, err := exec.Command("git", "-C", root, "config", "--path", "--get", setting).Output()
		if err == nil {
			args = append(args, "-c", setting+"="+strings.TrimSpace(string(value)))
		} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return fmt.Errorf("reading native ignore setting %s: %w", setting, err)
		}
	}
	for _, path := range paths {
		cmd := exec.Command("git", append(args, "check-ignore", "--quiet", "--no-index", "--", path)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("native hook path is not effectively ignored: %s: %w: %s", filepath.Join(root, path), err, output)
		}
	}
	return nil
}

func literalGitignorePath(path string) (string, error) {
	path = filepath.ToSlash(path)
	if strings.ContainsAny(path, "\r\n") {
		return "", fmt.Errorf("native ignore path contains newline and cannot be encoded: %q", path)
	}
	replacer := strings.NewReplacer("\\", "\\\\", " ", "\\ ", "#", "\\#", "!", "\\!", "[", "\\[", "]", "\\]", "*", "\\*", "?", "\\?")
	return replacer.Replace(path), nil
}

func hookEvents(selected []rules.Rule) (pre, stop bool) {
	for _, rule := range selected {
		for _, enforcement := range rule.Enforcement {
			switch enforcement.Type {
			case "protect-path":
				stop = true
				if rule.Severity == rules.Hard {
					pre = true
				}
			case "require-check":
				stop = true
			}
		}
	}
	return pre, stop
}

func planPlatformHooks(root string, project ir.Project, executable, platform, checksDigest string, active, pre, stop bool) (HookArtifact, bool, error) {
	configRoot := root
	path := filepath.Join(root, ".codex", "hooks.json")
	if platform == "claude-code" {
		configRoot = claudeSettingsRoot(root)
		path = filepath.Join(configRoot, ".claude", "settings.local.json")
	}
	if platform == "codex" && active {
		if err := rejectInlineCodexHooks(root); err != nil {
			return HookArtifact{}, false, err
		}
	}
	if active {
		if err := validateLocalConfigPath(configRoot, path); err != nil {
			return HookArtifact{}, false, err
		}
	} else {
		info, err := os.Stat(path)
		if os.IsNotExist(err) || (err == nil && !info.Mode().IsRegular()) {
			return HookArtifact{}, false, nil
		}
		if err != nil {
			return HookArtifact{}, false, err
		}
	}
	before, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return HookArtifact{}, false, fmt.Errorf("reading %s: %w", path, err)
	}
	if !active && !bytes.Contains(before, []byte(`"Harnest native hooks"`)) {
		return HookArtifact{}, false, nil
	}
	var doc map[string]any
	if exists {
		decoder := json.NewDecoder(bytes.NewReader(before))
		decoder.UseNumber()
		if err := decoder.Decode(&doc); err != nil {
			return HookArtifact{}, false, fmt.Errorf("parsing %s: %w", path, err)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			return HookArtifact{}, false, fmt.Errorf("parsing %s: trailing JSON value", path)
		}
	}
	if doc == nil {
		doc = map[string]any{}
	}
	changed, err := mergeNativeHooks(doc, platform, root, executable, checksDigest, active, pre, stop)
	if err != nil {
		return HookArtifact{}, false, err
	}
	if !changed {
		return HookArtifact{}, false, nil
	}
	if err := validateLocalConfigPath(configRoot, path); err != nil {
		return HookArtifact{}, false, err
	}
	tracked, err := isTracked(configRoot, path)
	if err != nil {
		return HookArtifact{}, false, err
	}
	if tracked {
		return HookArtifact{}, false, fmt.Errorf("native config is tracked and will not be overwritten: %s", path)
	}
	mode := os.FileMode(0600)
	if exists {
		info, err := os.Stat(path)
		if err != nil {
			return HookArtifact{}, false, err
		}
		mode = info.Mode().Perm()
	}
	if len(doc) == 0 && exists {
		return HookArtifact{Path: path, Before: before, Exists: true, Remove: true, Mode: mode}, true, nil
	}
	content, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return HookArtifact{}, false, err
	}
	content = append(content, '\n')
	return HookArtifact{Path: path, Content: content, Before: before, Exists: exists, Mode: mode}, true, nil
}

func validateLocalConfigPath(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("native config escapes source root: %s", path)
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("native config path contains symlink: %s", current)
		}
	}
	return nil
}

func mergeNativeHooks(doc map[string]any, platform, root, executable, checksDigest string, active, pre, stop bool) (bool, error) {
	original, _ := json.Marshal(doc)
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		if doc["hooks"] != nil {
			return false, fmt.Errorf("hooks must be a JSON object")
		}
		hooks = map[string]any{}
	}
	for _, event := range []string{"PreToolUse", "Stop"} {
		entries, err := objectSlice(hooks[event])
		if err != nil {
			return false, fmt.Errorf("hooks.%s: %w", event, err)
		}
		kept := make([]any, 0, len(entries)+1)
		for _, entry := range entries {
			handlers, err := objectSlice(entry["hooks"])
			if err != nil {
				kept = append(kept, entry)
				continue
			}
			remaining := make([]any, 0, len(handlers))
			for _, handler := range handlers {
				owned, otherRoot, err := ownedHandler(handler, platform, event)
				if err != nil {
					return false, err
				}
				if !owned {
					remaining = append(remaining, handler)
					continue
				}
				if otherRoot != root {
					if rootsOverlap(otherRoot, root) && sameCheckout(otherRoot, root) {
						return false, fmt.Errorf("Harnest hook for overlapping project root %q already exists", otherRoot)
					}
					remaining = append(remaining, handler)
				}
			}
			if len(remaining) > 0 {
				copyEntry := make(map[string]any, len(entry))
				for key, value := range entry {
					copyEntry[key] = value
				}
				copyEntry["hooks"] = remaining
				kept = append(kept, copyEntry)
			}
		}
		want := active && ((event == "PreToolUse" && pre) || (event == "Stop" && stop))
		if want {
			kept = append(kept, nativeEntry(platform, event, root, executable, checksDigest))
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	if len(hooks) == 0 {
		delete(doc, "hooks")
	} else {
		doc["hooks"] = hooks
	}
	updated, _ := json.Marshal(doc)
	return !bytes.Equal(original, updated), nil
}

func nativeEntry(platform, event, root, executable, checksDigest string) map[string]any {
	eventArg := "stop"
	matcher := ""
	if event == "PreToolUse" {
		eventArg = "pre-tool-use"
		if platform == "claude-code" {
			matcher = "Edit|Write|NotebookEdit"
		} else {
			matcher = "apply_patch"
		}
	}
	args := []string{"hook", "evaluate", "--platform", platform, "--event", eventArg, "--project", root}
	if event == "Stop" && checksDigest != "" {
		args = append(args, "--checks-digest", checksDigest)
	}
	handler := map[string]any{"type": "command", "timeout": 120, "statusMessage": nativeHookStatus}
	parts := append([]string{executable}, args...)
	for i := range parts {
		parts[i] = shellQuote(parts[i])
	}
	handler["command"] = strings.Join(parts, " ")
	entry := map[string]any{"hooks": []any{handler}}
	if matcher != "" {
		entry["matcher"] = matcher
	}
	return entry
}

func ownedHandler(handler map[string]any, platform, event string) (bool, string, error) {
	if handler["statusMessage"] != nativeHookStatus {
		return false, "", nil
	}
	if handler["type"] != "command" {
		return false, "", fmt.Errorf("ambiguous %q handler: type is not command", nativeHookStatus)
	}
	args, executable, ok := hookCommand(handler, platform)
	if !ok || !filepath.IsAbs(executable) {
		return false, "", fmt.Errorf("ambiguous %q handler: command is not a recognized absolute Harnest hook", nativeHookStatus)
	}
	values, ok := parseFlags(args)
	expectedEvent := "stop"
	if event == "PreToolUse" {
		expectedEvent = "pre-tool-use"
	}
	if !ok || values["platform"] != platform || values["event"] != expectedEvent || !filepath.IsAbs(values["project"]) {
		return false, "", fmt.Errorf("ambiguous %q handler: platform, event, or project does not match its config location", nativeHookStatus)
	}
	return true, filepath.Clean(values["project"]), nil
}

func hookCommand(handler map[string]any, platform string) ([]string, string, bool) {
	// Recognize older generated Claude entries so sync can migrate them.
	if platform == "claude-code" && handler["args"] != nil {
		command, ok := handler["command"].(string)
		if !ok || command == "" {
			return nil, "", false
		}
		values, err := stringSlice(handler["args"])
		if err != nil || len(values) < 2 || values[0] != "hook" || values[1] != "evaluate" {
			return nil, "", false
		}
		return values[2:], command, true
	}
	command, ok := handler["command"].(string)
	if !ok {
		return nil, "", false
	}
	parts, ok := splitShellWords(command)
	if !ok || len(parts) < 3 || parts[1] != "hook" || parts[2] != "evaluate" {
		return nil, "", false
	}
	quoted := make([]string, len(parts))
	for i := range parts {
		quoted[i] = shellQuote(parts[i])
	}
	if strings.Join(quoted, " ") != command {
		return nil, "", false
	}
	return parts[3:], parts[0], true
}

func parseFlags(args []string) (map[string]string, bool) {
	result := map[string]string{}
	if len(args) != 6 && len(args) != 8 {
		return nil, false
	}
	for i := 0; i+1 < len(args); i += 2 {
		key := strings.TrimPrefix(args[i], "--")
		if args[i] == key || (key != "platform" && key != "event" && key != "project" && key != "checks-digest") || result[key] != "" || args[i+1] == "" {
			return nil, false
		}
		if key == "checks-digest" && (len(args[i+1]) != 64 || strings.Trim(args[i+1], "0123456789abcdef") != "") {
			return nil, false
		}
		result[key] = args[i+1]
	}
	return result, result["platform"] != "" && result["event"] != "" && result["project"] != ""
}

func objectSlice(value any) ([]map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("must be an array")
	}
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("array entries must be objects")
		}
		result = append(result, object)
	}
	return result, nil
}

func stringSlice(value any) ([]string, error) {
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("must be an array")
	}
	result := make([]string, len(raw))
	for i, item := range raw {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("entries must be strings")
		}
		result[i] = text
	}
	return result, nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func splitShellWords(command string) ([]string, bool) {
	var words []string
	var b strings.Builder
	quote := byte(0)
	for i := 0; i < len(command); i++ {
		c := command[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				b.WriteByte(c)
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case ' ', '\t':
			if b.Len() > 0 {
				words = append(words, b.String())
				b.Reset()
			}
		default:
			b.WriteByte(c)
		}
	}
	if quote != 0 {
		return nil, false
	}
	if b.Len() > 0 {
		words = append(words, b.String())
	}
	return words, true
}

func rejectInlineCodexHooks(root string) error {
	data, err := os.ReadFile(filepath.Join(root, ".codex", "config.toml"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if tomlHooksSyntax(trimmed) {
			return fmt.Errorf("inline hooks in .codex/config.toml conflict with .codex/hooks.json; reconcile them manually")
		}
	}
	return nil
}

func tomlHooksSyntax(line string) bool {
	if strings.HasPrefix(line, "[") {
		section := strings.TrimSpace(strings.Trim(line, "[]"))
		first := strings.Trim(strings.TrimSpace(strings.SplitN(section, ".", 2)[0]), "\"'")
		return first == "hooks"
	}
	if equals := strings.IndexByte(line, '='); equals >= 0 {
		key := strings.TrimSpace(line[:equals])
		first := strings.Trim(strings.TrimSpace(strings.SplitN(key, ".", 2)[0]), "\"'")
		return first == "hooks"
	}
	return false
}

func claudeSettingsRoot(root string) string {
	if runtime.GOOS == "windows" {
		return root
	}
	top, topOK := gitPath(root, "--show-toplevel")
	common, commonOK := gitPath(root, "--git-common-dir")
	if !topOK || !commonOK {
		return root
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(top, common)
	}
	common, err := filepath.EvalSymlinks(filepath.Clean(common))
	if err != nil || filepath.Base(common) != ".git" {
		return root
	}
	mainRoot := filepath.Dir(common)
	userHome, _ := os.UserHomeDir()
	if mainRoot == userHome {
		return root
	}
	return mainRoot
}

func rootsOverlap(a, b string) bool {
	rel, err := filepath.Rel(a, b)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	rel, err = filepath.Rel(b, a)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sameCheckout(a, b string) bool {
	left, leftOK := checkoutID(a)
	right, rightOK := checkoutID(b)
	return leftOK && rightOK && left == right
}

func checkoutID(root string) (string, bool) {
	return gitPath(root, "--show-toplevel")
}

func gitPath(root, flag string) (string, bool) {
	output, err := exec.Command("git", "-C", root, "rev-parse", flag).Output()
	if err != nil {
		return "", false
	}
	path := strings.TrimSuffix(string(output), "\n")
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	return resolved, err == nil
}

func isTracked(root, path string) (bool, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false, err
	}
	cmd := exec.Command("git", "-C", root, "ls-files", "--error-unmatch", "--", filepath.ToSlash(rel))
	err = cmd.Run()
	if err == nil {
		return true, nil
	}
	if _, ok := err.(*exec.ExitError); ok {
		return false, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("checking whether %s is tracked: %w", path, err)
}

// ApplyHooks atomically writes a validated plan and rolls back earlier writes on failure.
func ApplyHooks(artifacts []HookArtifact) ([]string, error) {
	var written []HookArtifact
	fail := func(cause error) ([]string, error) {
		var failed []string
		for i := len(written) - 1; i >= 0; i-- {
			if err := restoreArtifact(written[i]); err != nil {
				failed = append(failed, written[i].Path)
			}
		}
		if len(failed) > 0 {
			return failed, fmt.Errorf("%w; rollback failed for %s", cause, strings.Join(failed, ", "))
		}
		return nil, cause
	}
	for _, artifact := range artifacts {
		current, err := os.ReadFile(artifact.Path)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return fail(err)
		}
		if exists != artifact.Exists || !bytes.Equal(current, artifact.Before) {
			return fail(fmt.Errorf("native config changed after planning: %s", artifact.Path))
		}
		if exists {
			info, err := os.Stat(artifact.Path)
			if err != nil {
				return fail(err)
			}
			if info.Mode().Perm() != artifact.Mode {
				return fail(fmt.Errorf("native config permissions changed after planning: %s", artifact.Path))
			}
		} else if artifact.Mode == 0 {
			artifact.Mode = 0600
		}
		var writeErr error
		if artifact.Remove {
			writeErr = os.Remove(artifact.Path)
		} else {
			writeErr = writeAtomic(artifact.Path, artifact.Content, artifact.Mode)
		}
		if writeErr != nil {
			return fail(fmt.Errorf("writing %s: %w", artifact.Path, writeErr))
		}
		written = append(written, artifact)
	}
	return pathsOf(written), nil
}

func writeAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".harnest-hooks-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(content); err == nil {
		err = tmp.Chmod(mode)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func restoreArtifact(artifact HookArtifact) error {
	current, err := os.ReadFile(artifact.Path)
	if artifact.Remove {
		if !os.IsNotExist(err) {
			return fmt.Errorf("rollback target recreated: %s", artifact.Path)
		}
	} else if err != nil || !bytes.Equal(current, artifact.Content) {
		return fmt.Errorf("rollback target changed: %s", artifact.Path)
	}
	if !artifact.Exists {
		return os.Remove(artifact.Path)
	}
	return writeAtomic(artifact.Path, artifact.Before, artifact.Mode)
}
func pathsOf(artifacts []HookArtifact) []string {
	result := make([]string, len(artifacts))
	for i := range artifacts {
		result[i] = artifacts[i].Path
	}
	return result
}
