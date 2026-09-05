# Target-specific agent overrides must preserve ownership boundaries

**Date:** 2026-09-05
**Area:** Harnest CLI initialization and YAML agent resolution
**Root Cause:** Multi-target support introduced overlay and buffered-input lifetimes without making their ownership rules explicit.

## What happened

Target-specific agent assignments were generated correctly, but later shared `agents set` commands could be hidden by adapter overrides. Executing agents with the same scope accumulated instead of replacing each other, and piped interactive input failed when the second target wizard started.

## Why it happened — Root Cause Analysis

The configuration has two source layers: shared `agents` and target-specific `adapters.<target>.agents`. Effective configuration is derived by merging them, so mutations must update the source layer selected by the user; changing only the shared layer cannot affect a key already owned by an adapter override.

Each overlay collection also needs a stable identity key. Consilium and model entries use role names, while executing agents use file scope. Treating an executing entry as `(agent, scope)` made a changed agent look like a new entry rather than a replacement.

The wizard issue had a separate ownership boundary: `bufio.Reader` may read ahead. Creating one reader per target discarded bytes already buffered by the first target. The reader lifetime needed to cover the complete multi-target session.

## What I probably didn't know

You may not have known that updating a merged configuration is different from updating the layer that produced it. A common misconception is that changing a shared value changes every effective value; an explicit child override continues to win.

You may also not have known that buffered readers own unread prefetched bytes. Replacing a reader while retaining only its underlying stream can lose data even though the application consumed fewer logical lines.

## The fundamental knowledge

Layered configuration needs three explicit contracts:

- source ownership: which layer a command edits;
- entry identity: which field determines replacement;
- precedence: which layer wins on an identity collision.

For Harnest, target-specific mutation is surgical:

```yaml
agents:
  consilium:
    security: shared-security
adapters:
  codex:
    agents:
      consilium:
        security: codex-security
```

`harnest agents set security new-agent --harness codex` must edit only the Codex adapter. A command without a target is ambiguous while that override exists, so failing before writing is safer than reporting a hidden success.

Executing-agent identity is its scope. When an adapter supplies `**/*.go`, it replaces the shared owner of `**/*.go` while unrelated shared scopes remain inherited.

Buffered input follows the same lifetime rule: create one `bufio.Reader` for one complete interactive session and pass it through every target step.

## Key takeaway — Prevention checklist

- Define an identity key for every collection merged across configuration layers.
- Mutate the requested source layer, not a computed effective configuration.
- Reject ambiguous global updates before saving any file.
- Keep one buffered reader alive for the entire logical input session.
- Add regression tests covering inheritance, override, and multi-step streamed input.

## Related concepts to study

- Layered configuration and cascading precedence
- Copy-on-write versus source-layer mutation
- Buffered I/O read-ahead and stream ownership
