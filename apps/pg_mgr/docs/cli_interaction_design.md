# pg_mgr CLI Interaction Design

Status: Accepted

Date: 2026-08-19
Decision record: [ADR 0001](adr/0001-automation-first-progressive-cli.md)

## Purpose

This document defines the user-interaction contract for `pg_mgr`. It applies to every command, prompt, result, warning, error, progress display, and machine-readable response. The goal is a predictable operations CLI that is efficient in automation and approachable in an interactive terminal.

The governing style is **automation-first progressive interaction**: a complete invocation executes deterministically, while an incomplete leaf command may collect missing information only from an interactive terminal.

## Current implementation findings

The existing implementation already has useful shared primitives, including numbered selection, hidden password entry, path completion, translated messages, tables, and progress stages. The inconsistency comes from commands composing or bypassing those primitives differently.

Observed gaps include:

- `instance create` enters a wizard by default, while `instance modify` rejects an invocation with no change flags instead of offering an interactive editing flow.
- Service and archive leaf commands can prompt for an instance, while other commands silently use defaults or exit.
- `archive` performs an implicit status action without a subcommand, unlike resource groups that act as containers.
- `--silent` is used across deploy, create, package install, remove, archive, and upgrade, but does not clearly distinguish “do not prompt” from “approve the operation.”
- The current create defaults include an implicit instance name and a fixed initial password. Those values can turn missing security-sensitive intent into an apparently complete invocation.
- Closed choices are implemented through both shared numbered menus and ad hoc free-text parsing. Some invalid choices retry, while others terminate.
- User-visible output mixes translated text, hard-coded English, and bilingual strings such as the sync choice prompt.
- Commands print directly from business logic and use `os.Exit` broadly. A repository scan found 84 direct exits under `cmd` and `internal`, making error rendering and exit semantics command-dependent.
- Normal results, prompts, warnings, progress, and errors are not consistently separated between stdout and stderr.
- Destructive flows confirm at different points and do not share a standard impact summary.
- Long operations use progress writers in some flows and plain messages in others; partial completion and recovery guidance are not consistently reported.
- Permission handling varies between immediate rejection and an in-process sudo/`su` selection.
- Wide instance tables do not define a narrow-terminal fallback, and colors are applied to values as well as semantic states.

These are design-system problems rather than isolated copy defects. Fixes should therefore begin with shared interaction infrastructure, then migrate commands onto it.

## Interaction modes

### Interactive terminal

Interactive mode is available only when stdin and stderr are terminals, output mode is human-readable, and `--non-interactive` is absent.

A leaf command may prompt only for missing required input. Values already supplied by flags are retained and shown in the review summary; they are not asked again.

### Non-interactive execution

Execution is non-interactive when any of the following is true:

- stdin or stderr is not a terminal;
- `--non-interactive` is present;
- `--output json` is present.

In this mode the command must never read from stdin unless a flag explicitly defines stdin as a data or secret source. Missing required inputs produce exit status `2` and name the missing flags.

### Resource groups and leaf commands

Resource groups such as `instance`, `pkg`, `backup`, `archive`, `daemon`, and `completion` display concise help and examples when invoked without a subcommand. They never run an implicit default action.

Only leaf commands may progressively collect missing input. For example, interactive `pg_mgr archive show` may offer an instance menu, while plain `pg_mgr archive` displays help.

## Global options

All commands use the same meanings:

| Option | Meaning |
|---|---|
| `--non-interactive` | Never prompt; fail when required information or confirmation is missing. |
| `--yes` | Pre-approve guarded operations; it does not supply missing targets or other inputs. |
| `--output table\|json` | Select human-readable or machine-readable output. Default: `table`. |
| `--lang zh-CN\|en` | Override locale-based UI language selection. |
| `--color auto\|always\|never` | Control semantic terminal color. Default: `auto`. |
| `--quiet` | Suppress nonessential success presentation, never warnings or errors. |
| `--verbose` | Show underlying commands and full external diagnostics after redaction. |

`--silent` remains for one release as a deprecated alias of `--non-interactive`. It never implies `--yes`. Help displays the new option and omits deprecated aliases from primary examples.

