# pg_mgr CLI Refactor: Remaining Work

Status: Work in progress  
Last reviewed: 2026-08-19  
Governing specification: [CLI Interaction Design](cli_interaction_design.md) and [ADR 0001](adr/0001-automation-first-progressive-cli.md)

## Purpose

This document is the handoff checklist for the unfinished CLI interaction refactor. It records gaps found by the final Standards and Spec reviews so later work can continue without repeating the repository audit.

The current implementation has already established the shared runtime, renderer, categorized errors, secret sources, redacted retry commands, durable operation stages, TTY detection, and root-only process exit. The items below describe what is still incomplete; passing tests alone does not mean the interaction contract is complete.

## Priority 0: Delivery integrity

### Make ADR 0001 trackable

Current state:

- The repository-level `.gitignore` contains a bare `adr` rule.
- That rule ignores `apps/pg_mgr/docs/adr/0001-automation-first-progressive-cli.md`.
- `docs/README.md` and the interaction design link to the ignored ADR, so a future commit can contain broken documentation links.

Required outcome:

- Narrow or remove the ignore rule without disturbing unrelated user-owned ignore changes.
- Confirm the ADR appears in `git status --short --untracked-files=all`.
- Do not commit as part of this refactor unless the repository owner explicitly requests it.

## Priority 1: Output and automation contract

### Complete JSON success responses

Affected areas include:

- `cmd/modify.go`
- `cmd/init.go`
- `cmd/completion.go`
- `cmd/sync.go`
- backup listing and any remaining leaf commands that print tables or success messages directly

Current state:

- Core commands such as create, deploy, package operations, remove, service, daemon, archive, upgrade, and adopt have partial or complete structured success output.
- Several remaining commands still print human-oriented text directly to stdout when `--output json` is selected.
- Some business functions print errors themselves instead of returning them to the root renderer.

Required outcome:

- A successful JSON invocation writes exactly one valid JSON document to stdout.
- An unsuccessful JSON invocation writes exactly one categorized JSON error to stderr.
- Prompts, colors, tables, and dynamic progress never appear in JSON mode.
- Business functions return errors; only the root boundary renders them.
- Human progress and warnings use stderr, leaving stdout safe for requested data.

Suggested tests:

- Execute every leaf command with `--output json` and decode stdout with `encoding/json`.
- Assert stderr is empty on success unless the command intentionally emits durable warnings.
- Assert an error produces no stdout and exactly one decodable stderr document.
- Repeat representative commands with piped stdin and stderr to ensure they never read stdin.

### Finish non-terminal durable events

Current state:

- Shared `interaction.Operation` stages replace dynamic progress in major long-running commands.
- Some remaining workflows still emit ad hoc status lines or only a terminal result.

Required outcome:

- Non-TTY human output uses durable, newline-delimited stage events.
- `--quiet` suppresses nonessential success presentation but never warnings, failures, or recovery guidance.
- `--verbose` displays external commands and diagnostics only after redaction.

## Priority 1: Review-before-execution coverage

Add the shared editable review flow to every multi-field state-changing command that still lacks it:

- global `init`
- instance `modify`
- `upgrade`
- `adopt`
- backup initialization and modification
- archive configuration (`archive set` and related mode changes)

Required behavior:

- Interactive mode shows the final resolved values before side effects.
- The user can start, edit a specific field, or cancel.
- Closed choices use the shared numbered menu, include `0. cancel`, retry invalid input, and show their default.
- Non-interactive mode never shows a review and fails with exit status 2 when required flags are absent.
- Secrets display only set/unset state and source category.
- A target or impact change invalidates the previous confirmation.

### Correct create/deploy derived-field presentation

Current state:

- Create and deploy have editable review screens.
- Automatically selected version and derived data directory are not consistently marked as automatic.
- Editing instance name or version does not clearly disclose dependent-value changes before reconfirmation.

Required outcome:

- Mark safely derived values with `Automatic` in the review model.
- Recompute dependent values explicitly and show which fields changed.
- Re-run permission preflight if review edits change the OS identity or another permission-sensitive target.

## Priority 1: Safety and confirmation ordering

### Backup destructive operations

Current state:

- Backup uninitialization/deletion uses an ad hoc free-text `1/2` prompt.
- The menu lacks the standard `0` cancellation item and does not retry invalid choices.
- The complete impact is not always known and displayed before confirmation.

Required outcome:

- Before confirmation, show the instance, backup catalog path, affected backup sets, and whether files are retained or deleted.
- Use the shared numbered menu with a default-no destructive choice.
- Require an explicit target and `--yes` in non-interactive mode.
- Perform no deletion or irreversible mutation before confirmation.

### Archive enable/disable ordering

