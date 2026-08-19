# pg_mgr CLI Refactor: Remaining Work

Status: Work in progress

Last reviewed: 2026-08-19
Specifications: [CLI Interaction Design](cli_interaction_design.md), [ADR 0001](adr/0001-automation-first-progressive-cli.md)

## Purpose

This is the handoff checklist for unfinished CLI interaction work. The shared runtime, renderer, categorized errors, secret sources, redacted retry commands, durable operation stages, TTY detection, and root-only process exit are already in place. The following items remain before the design can be considered fully implemented.

## Priority 0: Delivery integrity

### Track ADR 0001

The repository-level `.gitignore` contains a bare `adr` rule, which ignores `apps/pg_mgr/docs/adr/0001-automation-first-progressive-cli.md` even though other documents link to it.

Required outcome:

- Narrow or remove the ignore rule without disturbing unrelated ignore behavior.
- Confirm the ADR is tracked and its links resolve.

## Priority 1: Output and automation

### Complete JSON success responses

Known affected areas include `cmd/modify.go`, `cmd/init.go`, `cmd/completion.go`, `cmd/sync.go`, backup listing, and any other leaf command that still prints human-oriented output directly.

Required outcome:

- JSON success writes exactly one valid document to stdout.
- JSON failure writes exactly one categorized error document to stderr and nothing to stdout.
- JSON mode never prompts and never emits color, tables, or dynamic progress.
- Business functions return errors to the root renderer.
- Human progress and warnings go to stderr so stdout remains pipe-safe.

### Finish non-terminal durable events

- Replace remaining ad hoc long-operation output with newline-delimited operation stages.
- `--quiet` may hide nonessential success presentation, never warnings or recovery guidance.
- `--verbose` may display external commands and diagnostics only after redaction.

## Priority 1: Review-before-execution

Add the shared editable review flow to these multi-field state-changing operations:

- global initialization
- instance modification
- upgrade
- adoption
- backup initialization and modification
- archive configuration

Required behavior:

- Interactive mode shows all resolved values before side effects and allows start, field editing, or cancel.
- Closed choices use the shared numbered menu, include `0. cancel`, show a default, and retry invalid input.
- Non-interactive mode never reviews or reads stdin; missing flags produce exit status 2.
- Secrets show only set/unset state and source category.
- Changing a target or impact invalidates confirmation.

### Derived values in create/deploy

- Mark automatically selected versions and derived data directories as automatic.
- When instance name or version changes, recompute and disclose dependent values.
- Rerun permission preflight after edits to the OS user or another permission-sensitive target.

## Priority 1: Safety and confirmation ordering

### Backup deletion

Current backup deletion uses an ad hoc free-text `1/2` prompt and does not always display the complete impact before confirmation.

Required outcome:

- Show instance, catalog path, affected backup sets, and file retention/deletion before confirmation.
- Use the shared default-no numbered menu with `0. cancel` and invalid-input retry.
- Require explicit target plus `--yes` non-interactively.
- Perform no irreversible action before confirmation.

### Archive enable/disable

Archive configuration may be written before the restart decision is confirmed.

Required outcome:

- Resolve and confirm configuration, migration, and restart impact before mutation.
- If target or impact changes, obtain a new confirmation.
- Report partial success if configuration succeeds but reload or restart fails.

### Upgrade version selection

Replace the manually printed table and free-text index with the shared numbered menu. Prompts belong on stderr; headings and choices must be localized; invalid input must retry.

## Priority 2: Partial failure and recovery

`interaction.Operation` supports completed, failed, rolled-back, skipped, retained, and recovery information, but most workflows do not populate retained resources or recovery commands.

Apply it to create/deploy, package installation, adoption, upgrade/rollback, archive migration, and backup initialization/migration.

Required outcome:

- Report the failed and completed stages.
- Identify retained users, directories, packages, registry entries, and configuration files.
- Report rollback success or failure.
- Provide localized, copyable, redacted recovery commands where possible.
- Never expose secrets in stages, JSON, logs, diagnostics, or retry commands.

## Priority 2: Shared cleanup

### Automation guards

- Consolidate repeated archive target resolution, missing-input checks, and `--yes` enforcement.
- Centralize the one-release legacy `--silent` compatibility behavior.
- Keep `--silent` equivalent to `--non-interactive`; it must never imply `--yes`.

### Localization audit

Audit all user-visible strings under `cmd/` and `internal/`. Add English and Chinese entries to `internal/i18n/i18n.go`; keep command names, flags, enum values, JSON fields, and error codes as stable English identifiers.

```sh
rg -n 'fmt\.(Print|Printf|Println|Fprint|Fprintf|Fprintln).*"[A-Za-z]|Sprint[f]?\("[A-Za-z]' cmd internal --glob '*.go' --glob '!**/*_test.go'
```

## Priority 3: Acceptance-criteria audit

Verify and test:

- Ctrl+C gives a localized cancellation and exit status 130.
- EOF cancels interactive workflows safely.
- Narrow terminals use record-style details without truncating important values.
- All yes/no, instance, version, mode, and edit-field choices use the shared menu.
- Permission checks precede business prompts and side effects.
- Permission preflight reruns after a review edit changes identity or target.
- Color is semantic and disabled for non-terminals, `NO_COLOR`, and `--color never`.

## Verification

Run from `apps/pg_mgr`:

```sh
GOCACHE=/tmp/pg_mgr-go-cache go test ./...
GOCACHE=/tmp/pg_mgr-go-cache go test -race ./...
GOCACHE=/tmp/pg_mgr-go-cache go vet ./...
git diff --check
rg -n 'os\.Exit' cmd internal --glob '*.go'
```

Expected baseline:

- Tests and vet pass; `git diff --check` is clean.
- No `os.Exit` exists below the process root.
- No secret appears in snapshots, verbose output, JSON, or retry commands.

Before declaring completion, repeat both reviews:

1. Standards: `apps/pg_mgr/AGENTS.md`, localization, explicit errors, and duplication.
2. Spec: all interaction-design and ADR acceptance criteria across TTY, piped, quiet, verbose, and JSON modes.

## Change-safety notes

- Preserve user-owned and pre-existing staged changes; do not reset or discard them.
- Do not create further commits unless the repository owner explicitly requests one.
- Prefer focused patches and command-level tests.