## Inputs and defaults

### Default-value classes

Inputs are classified before a command is implemented:

1. **Explicit identity or secret** — must be supplied in non-interactive execution. Examples: instance name and initial database password. There is no fixed default password.
2. **Safely derived** — may be computed and marked as automatic. Examples: newest compatible installed version and a data directory derived from the instance name.
3. **Routine validated default** — may use a documented default after validation. Examples: port, OS user, and database user.

TTY interaction prompts for missing class 1 values. Non-interactive execution reports their corresponding flags or secret-input mechanisms.

Secrets should support mechanisms that avoid process listings and shell history, such as terminal-hidden input, an environment variable, a file descriptor, or a protected file. Secret values must never appear in review screens, logs, JSON, verbose diagnostics, or retry commands.

### Closed choices

Every closed set uses a numbered menu, including yes/no questions, instances, versions, modes, and fields to edit.

```text
请选择版本：
  1. PostgreSQL 16.14
  2. PostgreSQL 17.10
  0. 取消
请选择 [2]:
```

Rules:

- The displayed default appears in brackets and Enter accepts it.
- `0` cancels a numbered menu.
- Invalid input explains the valid range and repeats the same menu.
- Items have stable meaning within the displayed menu; numbering itself is not a machine interface.
- Yes/no menus use `1. 是`, `2. 否`, and `0. 取消`. A destructive choice defaults to no.

### Free-text and path input

- Empty input accepts a displayed default; if no default exists it fails validation and repeats.
- Validation occurs immediately and explains the expected format.
- `0` and `b` remain ordinary values in free-text fields.
- Paths use completion in a terminal and are displayed in normalized form before execution.
- A password is entered twice when initially set. Returning to that step offers a numbered choice to retain or replace it.

### Wizard navigation and dependencies

Multi-step wizards accept `b` to return one step. The final review screen is the primary editing surface and lets users select a specific field instead of restarting the wizard.

When changing one field invalidates a dependent or derived field, the CLI displays the affected fields and asks the user to confirm or revise them. It must not silently change previously reviewed values.

EOF cancels safely. Ctrl+C terminates with status `130` and an explicit cancellation message.

## Review before execution

Multi-field operations that write system state must show a review screen in interactive mode. This includes global initialization, package installation, deployment, instance creation, instance adoption, instance modification, upgrades, backup initialization or modification, and archive configuration.

```text
创建实例
  实例名称：sales-db
  PostgreSQL：17.10（自动选择）
  数据目录：/usr/local/pesql/instances/sales-db（自动生成）
  端口：51721
  OS 用户：postgres
  初始密码：已设置（终端输入）

  1. 开始执行
  2. 修改配置
  3. 取消
请选择 [1]:
```

Selecting “modify” opens a numbered field list. Secrets show only whether they are set and their source category.

Single-action commands such as start, reload, or status do not add a redundant review screen. Complete non-interactive invocations never show one.

## Safety and confirmation

Operations use three impact levels:

| Level | Examples | Required interaction |
|---|---|---|
| Normal | list, show, status, start, reload, validation | No confirmation. |
| Disruptive | stop, restart, archive mode changes, upgrade downtime | Display impact; interactive numbered confirmation when the effect is not already explicit, or `--yes` non-interactively. |
| Destructive | remove instance data, delete backup directory or backup sets | Exact impact summary followed by default-no numbered confirmation, or explicit target plus `--yes` non-interactively. |

A destructive summary lists the resource, paths, services, and whether backups are retained or deleted. Users are not required to retype an identifier; the numbered menu is the confirmation mechanism.

No confirmation may occur after an irreversible step has already begun. A change in the confirmed target or impact invalidates the confirmation.

## Permissions

Required permissions are checked before business prompts and side effects.

On failure, the CLI names the allowed identities and provides a redacted, copyable retry command. It does not ask for, read, or forward sudo or `su` credentials and does not attempt elevation midway through a workflow.

```text
权限不足：部署实例需要 root 权限。

请重新执行：
  sudo pg_mgr deploy --tar … --instance sales-db
```

