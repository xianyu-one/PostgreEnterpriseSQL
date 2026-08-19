package interaction

import (
	"regexp"
	"strings"
)

var safeShellWord = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

var secretFlags = map[string]bool{
	"--password":        true,
	"--password-file":   true,
	"--password-fd":     true,
	"--secret":          true,
	"--token":           true,
	"--api-key":         true,
	"--credential":      true,
	"--credential-file": true,
}

func RetryCommand(prefix string, args []string) string {
	redacted := make([]string, 0, len(args)+1)
	if prefix != "" {
		redacted = append(redacted, prefix)
	}
	redactNext := false
	for _, arg := range args {
		if redactNext {
			redacted = append(redacted, "[REDACTED]")
			redactNext = false
			continue
		}
		name, _, hasValue := strings.Cut(arg, "=")
		if secretFlags[name] {
			if hasValue {
				redacted = append(redacted, name+"=[REDACTED]")
			} else {
				redacted = append(redacted, arg)
				redactNext = true
			}
			continue
		}
		redacted = append(redacted, arg)
	}
	for i, word := range redacted {
		redacted[i] = shellQuote(word)
	}
	return strings.Join(redacted, " ")
}

func shellQuote(value string) string {
	if safeShellWord.MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
