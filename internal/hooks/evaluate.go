// Package hooks evaluates selected project rules using native command-hook protocols.
package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/daniilsintsov/harnest-universal/internal/checks"
	"github.com/daniilsintsov/harnest-universal/internal/rules"
	"github.com/daniilsintsov/harnest-universal/internal/verify"
	harnestYaml "github.com/daniilsintsov/harnest-universal/internal/yaml"
)

const MaxInputBytes = 1 << 20

type Options struct {
	Platform string
	Event    string
	Project  string
}

type Event struct {
	Name           string          `json:"hook_event_name"`
	CWD            string          `json:"cwd"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	StopHookActive *bool           `json:"stop_hook_active"`
	SessionID      string          `json:"session_id"`
	TurnID         string          `json:"turn_id"`
	ToolUseID      string          `json:"tool_use_id"`
}

type Record struct {
	Status  string   `json:"status"`
	Reason  string   `json:"reason"`
	RuleIDs []string `json:"rule_ids,omitempty"`
	CheckID string   `json:"check_id,omitempty"`
	Base    string   `json:"base,omitempty"`
}

type Result struct {
	Status  string
	Reason  string
	Records []Record
	Output  map[string]any
}

// Evaluate always produces a native JSON response. Policy/check output never
// shares stdout with the host protocol, including when evaluation fails.
func Evaluate(ctx context.Context, options Options, input io.Reader) (result Result) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 110*time.Second)
	defer cancel()
	var event Event
	root := ""
	validEvent := false
	defer func() {
		if len(result.Records) == 0 {
			result.Records = []Record{{Status: result.Status, Reason: result.Reason}}
		}
		responseEvent := options.Event
		if event.Name == "PreToolUse" {
			responseEvent = "pre-tool-use"
		} else if event.Name == "Stop" {
			responseEvent = "stop"
		}
		result.Output = nativeOutput(responseEvent, validEvent, event.StopHookActive, result)
		if root != "" {
			logOptions := options
			logOptions.Event = responseEvent
			if err := appendLog(root, logOptions, event, result.Records, time.Since(started)); err != nil {
				message := "Harnest: local hook history could not be written; run harnest doctor."
				if previous, ok := result.Output["systemMessage"].(string); ok {
					message = previous + " " + message
				}
				result.Output["systemMessage"] = message
			}
		}
	}()
	fail := func(status, reason string) Result { return Result{Status: status, Reason: reason} }
	data, inputErr := io.ReadAll(io.LimitReader(input, MaxInputBytes+1))
	decodeErr := json.Unmarshal(data, &event)
	if (options.Platform != "claude-code" && options.Platform != "codex") || (options.Event != "pre-tool-use" && options.Event != "stop") || !filepath.IsAbs(options.Project) {
		return fail("evaluation-error", "invalid evaluator arguments; regenerate native hooks")
	}
	var err error
	root, err = canonical(options.Project)
	if err != nil {
		root = ""
		return fail("evaluation-error", "registered project root cannot be resolved")
	}
	if inputErr != nil || len(data) > MaxInputBytes || decodeErr != nil {
		return fail("evaluation-error", "invalid or oversized native event; inspection required")
	}
	want := "PreToolUse"
	if options.Event == "stop" {
		want = "Stop"
	}
	if event.Name != want || !filepath.IsAbs(event.CWD) {
		return fail("evaluation-error", "native event name or cwd does not match its binding")
	}
	if options.Event == "stop" && event.StopHookActive == nil {
		return fail("evaluation-error", "Stop event lacks a reliable repetition flag; inspection required")
	}
	validEvent = true
	cwd, err := canonical(event.CWD)
	if err != nil {
		return fail("evaluation-error", "event cwd cannot be resolved")
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return fail("evaluation-error", "event cwd must be an existing directory")
	}
	if !inside(root, cwd) {
		root = "" // A shared native source must not write another project's history.
		return fail("not-applicable", "event belongs to another project")
	}
	projectGit, projectErr := checkoutRoot(ctx, root)
	cwdGit, cwdErr := checkoutRoot(ctx, cwd)
	if (projectErr == nil && cwdErr == nil && projectGit != cwdGit) || (projectErr != nil && cwdErr == nil) {
		root = ""
		return fail("not-applicable", "event belongs to another Git checkout")
	}
	if projectErr == nil && cwdErr != nil {
		return fail("evaluation-error", "cannot verify event Git checkout")
	}
	local, err := harnestYaml.LoadLocal(root)
	if err != nil {
		return fail("evaluation-error", "cannot load local hook switch; repair .harnest-local.yaml")
	}
	if local != nil && local.Hooks.Enabled != nil && !*local.Hooks.Enabled {
		return fail("disabled", "hooks disabled by .harnest-local.yaml")
	}
	cfg, err := harnestYaml.Load(root)
	if err != nil {
		return fail("evaluation-error", "cannot load project policy; run harnest doctor")
	}
	project, err := harnestYaml.BuildIR(root, cfg)
	if err != nil {
		return fail("evaluation-error", "invalid selected policy; run harnest doctor")
	}
	selected, err := rules.SelectHooks(project.PolicyRules, project.Hooks.Rules)
	if err != nil {
		return fail("evaluation-error", "invalid selected rules; run harnest doctor")
	}
	targetFound := false
	for _, target := range project.Targets {
		targetFound = targetFound || target == options.Platform
	}
	if !targetFound || len(selected) == 0 {
		return fail("not-applicable", "no selected rules for this native target")
	}
	if options.Event == "pre-tool-use" {
		changes, supported, err := toolChanges(options.Platform, cwd, event)
		if !supported {
			return fail("not-applicable", "tool has no supported preflight contract")
		}
		if err != nil {
			return fail("evaluation-error", "supported tool input cannot be safely evaluated")
		}
		changes, err = normalizeChanges(root, cwd, changes)
		if err != nil {
			return fail("evaluation-error", "supported tool paths cannot be safely evaluated")
		}
		result = Result{Status: "not-applicable", Reason: "no selected hard rule applies"}
		for _, rule := range selected {
			if rule.Severity != rules.Hard {
				continue
			}
			for _, enforcement := range rule.Enforcement {
				for _, change := range changes {
					match, err := rules.MatchesChange(rule.Scope, enforcement.Paths, change)
					if err != nil {
						return fail("evaluation-error", "invalid selected path policy; run harnest doctor")
					}
					if match {
						reason := fmt.Sprintf("rule %s forbids this file operation; choose an allowed path or ask the owner to revise the rule", rule.ID)
						result.Status, result.Reason = "violation", reason
						result.Records = append(result.Records, Record{Status: "violation", Reason: reason, RuleIDs: []string{rule.ID}})
						break
					}
				}
			}
		}
		return result
	}
	changes, base, err := verify.DiscoverChangesContext(ctx, root, "")
	if err != nil {
		return fail("not-verified", "Git changes could not be determined; run harnest verify --changed")
	}
	changes, err = normalizeChanges(root, root, changes)
	if err != nil {
		return fail("evaluation-error", "changed paths cannot be safely evaluated")
	}
	result = Result{Status: "not-applicable", Reason: "no selected rule applies to discovered changes"}
	if base == "" {
		base = "worktree-only (no mainline base)"
	}
	result.Reason += "; base=" + base
	required := map[string][]string{}
	for _, rule := range selected {
		for _, enforcement := range rule.Enforcement {
			applicable := false
			for _, change := range changes {
				var protected []string
				if enforcement.Type == "protect-path" {
					protected = enforcement.Paths
				}
				match, matchErr := rules.MatchesChange(rule.Scope, protected, change)
				if matchErr != nil {
					return fail("evaluation-error", "invalid selected path policy; run harnest doctor")
				}
				applicable = applicable || match
			}
			if !applicable {
				continue
			}
			if enforcement.Type == "protect-path" {
				result.Records = append(result.Records, Record{Status: "violation", Reason: "protected path changed; review existing changes with the owner", RuleIDs: []string{rule.ID}})
			} else {
				required[enforcement.Check] = appendUnique(required[enforcement.Check], rule.ID)
			}
		}
	}
	var checkIDs, files []string
	for id := range required {
		checkIDs = append(checkIDs, id)
	}
	sort.Strings(checkIDs)
	for _, change := range changes {
		files = appendUnique(files, change.Path)
	}
	sort.Strings(files)
	for _, id := range checkIDs {
		record := Record{Status: "passed", Reason: "approved check passed", RuleIDs: required[id], CheckID: id}
		check, err := checks.Load(root, project.Checks.Root, id)
		if err == nil {
			err = checks.RunContext(ctx, root, check, files)
		}
		if err != nil {
			record.Status, record.Reason = "evaluation-error", "check could not complete; inspect definition, approval, executable and limits"
			if checks.IsFailure(err) {
				record.Status, record.Reason = "violation", "check failed; run harnest verify --changed for check output and fix the reported rule"
			}
		}
		result.Records = append(result.Records, record)
	}
	for _, record := range result.Records {
		if result.Status == "not-applicable" || record.Status == "evaluation-error" || (record.Status == "violation" && result.Status == "passed") {
			result.Status = record.Status
			result.Reason = fmt.Sprintf("%s: rules=%s check=%s; %s; base=%s", record.Status, strings.Join(record.RuleIDs, ","), record.CheckID, record.Reason, base)
		}
	}
	for i := range result.Records {
		result.Records[i].Base = base
	}
	return result
}

func nativeOutput(event string, valid bool, repeated *bool, result Result) map[string]any {
	output := map[string]any{}
	failure := result.Status == "violation" || result.Status == "evaluation-error" || result.Status == "not-verified"
	if event == "pre-tool-use" && failure {
		output["hookSpecificOutput"] = map[string]any{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": "Harnest: " + result.Reason}
	} else if event == "stop" && failure && valid && repeated != nil && !*repeated {
		output["decision"], output["reason"] = "block", "Harnest: "+result.Reason
	}
	if failure || result.Status == "disabled" {
		output["systemMessage"] = "Harnest " + result.Status + ": " + result.Reason
	}
	return output
}

func checkoutRoot(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	data, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return canonical(strings.TrimSuffix(string(data), "\n"))
}

func toolChanges(platform, cwd string, event Event) ([]rules.Change, bool, error) {
	if platform == "codex" {
		if event.ToolName != "apply_patch" {
			return nil, false, nil
		}
		var input struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(event.ToolInput, &input); err != nil {
			return nil, true, err
		}
		changes, err := patchChanges(cwd, input.Command)
		return changes, true, err
	}
	if event.ToolName != "Write" && event.ToolName != "Edit" && event.ToolName != "NotebookEdit" {
		return nil, false, nil
	}
	var input struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	}
	if err := json.Unmarshal(event.ToolInput, &input); err != nil {
		return nil, true, err
	}
	name, operation := input.FilePath, "update"
	if event.ToolName == "NotebookEdit" {
		name = input.NotebookPath
	}
	if name == "" {
		return nil, true, fmt.Errorf("missing file path")
	}
	if event.ToolName == "Write" {
		absolute := name
		if !filepath.IsAbs(absolute) {
			absolute = cwd + string(filepath.Separator) + absolute
		}
		if _, err := os.Stat(absolute); os.IsNotExist(err) {
			operation = "create"
		} else if err != nil {
			return nil, true, err
		}
	}
	return []rules.Change{{Path: name, Operation: operation}}, true, nil
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
