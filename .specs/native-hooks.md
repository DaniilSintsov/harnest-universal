# Native hook enforcement for Harnest

Status: proposed  
Date: 2026-08-18  
Targets: Claude Code, Codex  
Platforms: macOS, Linux, Windows

## 1. Problem

Harnest currently expresses workflow and policy mostly through generated instructions in `CLAUDE.md` and `AGENTS.md`. A model may ignore those instructions. Existing rules and checks already describe mechanical constraints, but enforcement happens only when somebody explicitly runs `harnest verify --changed`.

Harnest needs to install native command hooks which evaluate canonical `.harnest/rules/*.yaml` without converting profiles into another orchestration system. This preserves the lower-token profile workflow while moving only deterministic enforcement out of model context.

Native hooks are guardrails, not a security boundary. Claude Code and Codex can skip a failed, missing, disabled, untrusted, or timed-out command hook. Some tool paths are not observable. CI, managed policy, or an OS sandbox remains necessary where bypass must be impossible.

## 2. Goals

- Compile existing `protect-path` and `require-check` enforcement into native Claude Code and Codex command hooks.
- Keep `.harnest/rules/*.yaml` and `.harnest/checks/*.yaml` as the only canonical policy sources.
- Load current rules for every event, so editing a rule requires no regeneration or watcher.
- Block a structured file mutation before execution when a `hard` `protect-path` rule matches.
- Re-run applicable `required` checks at every `Stop` event and continue the agent when they fail.
- Preserve non-Harnest hooks and make repeated generation idempotent.
- Support a local emergency disable switch with visible degraded status.
- Work on macOS, Linux, and Windows without shell scripts or extra runtime dependencies.

## 3. Non-goals for v1

- Replacing profiles, skills, agents, rules, or the Harnest Research → Plan → Executing workflow.
- Generating prompt hooks, agent hooks, workflows, or model-backed decisions.
- `deny-command`, regex command policies, or shell-language parsing.
- Preflight enforcement for arbitrary Bash/PowerShell, MCP, hosted, or unknown tools.
- A daemon, file watcher, cache, persistent audit log, telemetry, or per-rule bypass UI.
- Claiming strict enforcement when the native host does not run the hook.
- Mechanically interpreting `scope.domains`.

## 4. Product decisions

1. Rules generate deterministic command hooks only.
2. `hard` means preflight denial where the platform exposes enough structured data. `required` means validation at `Stop`. `preference` never generates a hook decision.
3. Profiles cannot enable or disable `hard` or `required` rules.
4. Hard rules have no action-level `--allow` bypass. Local emergency disable is the only v1 escape hatch.
5. Checks may execute only when their existing definition has `approved: true`. Repository trust is the approval boundary for changing that definition.
6. A hard rule which cannot compile for every configured target makes `harnest generate` fail before writing files. A required rule may fall back to CLI verification with a warning.
7. Hook evaluator errors fail closed when a potentially applicable hard rule cannot be evaluated. Other evaluator errors fail open with a concise diagnostic.
8. Generated native configs are machine-local and gitignored. Foreign entries are preserved.

## 5. Source model

No new rule schema is introduced.

```yaml
id: protect-production-config
title: Protect production configuration
severity: hard
statement: Production configuration cannot be edited by an agent.
scope:
  paths: ["config/**"]
  operations: ["create", "update", "delete", "move"]
enforcement:
  - type: protect-path
    paths: ["config/production/**"]
```

```yaml
id: test-changed-code
title: Test changed code before completion
severity: required
statement: Applicable tests must pass before completion.
scope:
  paths: ["**/*.go"]
enforcement:
  - type: require-check
    check: go-test
```

```yaml
id: go-test
command: go
args: ["test", "./..."]
approved: true
```

Supported v1 combinations:

| Severity | Enforcement | PreToolUse | Stop | Generation |
|---|---|---:|---:|---|
| `hard` | `protect-path` | deny matching structured writes | backstop verify | supported |
| `required` | `protect-path` | no | continue on changed protected path | supported |
| `required` | `require-check` | no | continue on failed check | supported |
| `preference` | either | no | no | prose only |
| `hard` | `require-check` | no safe preflight | no | error in v1 |
| any | `deny-command` | no | no | unsupported in v1 |

`scope.paths` and `enforcement.paths` are both filters; a path must satisfy both when both exist. `scope.operations` accepts only `create`, `update`, `delete`, and `move`. Unknown operations make a hard rule non-compilable and produce a warning for softer rules. `scope.domains` remains advisory and never narrows mechanical enforcement. A hard rule which relies only on domains is rejected.

## 6. Runtime design

One CLI entrypoint handles both native formats:

```text
native host event on stdin
        │
        ▼
harnest hook evaluate --platform <claude-code|codex> --event <pre-tool-use|stop>
        │
        ├─ find nearest ancestor harnest.yaml from event cwd
        ├─ read .harnest-local.yaml; exit success when hooks.enabled=false
        ├─ load current project IR, rules, and checks
        ├─ normalize vendor event
        ├─ evaluate existing Harnest policy
        └─ render vendor-specific JSON decision or exit 2
```

