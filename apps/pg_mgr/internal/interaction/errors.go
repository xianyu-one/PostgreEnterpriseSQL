package interaction

import (
	"errors"
	"pg_mgr/internal/i18n"
)

type Code string

const (
	CodeExecutionFailed  Code = "execution_failed"
	CodeInvalidInput     Code = "invalid_input"
	CodeMissingInput     Code = "missing_input"
	CodePermissionDenied Code = "permission_denied"
	CodeTargetNotFound   Code = "target_not_found"
	CodeResourceConflict Code = "resource_conflict"
	CodeInterrupted      Code = "interrupted"
	CodeCancelled        Code = "cancelled"
)

type ExitCategory int

const (
	ExitSuccess     ExitCategory = 0
	ExitExecution   ExitCategory = 1
	ExitUsage       ExitCategory = 2
	ExitPermission  ExitCategory = 3
	ExitTarget      ExitCategory = 4
	ExitInterrupted ExitCategory = 130
)

var ErrCancelled = errors.New("operation cancelled")

type Error struct {
	Code        Code           `json:"code"`
	Message     string         `json:"message"`
	Details     map[string]any `json:"details,omitempty"`
	Remediation string         `json:"remediation,omitempty"`
	Category    ExitCategory   `json:"-"`
	Cause       error          `json:"-"`
}

func NewError(code Code, message string, category ExitCategory) *Error {
	return &Error{Code: code, Message: message, Category: category}
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Cause }

func (e *Error) WithDetail(key string, value any) *Error {
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	e.Details[key] = value
	return e
}

func (e *Error) WithCause(cause error) *Error { e.Cause = cause; return e }

func (e *Error) WithRemediation(remediation string) *Error {
	e.Remediation = remediation
	return e
}

func ExitCode(err error) int {
	if err == nil || errors.Is(err, ErrCancelled) {
		return 0
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return int(appErr.Category)
	}
	return int(ExitExecution)
}

func MissingFlags(flags ...string) *Error {
	return NewError(CodeMissingInput, i18n.T("err_missing_flags", flags), ExitUsage).
		WithDetail("missing_flags", flags)
}