Current state:

- Archive configuration can be written before asking whether PostgreSQL should restart.
- Restart questions still use command-local confirmation paths.

Required outcome:

- Resolve and display the full impact, including restart behavior, before changing configuration.
- Confirm once through the shared menu, then perform mutations.
- If the archive target, migration choice, or restart impact changes, obtain a new confirmation.
- Return partial-failure information if configuration succeeds but reload/restart fails.

### Upgrade target selection

Current state:

- Interactive version selection still uses a manually printed table and free-text index parsing.
- Invalid input can terminate instead of retrying.

Required outcome:

- Use the shared numbered menu for available versions.
- Include `0. cancel`, display the default, and retry invalid input.
- Send prompts and review output to stderr.
- Localize headings and choices through `internal/i18n/i18n.go`.

## Priority 2: Partial failure and recovery

Current state:

- `interaction.Operation` supports completed, failed, rolled-back, skipped, retained, and recovery information.
- Most production workflows do not yet populate retained resources or recovery commands when only part of an operation succeeds.

Apply this to at least:

- create and deploy
- package installation
- instance adoption
- upgrade and rollback
- archive configuration and migration
- backup initialization and migration

Required outcome:

- Report the failed stage and all completed stages.
- Identify resources retained after failure, such as users, directories, installed packages, registry entries, or configuration files.
- Report rollback success or failure explicitly.
- Provide a localized, copyable, redacted recovery or resume command where possible.
- Never print secret values in stages, JSON, logs, diagnostics, or retry commands.

## Priority 2: Shared interaction cleanup

### Consolidate repeated automation guards

Known duplication:

- Archive enable/set repeat target resolution, non-interactive missing-input checks, and `--yes` enforcement.
- Legacy `--silent` default-instance compatibility is distributed across create, deploy, remove, and upgrade paths.

Required outcome:

- Extract a shared archive target/confirmation guard so related actions cannot drift.
- Centralize the one-release `--silent` compatibility policy, including its deprecated warning and legacy default behavior.
- Keep `--silent` equivalent to `--non-interactive`; it must never imply `--yes`.

### Finish localization audit

Current state:

- New shared interaction text and the most visible flags/stages have English and Chinese catalog entries.
- Older command-local prompts, migration errors, table labels, and help strings may still bypass the catalog.

Required outcome:

- Audit every user-visible string under `cmd/` and `internal/`.
- Add both English and Chinese entries to `internal/i18n/i18n.go`.
- Keep command names, flags, enum values, JSON fields, and error codes as stable English identifiers.
- Ensure one invocation never mixes translated UI languages.

Useful audit command:

```sh
rg -n 'fmt\.(Print|Printf|Println|Fprint|Fprintf|Fprintln).*"[A-Za-z]|Sprint[f]?\("[A-Za-z]' cmd internal --glob '*.go' --glob '!**/*_test.go'
```

## Priority 3: Remaining acceptance criteria

These design requirements still need an explicit implementation audit and tests:

- Ctrl+C produces a localized cancellation message and exit status 130.
- EOF cancels interactive workflows safely.
- Narrow terminals render record-style details without truncating important values.
- All yes/no, instance, version, mode, and edit-field choices use the shared menu protocol.
- Permission checks always occur before business prompts and side effects.
- A review edit that changes OS user or target reruns permission preflight.
- Colors are semantic, are disabled for non-terminals/`NO_COLOR`/`--color never`, and are not applied to ordinary values.

## Verification checklist

Run from `apps/pg_mgr` using a writable Go cache when necessary:

```sh
GOCACHE=/tmp/pg_mgr-go-cache go test ./...
GOCACHE=/tmp/pg_mgr-go-cache go test -race ./...
GOCACHE=/tmp/pg_mgr-go-cache go vet ./...
git diff --check
rg -n 'os\.Exit' cmd internal --glob '*.go'
```

Expected baseline:

- Tests and vet pass.
- `git diff --check` produces no output.
- No `os.Exit` exists below the process root; `main` is the only process-exit boundary.
- No secrets appear in test snapshots, verbose output, JSON errors, or retry commands.

Before declaring the refactor complete, perform both reviews again:

1. **Standards review:** check `apps/pg_mgr/AGENTS.md`, localization, explicit error handling, and duplication.
2. **Spec review:** check every acceptance criterion in the interaction design and ADR against real command behavior, including TTY, piped, quiet, verbose, and JSON modes.

## Change-safety notes

- The working tree contains user-owned and previously staged changes. Do not reset, discard, restage, or rewrite them while completing this checklist.
- Do not create a commit unless the repository owner explicitly requests one.
- Prefer focused patches and command-level tests so unrelated CLI behavior remains stable.