The evaluator is synchronous, reads exactly one bounded JSON object from stdin, writes only protocol output to stdout, and sends diagnostics to stderr. It stores neither hook input nor transcript content.

### 6.1 Project discovery

Start at the event `cwd`, resolve it to an absolute clean path, and walk ancestors until `harnest.yaml` is found. Stop at the filesystem root or Windows volume root. No project means successful no-op. This supports sessions started in a subdirectory and worktrees.

Tool paths are resolved relative to event `cwd`, then converted to a project-relative slash-separated path. Reject malformed paths and paths outside the project. Resolve existing symlinks and the nearest existing parent for new files before containment checks. Symlink swaps after evaluation remain an acknowledged OS-level race.

Reuse the glob semantics currently implemented by `internal/verify`; do not create a second matcher. Invalid globs are configuration errors rather than silent non-matches.

### 6.2 PreToolUse

The platform adapter extracts paths and operations only from known structured mutation tools:

- Claude Code: `Edit`, `Write`, and `NotebookEdit`.
- Codex: `apply_patch`, including every added, updated, deleted, and moved path in one patch.

Patch parsing must reject malformed headers, traversal, absolute paths, Windows UNC/device paths, drive-relative paths, and alternate data stream syntax. Both old and new paths of a move are evaluated.

Evaluation uses hard `protect-path` rules only. A match returns a native deny decision naming rule ID and safe remediation. If input cannot be decoded while at least one hard path rule might apply, return exit code `2`. Unknown tools pass and are covered only by Stop backstop where their effects appear in Git.

### 6.3 Stop

Stop invokes the same shared verification core as `harnest verify --changed`:

- discover changed files;
- apply rule scope;
- report hard or required protected-path violations;
- load each applicable approved check;
- execute each check once per Stop invocation using exact `command` and `args`, never a shell;
- continue the agent when any required result fails.

Checks are rerun on every Stop. v1 keeps no cache or retry counter. Native `stop_hook_active` is included in diagnostics but does not suppress a rerun because the agent may have changed files since the previous attempt. Native host loop protection is accepted as the final ceiling.

`harnest verify --allow <id>` must reject IDs belonging to hard rules. The native evaluator never accepts allow IDs.

### 6.4 Errors

| Failure | Result |
|---|---|
| malformed rule set; hard rules may be hidden | block with exit `2` |
| malformed hard-rule tool input | block with exit `2` |
| missing/unapproved check for required rule | allow Stop with diagnostic |
| ordinary required check failure | block Stop and continue agent with diagnostic |
| no project, no applicable rule, preference only | success/no decision |
| hooks locally disabled | success/no decision; `doctor` warns |

Diagnostics must not echo full tool input, environment, transcript, or check secrets.

## 7. Generated native configuration

`harnest generate` installs one Harnest aggregator for `PreToolUse` and one for `Stop` per configured target. No hook is generated per rule because matching native hooks can run concurrently.

### Claude Code

- File: `.claude/settings.local.json`.
- `PreToolUse` matcher covers the known structured file tools.
- Command handler uses an absolute path returned by `os.Executable` plus structured `args`.
- File is added to the Harnest gitignore block.

