---
name: architecture-context-builder
description: Build and maintain task-scoped architecture context for any repository. Use when initializing or refreshing `docs/architecture/`, mapping an unfamiliar or changed codebase, documenting architecture and dependencies, interviewing about infrastructure that code cannot prove, or adding safe architecture-context routing to root `AGENTS.md` and `CLAUDE.md`.
---

# Architecture Context Builder

Build evidence-backed, task-loadable architecture documentation. Keep root agent
instructions short; put detail in `docs/architecture/`.

## Required architecture-file language

Write all human-readable content created or updated under `docs/architecture/`
in the user's language. This requirement applies to architecture documents,
operator-confirmed notes, and human-readable values in `.context-state.json`.
If existing architecture files use another language, translate them during
bootstrap or refresh unless the user explicitly requests otherwise. This rule
does not require translating `SKILL.md`, reports outside `docs/architecture/`,
or root managed routing blocks. Keep file names, paths, code symbols, commands,
and machine-readable keys/enums unchanged.

## Select a mode

- **Bootstrap**: use when `docs/architecture/INDEX.md` is absent or the user asks
  to map/onboard/document a repository for the first time.
- **Refresh**: use when the user asks to update the architecture context, after a
  substantial architecture change, or when the last scan is stale.
- **Targeted refresh**: use when the user names a subsystem; update its documents
  plus only the cross-references and overview that are actually affected.

## Use the ECC references

The files in `references/ecc/` are local snapshots, not required dependencies.
They are licensed source material for this workflow; see `references/ecc/SOURCES.md`.

1. For repository reconnaissance, read `codebase-onboarding.SKILL.md`.
2. For the documentation scope and quality bar, read `doc-updater.md`.
3. For refresh diff rules, freshness metadata, and the 30% confirmation threshold,
   read `update-codemaps.md`.

If the corresponding ECC skill, agent, or command is installed in the active
harness, it may perform its native step. Otherwise perform the documented steps
directly. Never make ECC installation a prerequisite.

## Bootstrap workflow

1. Read root and applicable nested `AGENTS.md` / `CLAUDE.md`, existing architecture
   docs, READMEs, contribution guides, and project manifests. Preserve project rules.
2. Reconnoitre before deep reading: identify repository shape, workspaces, source
   roots, manifests, configurations, CI, deployment definitions, entry points,
   tests, APIs, data stores, queues/workers, and third-party integrations. Exclude
   generated and dependency directories unless they are the source of truth.
3. Verify ambiguous claims from the implementation. Trace at least one representative
   request or event lifecycle from ingress to storage or external effect.
4. Create `docs/architecture/INDEX.md`, `system-overview.md`, `codebase-map.md`,
   `runtime-and-entrypoints.md`, `data-and-state.md`, `dependency-map.md`, and
   `local-development.md`. Create domain documents only when applicable:
   `frontend.md`, `backend.md`, `background-jobs.md`, `integrations.md`,
   `infrastructure-and-deployment.md`, `security-and-auth.md`, and `decisions/`.
5. In every document, record verified paths, component boundaries, key dependencies,
   data/control flow, and links to related documents. Add a freshness header with
   scan date and files scanned. Prefer concise tables and ASCII diagrams to prose.
6. For facts the repository cannot prove, run `/grill-me` when it is available;
   otherwise ask concise questions one at a time. Ask only about material unknowns:
   cloud/account boundaries, runtime and networking, secrets, queues, observability,
   production deploy/rollback, managed data services, and ownership. Attribute each
   answer as operator-confirmed rather than code-derived.
7. Save `.context-state.json` in `docs/architecture/` with scan date, revision when
   available, files/manifests scanned, discovered domains, and document inventory.
   Write an analysis report under `.reports/architecture-context/`.
8. Add or update only the managed routing block in each existing root `AGENTS.md` and
   `CLAUDE.md`. Do not overwrite user instructions and do not create a missing root
   instruction file without the user's approval. Use these stable markers:

   ```markdown
   <!-- architecture-context-builder:start -->
   ## Architecture context
   Start with `docs/architecture/INDEX.md`; load only documents relevant to the task.
   Do not load the entire architecture directory by default.
   <!-- architecture-context-builder:end -->
   ```

9. Verify every documented path and Markdown link. Report unknowns and documents that
   were intentionally omitted rather than filling gaps with guesses.

## Refresh workflow

1. Read the prior index, state file, latest report, and managed routing blocks.
2. Compare the repository with the recorded revision and inventory. Examine changed
   manifests, source roots, entry points, CI/deployment config, schema/migrations,
   API contracts, worker definitions, and integration configuration.
3. Map changed files to architecture documents. Update only affected documents,
   their cross-references, `INDEX.md`, state, freshness metadata, and a dated report.
4. If the proposed codemap content changes by more than 30%, show the meaningful diff
   and ask before overwriting it. Otherwise update it in place.
5. Re-run the infrastructure interview only when a verified gap appears or a changed
   artifact could alter an operator-confirmed fact. Never ask the same question if a
   still-current answer is already recorded.
6. Re-validate paths, links, freshness headers, and root routing. State exactly what
   changed, what was not rechecked, and any remaining unknowns.

## Non-negotiable rules

- Derive architecture from source of truth; never infer production infrastructure
  solely from package names or environment-variable names.
- Do not copy the full architecture into `AGENTS.md` or `CLAUDE.md`.
- Do not enumerate every dependency. Document dependencies that affect boundaries,
  runtime behaviour, security, deployment, or a task's likely change surface.
- Keep documents task-loadable and cross-linked. Cite a concrete file, config, test,
  or operator-confirmed answer for every non-obvious claim.
- Preserve user-owned docs and text. Use the managed block only for agent routing.
- The required output language applies to every human-readable artifact under
  `docs/architecture/`; technical identifiers remain unchanged.