Machine-readable mode returns the same category and remediation without attempting elevation.

## Language

One invocation uses exactly one UI language:

- `--lang` has highest priority;
- otherwise `LC_ALL`, then `LANG`, selects the supported locale;
- an unsupported locale falls back to English.

All prompts, validation messages, warnings, stages, results, table headings, and help text are localized. A line never presents the same UI text in two languages.

Command names, flags, enum values accepted by flags, JSON field names, and error codes remain stable English identifiers. External PostgreSQL or systemd diagnostics retain their original text beneath a localized explanation.

## Output contract

### Streams

- stdout: successful command results and requested data.
- stderr: prompts, review screens, progress, warnings, errors, and recovery guidance.

This separation applies in both terminal and non-terminal execution so piping stdout remains safe.

### Human-readable output

The default `table` mode is localized. Tables are used for compact collections; narrow terminals switch to record-style details rather than truncating instance names, paths, or diagnostic text.

The visual vocabulary is restrained:

| State | Symbol | Color |
|---|---:|---|
| Success | `✓` | Green |
| Warning | `!` | Yellow |
| Failure | `✗` | Red |
| Guidance | `→` | Cyan |

Every symbol has a textual label. Color is disabled when output is not a terminal, `NO_COLOR` is set, or `--color never` is used. Ordinary values are not colored.

### JSON output

`--output json` disables prompts, color, and dynamic progress. Successful output is one valid JSON document on stdout. Errors are one valid JSON document on stderr.

Minimum error shape:

```json
{
  "code": "permission_denied",
  "message": "Root privileges are required to deploy an instance.",
  "details": {
    "required_identity": "root"
  }
}
```

JSON field names and error codes are English and version-stable. Localized `message` is presentation; automation branches on `code` and structured fields.

YAML output is outside the current scope.

## Errors, cancellation, and exit status

| Status | Meaning |
|---:|---|
| `0` | Success, or explicit menu cancellation before any side effect. |
| `1` | Execution or external-tool failure. |
| `2` | Invalid syntax, missing input, or validation failure. |
| `3` | Insufficient permission. |
| `4` | Target missing or resource conflict. |
| `130` | Interrupted with Ctrl+C. |

Command and business functions return categorized errors. Only the root command renders the final error and maps it to an exit status. A function below the root must not call `os.Exit`.

If a failure or cancellation occurs after side effects, the result is nonzero and must report completed work, failed work, rollback performed, retained state, and recovery steps.

## Long-running operations

Long operations expose meaningful stages with these states: pending, running, completed, failed, rolled back, and skipped. They do not fabricate percentages.

TTY output may update stage lines dynamically. Non-TTY output emits durable line-oriented events. An indeterminate spinner is permitted only when no meaningful stages or progress measure exists.

On failure, dependent stages stop. Safe rollback occurs automatically; data that cannot be safely reconstructed is retained by default.

```text
已完成：
  ✓ 创建数据目录
  ✓ 初始化数据库

失败：
  ✗ 创建 systemd 服务：权限不足

已恢复：
  ✓ 删除未完成的服务文件

仍需处理：
  → 数据目录 /data/postgresql/sales-db 已保留
  → 修复权限后重新执行原命令
```

Successful write operations end with a concise result and at most three actionable next commands. `--verbose` may expose redacted commands and full diagnostics; secrets are redacted at every verbosity.

## Command application matrix

