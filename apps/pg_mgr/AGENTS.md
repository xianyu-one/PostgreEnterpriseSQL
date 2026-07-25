# Repository Guidelines

## Project Structure & Module Organization

This directory is the `pg_mgr` Go module, a Linux-focused PostgreSQL management CLI.

- `main.go` starts the application.
- `cmd/` contains Cobra commands and command-level tests. Add new CLI behavior to the closest command file, such as `cmd/backup.go`.
- `internal/config/`, `internal/i18n/`, `internal/process/`, `internal/logger/`, and `internal/utils/` contain reusable implementation packages.
- `docs/` holds user and design documentation, including CLI naming and permission conventions.
- Tests live beside production code as `*_test.go`; `test/` is reserved for additional test material.

Keep package boundaries narrow. Code under `internal/` must not be imported by external modules.

## Build, Test, and Development Commands

- `go build -o pg_mgr .` builds the local CLI binary.
- `go run . --help` runs the development entry point and lists commands.
- `go test ./...` runs the complete test suite.
- `go test ./cmd -run TestName` runs one targeted command test.
- `go test -race ./...` checks tests for data races.
- `gofmt -w path/to/file.go` formats changed Go files before review.
- `go vet ./...` performs standard static analysis.

The tool only runs on Linux. Commands that manage installations or services may require root privileges and can touch system configuration, so use disposable test hosts for integration checks.

## Coding Style & Naming Conventions

Follow idiomatic Go and let `gofmt` determine tabs and layout. Use exported `PascalCase` names, unexported `camelCase` names, and concise lowercase package names. Cobra command names should follow `docs/cli_naming_convention.md`; prefer noun-led command groups such as `instance` and `pkg`. Add user-facing text to `internal/i18n/i18n.go` in both English and Chinese rather than embedding messages in command logic. Handle errors explicitly and avoid silently changing global OS settings.

## Testing Guidelines

Use Go's standard `testing` package. Name tests `TestBehavior` and table-driven cases descriptively. Cover parsing and utility logic with unit tests, and add regression tests for command behavior when fixing defects. No fixed coverage threshold is enforced; prioritize meaningful success, failure, permission, and filesystem edge cases.

## Commit & Pull Request Guidelines

Recent history follows Conventional Commits: `feat(backup): ...`, `fix(archive): ...`, and `refactor(cli): ...`. Keep each commit focused and use an imperative, specific summary.

Pull requests should explain the motivation, behavior change, and validation commands. Link relevant issues, call out root/systemd or configuration impact, and include terminal output or screenshots when CLI presentation changes. Update documentation and both translations with user-visible changes.
