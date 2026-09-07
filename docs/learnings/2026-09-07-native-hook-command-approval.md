# Native hook invocation and check approval

**Date:** 2026-09-07
**Area:** Native hook generation and Stop evaluation
**Root Cause:** Approval was a mutable boolean rather than a binding to the reviewed executable definition and source.

## What happened

Claude hooks used executable-plus-args configuration, which requires host support for exec form. More critically, Stop reloaded checks with `approved: true` and executed changed commands or scripts without invalidating the existing native binding.

## Why it happened — Root Cause Analysis

The approval flag described an earlier decision but did not identify what that decision covered. The host trusted a stable evaluator command while the evaluator read changing executable policy from the checkout. A YAML edit could therefore preserve the flag while replacing the approved behavior.

## What I probably didn't know

A common misconception is that trusting a launcher also approves whatever it later loads. Approval must bind the reviewed definition and executable dependencies to a value retained at the trust boundary; a digest next to the mutable YAML alone can be replaced with that YAML.

## The fundamental knowledge

Native Stop commands now carry a SHA-256 digest of selected check definitions, canonical cwd, resolved executables and file contents. Evaluation compares the current inputs with that digest before each execution and retains the compared definitions. Regeneration changes the native command and requires the host's trust/reload workflow.

Executable files and existing file arguments are included automatically. Indirect script dependencies require explicit `sources`; this is not a shell or import dependency analyzer. File arguments are treated conservatively as inputs.

Claude generation uses a fully quoted shell command, with recognition of existing exec-form entries for migration. Current [Claude documentation](https://code.claude.com/docs/en/hooks#command-hook-fields) supports both forms, so lack of exec-form support must not be claimed for every version.

## Key takeaway — Prevention checklist

- Bind approval to definition and source content, not only a boolean.
- Keep the expected digest in the host-trusted command.
- Reject missing or changed approval before spawning a process.
- Declare indirect executable dependencies and review them before regeneration.
- Test the rendered command through a shell and assert rejected checks leave no execution marker.

## Related concepts to study

- Trust boundaries and transitive executable dependencies
- Content hashing and time-of-check/time-of-use limits
- Shell quoting versus direct argument vectors
