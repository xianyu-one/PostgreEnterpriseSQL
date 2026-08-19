package interaction

import (
	"os"
	"strings"

	"pg_mgr/internal/i18n"
)

// ResolveSecret accepts exactly one source. literal exists only for legacy
// compatibility and should not be shown in retry commands or diagnostics.
func ResolveSecret(literal, environment, file string) (value, source string, err error) {
	count := 0
	for _, candidate := range []string{literal, environment, file} {
		if candidate != "" {
			count++
		}
	}
	if count > 1 {
		return "", "", NewError(CodeInvalidInput, i18n.T("secret_multiple_sources"), ExitUsage)
	}
	switch {
	case environment != "":
		value, ok := os.LookupEnv(environment)
		if !ok {
			return "", "", NewError(CodeMissingInput, i18n.T("secret_env_missing", environment), ExitUsage)
		}
		return value, i18n.T("secret_source_environment"), nil
	case file != "":
		content, readErr := os.ReadFile(file)
		if readErr != nil {
			return "", "", NewError(CodeInvalidInput, i18n.T("secret_file_unreadable", readErr), ExitUsage).WithCause(readErr)
		}
		return strings.TrimRight(string(content), "\r\n"), i18n.T("secret_source_file"), nil
	case literal != "":
		return literal, i18n.T("secret_source_argument"), nil
	default:
		return "", "", nil
	}
}
