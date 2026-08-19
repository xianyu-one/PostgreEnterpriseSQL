package interaction

import (
	"encoding/json"
	"fmt"
	"io"

	"pg_mgr/internal/i18n"
)

type Renderer struct {
	stdout io.Writer
	stderr io.Writer
	mode   OutputMode
	quiet  bool
}

type ReviewField struct {
	Label     string
	Value     string
	Secret    bool
	Source    string
	Automatic bool
}

func NewRenderer(stdout, stderr io.Writer, mode OutputMode, quiet bool) *Renderer {
	return &Renderer{stdout: stdout, stderr: stderr, mode: mode, quiet: quiet}
}

func (r *Renderer) Success(value any) error {
	if r.quiet && r.mode != OutputJSON {
		return nil
	}
	if r.mode == OutputJSON {
		return json.NewEncoder(r.stdout).Encode(value)
	}
	_, err := fmt.Fprintln(r.stdout, value)
	return err
}

func (r *Renderer) Error(err error) error {
	appErr, ok := err.(*Error)
	if !ok {
		appErr = NewError(CodeExecutionFailed, err.Error(), ExitExecution).WithCause(err)
	}
	if r.mode == OutputJSON {
		return json.NewEncoder(r.stderr).Encode(appErr)
	}
	_, writeErr := fmt.Fprintln(r.stderr, appErr.Message)
	if writeErr == nil && appErr.Remediation != "" {
		_, writeErr = fmt.Fprintln(r.stderr, appErr.Remediation)
	}
	return writeErr
}

func (r *Renderer) Review(title string, fields []ReviewField) {
	fmt.Fprintln(r.stderr, title)
	for _, field := range fields {
		value := field.Value
		if field.Secret {
			value = i18n.T("secret_set")
			if field.Source != "" {
				value += " (" + field.Source + ")"
			}
		} else if field.Automatic {
			value += " (" + i18n.T("value_automatic") + ")"
		}
		fmt.Fprintf(r.stderr, "  %s: %s\n", field.Label, value)
	}
}
