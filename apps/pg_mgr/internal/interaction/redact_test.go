package interaction

import (
	"strings"
	"testing"
)

func TestRetryCommandRedactsSecretArguments(t *testing.T) {
	got := RetryCommand("sudo", []string{"pg_mgr", "deploy", "--password", "secret-one", "--password=secret-two", "--instance", "sales"})
	if strings.Contains(got, "secret-one") || strings.Contains(got, "secret-two") {
		t.Fatalf("RetryCommand leaked a secret: %q", got)
	}
	if !strings.Contains(got, "sudo pg_mgr deploy --password '[REDACTED]' '--password=[REDACTED]' --instance sales") {
		t.Fatalf("RetryCommand() = %q", got)
	}
}