Claude Code documents `.claude/settings.local.json` as project-local and non-shareable, and documents `PreToolUse` as block-capable: [Claude Code hooks reference](https://code.claude.com/docs/en/hooks).

### Codex

- File: `.codex/hooks.json`.
- `PreToolUse` matcher covers `apply_patch` and its `Edit`/`Write` aliases.
- Command is a correctly quoted absolute invocation. On Windows, emit the platform-appropriate `commandWindows` form.
- File is added to the Harnest gitignore block.

Codex documents project `hooks.json`, hash-based trust, `commandWindows`, current command-only handlers, and the fact that specialized paths may bypass hooks: [Codex hooks reference](https://learn.chatgpt.com/docs/hooks).

Codex has no project-local uncommitted override filename equivalent to Claude's `settings.local.json`. Therefore generation refuses to modify a tracked `.codex/hooks.json`. It also refuses generation when `.codex/config.toml` already contains inline hooks, avoiding Codex's duplicate-source warning. The error explains that v1 requires one untracked, gitignored `.codex/hooks.json`. Harnest never silently moves or duplicates existing inline hooks. The same tracked-file guard applies to Claude's generated local config.

### Merge and ownership

Parse the complete JSON document, preserve unknown top-level keys, events, matchers, and foreign handlers, then atomically replace only Harnest-owned handlers. A handler is Harnest-owned only when both are true:

1. `statusMessage` equals the reserved `Harnest: enforce project rules` marker;
2. its invocation targets `hook evaluate` with a supported Harnest platform and event.

This allows replacement after the executable path changes without a sidecar manifest. A foreign handler which matches only one condition is preserved. Invalid existing JSON aborts generation without writing. Repeated generation produces semantically identical JSON and no duplicate handlers.

Generation first computes and validates every requested artifact, then writes all files. This prevents a later hard-capability error from leaving half-updated configuration. Existing atomic-write support is reused.

## 8. Local recovery switch

Extend `.harnest-local.yaml` only:

```yaml
hooks:
  enabled: false
```

Commands:

```text
harnest local set hooks.enabled false
harnest local set hooks.enabled true
harnest local unset hooks.enabled
```

The field is an optional boolean; omitted means enabled. Disabling does not delete native wiring. The evaluator becomes a fast no-op, while `harnest doctor` reports a prominent degraded-state warning. Environment variables and project rules cannot disable hooks.

## 9. CLI and integration changes

No standalone installer, daemon, or hook doctor is added.

- `harnest generate`: validate capabilities, merge native hook configs, update gitignore.
- `harnest generate --dry-run`: show every instruction and hook artifact without writing.
- `harnest hook evaluate ...`: internal native entrypoint; still documented for troubleshooting.
- `harnest doctor`: verify config presence, ownership, executable path, local switch, target capability, check approval, and native trust state where it can be observed.
- `harnest verify --changed`: reuse the same evaluator primitives used at Stop.
- `harnest local set/unset/show`: support `hooks.enabled`.

Current generator APIs assume one output file per adapter. Replace that assumption with a list of generated artifacts so `CLAUDE.md`/`AGENTS.md` and native hook JSON participate in the same validation, dry-run, write, and gitignore flow. Do not add a second parallel generation pipeline.

Capabilities must be enforcement-specific. A single generic `Verification` flag cannot represent native `protect-path`, Stop-only `require-check`, and unsupported hard checks.

## 10. Migration of `workflow.verify_changed`

`workflow.verify_changed` becomes deprecated. `harnest migrate` performs one atomic migration:

1. Require an approved check with ID `verify-changed` in the configured checks root.
2. Create `.harnest/rules/verify-changed.yaml` as a `required` `require-check` rule referencing that check.
3. Remove `workflow.verify_changed` from `harnest.yaml`.
4. Preserve backups using existing migration behavior.

If the approved check is absent, migration stops before writing and prints the exact check file required. Harnest must not generate a recursive check that invokes `harnest verify --changed`, nor guess a project test command. Deep bootstrap may propose and create the project-appropriate approved check after user confirmation.

## 11. Security and operational limits

- Hook config and check changes are trusted repository code. Claude workspace trust and Codex hook-hash review remain mandatory platform gates.
- Check execution keeps exact argv and project cwd. v1 must add a timeout, bounded captured output, and process-tree termination before enabling native Stop execution.
- Resolve the Harnest executable absolutely; do not trust a repository-controlled `PATH` entry.
- Do not parse arbitrary shell commands. Stop verification is the fallback for shell and MCP mutations visible to Git.
- Untracked files must be included in changed-file discovery or protected new files could escape Stop verification.
- Native config deletion, host feature disablement, trust refusal, process startup failure, timeout, and unobservable tools can fail open. `doctor` detects static drift; it cannot prove that every future event will run.
- Strict enforcement belongs in CI/managed hooks/OS sandbox, not this feature.

## 12. Acceptance criteria

1. `harnest generate` on a project targeting both platforms creates/updates both local JSON files, preserves foreign hooks, and is idempotent.
2. Generation performs no write when any hard rule cannot compile for a configured target.
3. Editing a rule changes the next hook decision without regeneration.
4. Claude `Edit`/`Write` and Codex `apply_patch` are denied before execution for matching hard protected paths.
5. Multi-file patches, moves, deletes, relative paths, subdirectory sessions, and worktrees resolve correctly.
6. Shell or MCP edits missed by PreToolUse are reported by Stop when visible in Git.
7. Required checks run once on every Stop and a failure continues the agent with rule/check IDs.
8. Hard rules cannot be bypassed with `harnest verify --allow`.
9. `hooks.enabled: false` makes the evaluator pass while `harnest doctor` reports degraded enforcement.
10. Tracked Codex hook configuration is never silently modified.
11. Malformed JSON/YAML, path traversal, symlink escape, invalid globs, unapproved checks, timeout, and missing executable have deterministic tests.
12. `harnest generate --dry-run` previews all new artifacts and writes nothing.

## 13. Minimal implementation sequence

1. Refactor path matching and changed-file/check evaluation into shared internal functions; retain current CLI behavior except hard `--allow` rejection.
2. Add normalized event parsing and policy evaluation for PreToolUse and Stop.
3. Add vendor JSON encoders and the `harnest hook evaluate` command.
4. Add JSON merge/install support to existing adapter generation and dry-run flow.
5. Add local `hooks.enabled`, capability checks, and doctor diagnostics.
6. Add `workflow.verify_changed` migration.
7. Add table-driven unit tests and one end-to-end fixture per platform; run `harnest verify --changed`.

No new hook framework or parser dependency is required. Go standard library JSON, path, process, and filesystem packages plus existing YAML support cover the policy and configuration path; process-tree termination may use small OS-specific implementations.