| Command family | Progressive input | Review | Confirmation | Structured output |
|---|---|---|---|---|
| `init` | Missing config values | Yes | If overwriting existing config | Result object |
| `pkg install` | Package path/version | Yes | On overwrite | Result object |
| `deploy` | All missing deployment fields | Yes | On overwrite or destructive replacement | Result object |
| `instance create` | All missing creation fields | Yes | On conflicting replacement | Result object |
| `instance modify` | Instance and fields to edit | Yes | For disruptive/destructive changes | Before/after result |
| `instance remove` | Instance and retention choice | Impact summary | Always, default no | Removal result |
| `instance adopt` / `sync` | Detected target and conflict resolution | Yes | Before registry/system changes | Result object |
| `instance upgrade` | Instance, target version, migration choices | Yes | Fresh managed full backup is mandatory when configured; bypass requires `--skip-backup` plus a second risk acknowledgement | Staged result |
| service controls | Missing instance only | No | Stop/restart when not already explicit in a complete invocation | Status result |
| `backup init` / `modify` | Configuration fields | Yes | Migration or overwrite impact | Configuration result |
| backup removal/delete | Instance, deletion scope/date | Impact summary | Always, default no | Deletion result |
| archive enable/disable/set | Instance and configuration | Yes for `set` | Restart/migration impact | Configuration result |
| `self-update` | Local candidate binary | Impact summary | Always; `--yes` non-interactively | Update and daemon restart result |
| list/show/status/use | Missing instance where applicable | No | No | `table` or `json` |
| resource group without leaf | None | No | No | Help only |

## Implementation boundaries

The interaction contract should be implemented through shared services rather than duplicated in commands:

- **Runtime context**: terminal detection, locale, output mode, color, verbosity, confirmation policy, stdin/stdout/stderr.
- **Prompt service**: numbered menus, validated text/path/secret input, navigation, cancellation, and testable readers/writers.
- **Renderer**: human and JSON result/error rendering with strict stream ownership.
- **Error model**: stable error code, localized message key, details, cause, exit category, and optional remediation.
- **Operation plan**: review fields, impact classification, stages, rollback state, and recovery instructions.
- **Permission policy**: preflight requirement and retry-command generation.

Cobra handlers should parse intent and return errors. Domain operations should not prompt, render, or exit. This separation makes interactive behavior testable without a real terminal and ensures the same operation can serve table and JSON modes.

## Migration plan

### Phase 1 — Foundations

- Introduce runtime context, stream-aware renderer, categorized errors, and terminal detection.
- Add global output, language, color, quiet, verbose, non-interactive, and yes options.
- Replace internal `os.Exit` calls with returned errors, starting at shared permission and prompt utilities.
- Centralize all localized user-visible strings.

### Phase 2 — Shared interaction patterns

- Replace ad hoc choice parsing with the numbered menu service.
- Add review/edit/cancel and impact-summary models.
- Add stage and recovery reporting for long operations.
- Implement secret sources and remove the fixed password default.

### Phase 3 — Command migration

Migrate in risk order:

1. instance removal and backup deletion;
2. deploy, create, modify, and upgrade;
3. adopt, sync, backup, and archive configuration;
4. service, list/show, use, completion, and daemon commands.

Change `archive` without a subcommand from implicit show to resource help during this phase.

### Phase 4 — Compatibility removal

- Keep `--silent` as a warning alias for one release only.
- Never allow it to imply confirmation.
- Retain the implicit `default` instance only for deprecated `--silent` calls during that release.
- Remove `--silent` and the implicit instance fallback in the following release.
- Preserve legacy command aliases but exclude them from primary help examples.

## Acceptance criteria

The interaction redesign is complete when:

- no command or internal business function below the root calls `os.Exit`;
- no user-visible string is hard-coded outside the localization and rendering layers;
- every closed choice uses the shared numbered menu and retries invalid input;
- every destructive operation displays an exact impact summary and defaults to no;
- every multi-field write flow has an editable review screen;
- every command is guaranteed not to prompt in non-interactive or JSON mode;
- stdout contains no prompts, progress, warnings, or errors;
- JSON success and error output is valid, stable, and free of terminal decoration;
- permission failures occur before business prompts or side effects;
- partial failures report retained state and remediation;
- color-disabled, narrow-terminal, EOF, Ctrl+C, and piped-I/O behavior is covered by tests;
- English and Chinese message catalogs contain matching keys;
- secret values do not appear in output, logs, retry commands, or tests.

## Non-goals

- Changing the canonical command naming defined in `cli_naming_convention.md`.
- Adding YAML output.
- Building a full-screen terminal UI.
- Automatically acquiring root privileges.
- Translating command names, flags, JSON keys, or stable error codes.
- Requiring users to retype resource identifiers for destructive confirmation.
